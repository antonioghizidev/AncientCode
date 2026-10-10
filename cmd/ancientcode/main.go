// Command ancientcode is the REPL entry point (SPEC §3.3). It wires the
// configuration, the provider adapter, the session store and the loop. Output
// and errors are secret-safe by construction: config validation never prints
// key values, and every error returned by the loop is already redacted.
//
// M3 (SPEC §6): the default is plan mode — the model produces a plan with only
// read-only tools; nothing mutative runs until the operator answers y to the
// plan prompt. --run is the explicit non-interactive path and requires an
// explicit allow policy in the config (no silent downgrade).
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/antonioghizidev/ancientcode/internal/audit"
	"github.com/antonioghizidev/ancientcode/internal/catalog"
	"github.com/antonioghizidev/ancientcode/internal/config"
	"github.com/antonioghizidev/ancientcode/internal/connect"
	"github.com/antonioghizidev/ancientcode/internal/creds"
	"github.com/antonioghizidev/ancientcode/internal/loop"
	"github.com/antonioghizidev/ancientcode/internal/model"
	"github.com/antonioghizidev/ancientcode/internal/provider"
	"github.com/antonioghizidev/ancientcode/internal/sandbox"
	"github.com/antonioghizidev/ancientcode/internal/skills"
	"github.com/antonioghizidev/ancientcode/internal/tools"
	"github.com/antonioghizidev/ancientcode/internal/tools/apttools"
	"github.com/antonioghizidev/ancientcode/internal/tools/code"
	"github.com/antonioghizidev/ancientcode/internal/tools/docker"
	"github.com/antonioghizidev/ancientcode/internal/tools/files"
	"github.com/antonioghizidev/ancientcode/internal/tools/shell"
	skillstool "github.com/antonioghizidev/ancientcode/internal/tools/skills"
	"github.com/antonioghizidev/ancientcode/internal/workspace"
)

var (
	flagConfig      = flag.String("config", "ancientcode.yaml", "path to the YAML configuration (may not exist)")
	flagSession     = flag.String("session", "default", "session id -> <workdir>/sessions/<id>.jsonl")
	flagResume      = flag.String("resume", "", "resume session <id>: reutiliza sessions/<id>.jsonl sem reset (M5 §9)")
	flagReset       = flag.Bool("reset", false, "truncate the session file before starting")
	flagRun         = flag.Bool("run", false, "non-interactive execution: requires approval.write_file=allow in config")
	flagVersion     = flag.Bool("version", false, "print version and exit")
	flagCleanup     = flag.Int("cleanup", 0, "remove session files under sessions/ older than N days (0=off)")
	flagVerifySkill = flag.String("verify-skill", "", "verify a downloaded skill <name> (stamp + origin_hash) and exit")
	flagTUI         = flag.Bool("tui", false, "interactive TUI (Bubble Tea, M9): requires stdin+stdout TTY; incompatible with --run")
)

func init() {
	flag.Func("skill", "activate skill <name> from skills_dir (repeatable; M7)", func(v string) error {
		skillFlags = append(skillFlags, v)
		return nil
	})
}

// skillFlags accumulates every --skill occurrence (flag.Func style).
var skillFlags []string

// turnTimeout caps one agent turn (the whole loop, including any tool rounds).
// It is aligned with the provider HTTP ceiling (D-2026-09-16-32): on a cold
// NVIDIA NIM the first token can take several minutes, so a tighter turn budget
// would abort before the provider itself gives up.
const turnTimeout = 5 * time.Minute

const version = "0.14.0-m14"

// terminalApprover is the human gate (SPEC §5.6.6). It shows the exact command
// before deciding. Answering "d" registers a veto for the tool: from then on
// the tool is refused even on ask/allow paths — the operator is the last word.
// On a raw TTY the confirmation reads keystrokes through confirmRaw (M8), so a
// Ctrl+C or an approval timeout cancels the gate cleanly instead of blocking.
type terminalApprover struct {
	vetoed  map[string]bool
	in      *bufio.Reader                                            // cooked path
	confirm func(ctx context.Context, prompt string) (string, error) // raw path (nil = cooked)
}

func (a *terminalApprover) Decide(ctx context.Context, toolName, summary string) (loop.Decision, error) {
	if a.vetoed[toolName] {
		return loop.DecisionDeny, nil
	}
	prompt := fmt.Sprintf("\n[aprovacao] tool %q\n  %s\nexecutar? [y/N, d = negar sempre] ", toolName, summary)
	var line string
	if a.confirm != nil {
		l, err := a.confirm(ctx, prompt)
		if err != nil {
			return loop.DecisionDeny, err
		}
		line = l
	} else {
		fmt.Fprint(os.Stderr, prompt)
		l, err := a.in.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return loop.DecisionDeny, err
		}
		line = l
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return loop.DecisionApprove, nil
	case "d", "deny":
		a.vetoed[toolName] = true
		return loop.DecisionDeny, nil
	default:
		return loop.DecisionDeny, nil
	}
}

func (a *terminalApprover) Vetoed(toolName string) bool { return a.vetoed[toolName] }

func checkRunFlags(cfg *config.Config) error {
	if cfg.ShellTool == "enabled" {
		return errors.New("--run e incompativel com shell_tool=enabled (shell exige aprovacao humana)")
	}
	if cfg.DockerTool == "enabled" {
		return errors.New("--run e incompativel com docker_tool=enabled (docker exige aprovacao humana)")
	}
	if cfg.AptTool == "enabled" {
		return errors.New("--run e incompativel com apt_tool=enabled (apt exige aprovacao humana)")
	}
	if cfg.Approval.WriteFile != "allow" {
		return errors.New("--run exige approval.write_file=allow explicito na config (sem aprovacao interativa)")
	}
	return nil
}

// cleanupSessions removes session JSONL files under <workdir>/sessions whose
// mtime is older than days. Only .jsonl files are eligible and only when the
// operator asked explicitly; a missing directory is a no-op.
func cleanupSessions(workdir string, days int) ([]string, error) {
	if days <= 0 {
		return nil, nil
	}
	dir := filepath.Join(workdir, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	var removed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			p := filepath.Join(dir, e.Name())
			if err := os.Remove(p); err != nil {
				return removed, err
			}
			removed = append(removed, p)
		}
	}
	return removed, nil
}

// catalogPage remembers where the operator is in the ranked catalog, so
// /models --next walks pages instead of restarting at 1 every time, and
// /models <texto> starts back at the top.
//
// It is package-level on purpose. Declared inside the command loop it is
// re-initialised on every iteration and --next silently does nothing — which is
// exactly what the Inspiron smoke showed: page 1 of 12, over and over.
//
// Without state here, paging is unusable: the operator presses next, re-reads
// page 1, and concludes the catalog is broken.
var catalogPage = struct {
	page int
	cur  int // last rendered page, so --next knows where it came from
}{}

func main() {
	flag.Parse()
	if *flagVersion {
		fmt.Printf("ancientcode %s\n", version)
		return
	}

	// M14: `auth` is handled BEFORE the config is loaded. It must work on a
	// machine that has no config yet — that is precisely when the operator
	// needs it — and it must never require a credential to manage credentials.
	if len(flag.Args()) > 0 && strings.EqualFold(flag.Args()[0], "auth") {
		os.Exit(runAuthCLI(context.Background(), flag.Args()[1:]))
	}

	// M14: first-use onboarding. config.Load() below fails with
	// "api_key_env is not set", which reads like a missing secret when the real
	// cause is that the operator never connected. Offering /connect first turns
	// that dead end into a question with an answer (D-2026-10-02-67).
	if !*flagRun && *flagVerifySkill == "" {
		if svc, err := credSvc(); err == nil {
			ok, oerr := svc.HasAny(knownCandidates())
			if oerr != nil {
				fmt.Fprintln(os.Stderr, "erro:", oerr)
				os.Exit(1)
			}

			if !ok {
				if deployed, derr := runOnboarding(context.Background(), svc); derr != nil {
					fmt.Fprintln(os.Stderr, "erro:", derr)
					os.Exit(1)
				} else if deployed {
					fmt.Fprintln(os.Stderr, "credencial guardada; seguindo")
					fmt.Fprintln(os.Stderr, "")
				} else if !hasStdinTerminal() {
					printNoCredentialHelp()
					os.Exit(1)
				}
			}
		}
	}

	// M14: the credential store must be wired BEFORE the config is validated,
	// because Validate is what decides "no credential". SetCredentialResolver
	// after Load would be too late — Load would already have exited 1.
	var credStoreForConfig *creds.Store
	if cs, cerr := credStore(); cerr == nil {
		credStoreForConfig = cs
	}

	cfg, err := config.LoadWithResolver(*flagConfig, credStoreForConfig)
	if err != nil {
		// Name the config that was consulted. A missing file is legal (defaults
		// apply), so the failure usually shows up later as "api_key_env is not
		// set" — which reads like a missing secret when the real cause is that the
		// config file is not where this binary looks for it. Renaming the project
		// moved the default from ancientai.yaml to ancientcode.yaml, so an operator
		// with the old file gets exactly that confusion.
		fmt.Fprintf(os.Stderr, "config: %v (consulted: %s)\n", err, *flagConfig)
		if _, statErr := os.Stat(*flagConfig); os.IsNotExist(statErr) {
			fmt.Fprintf(os.Stderr,
				"dica: %s nao existe; copie ancientcode.example.yaml para %s ou aponte --config\n",
				*flagConfig, *flagConfig)
		}
		os.Exit(1)
	}
	// M7: explicit operator verification of a downloaded skill (SPEC §11.1).
	// Runs BEFORE anything else, writes the stamp and exits.
	if *flagVerifySkill != "" {
		if err := skills.Verify(cfg.SkillsDir, *flagVerifySkill); err != nil {
			fmt.Fprintln(os.Stderr, "verify-skill:", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "verify-skill: %q verificada (stamp .ancientcode-verified escrito em %s)\n",
			*flagVerifySkill, cfg.SkillsDir)
		os.Exit(0)
	}
	// M5: explicit session cleanup. Nothing is ever deleted automatically; the
	// operator must ask for it by day-age (SPEC M5 "limpeza sem auto-executar").
	if *flagCleanup > 0 {
		removed, err := cleanupSessions(cfg.Workdir, *flagCleanup)
		if err != nil {
			fmt.Fprintln(os.Stderr, "cleanup:", err)
			os.Exit(1)
		}
		for _, p := range removed {
			fmt.Fprintln(os.Stderr, "session removida:", p)
		}
		return
	}
	if *flagRun {
		cfg.ToolboxMode = "run"
		if err := checkRunFlags(cfg); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	// M9: --tui is the interactive Bubble Tea frontend. It is refused for
	// non-interactive paths (--run) and whenever either stdin or stdout is not
	// a real char device (pipes/files/captures get the byte-stable REPL or
	// --run path, spec 09 §4).
	if *flagTUI && *flagRun {
		fmt.Fprintln(os.Stderr, "erro: --tui e incompativel com --run (TUI e a entrega interativa)")
		os.Exit(2)
	}
	if *flagTUI && !canUseTUI(isCharDevice(os.Stdin), isCharDevice(os.Stdout)) {
		fmt.Fprintln(os.Stderr, "erro: --tui exige stdin E stdout TTY (pipes/arquivos recusados)")
		os.Exit(2)
	}

	// resume wins over --session: --resume is the explicit "reuse, never reset" id.
	// Combining it with --reset is refused — it would wipe the history being
	// resumed (SPEC §9, M5).
	if *flagResume != "" {
		if *flagReset {
			fmt.Fprintln(os.Stderr, "erro: --resume e incompativel com --reset (resumo reutiliza o historico)")
			os.Exit(2)
		}
		*flagSession = *flagResume
	}

	sessionPath := filepath.Join(cfg.Workdir, "sessions", *flagSession+".jsonl")
	store, err := model.Open(sessionPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "model:", err)
		os.Exit(1)
	}
	if *flagReset {
		if err := store.Reset(); err != nil {
			fmt.Fprintln(os.Stderr, "model:", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "session reset:", sessionPath)
	}

	client := provider.NewOpenAICompatible(cfg.BaseURL, cfg.Model, cfg.APIKeyEnv, nil)

	// M14: wire the credential store into the provider so a key saved by /connect
	// is actually used by requests. Chat and ListModels resolve per request
	// instead of only reading the environment.
	if credStoreForConfig != nil {
		client.SetKeyResolver(keyResolverFor(credStoreForConfig))
	}

	// M15: one live root, shared by the shell and the file/code tools.
	//
	// These used to be two different directories (workdir for the shell,
	// code_root for the tools), which meant the agent could read in one place
	// and write in another. One root fixes that and is what makes /cd mean
	// "this folder is now the project".
	ws, err := workspace.New(cfg.ProjectRoot, cfg.Workdir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "workspace:", err)
		os.Exit(1)
	}
	// M15: reopen in the last project the operator cd'ed into. Silently ignored
	// on any failure — a missing or hand-edited state.json falls back to the
	// config, and refusing to start over a convenience would be worse.
	if st, serr := stateStore(); serr == nil {
		if saved, ok, perr := st.ProjectRoot(); perr == nil && ok {
			if serr := ws.SetProjectRoot(saved); serr == nil {
				cfg.ProjectRoot = ws.ProjectRoot()
			}
		}
	}
	rootRef := workspace.NewRef(ws)

	// The filesystem tools are rooted at the project root (SPEC §10.3): every
	// read/write for the coding agent stays inside the folder the operator is
	// working in.
	fs := files.NewFilesLive(rootRef.CodeRoot, cfg.OutputMaxBytes)
	reg := tools.NewRegistry()
	for _, t := range []tools.Tool{fs.ToolReadFile(), fs.ToolWriteFile(), fs.ToolListDir()} {
		if err := reg.Register(t); err != nil {
			fmt.Fprintln(os.Stderr, "tools:", err)
			os.Exit(1)
		}
	}
	// M6: coding-agent tools (SPEC §10). code_review is read-only; scaffold and
	// edit always require human approval. Generated code is never executed.
	codeTools := code.NewToolsetLive(rootRef.CodeRoot, cfg.OutputMaxBytes)
	for _, t := range []tools.Tool{codeTools.ToolCodeScaffold(), codeTools.ToolEditFile(), codeTools.ToolCodeReview()} {
		if err := reg.Register(t); err != nil {
			fmt.Fprintln(os.Stderr, "tools:", err)
			os.Exit(1)
		}
	}
	if cfg.ShellTool == "enabled" {
		shellTimeout, err := cfg.ToolTimeout("shell")
		if err != nil {
			fmt.Fprintln(os.Stderr, "config:", err)
			os.Exit(1)
		}
		policy := sandbox.NewPolicy(sandbox.DefaultAllowed)
		exec := sandbox.NewExecutorLive(rootRef.Workdir, cfg.RunUser, shellTimeout, cfg.OutputMaxBytes)
		if err := reg.Register(shell.New(policy, exec, cfg.OutputMaxBytes)); err != nil {
			fmt.Fprintln(os.Stderr, "tools:", err)
			os.Exit(1)
		}
	}
	// M5: docker and apt only operate with human approval (ask), and only when
	// explicitly enabled in the config. Both are argument-vector wrappers —
	// model text never becomes a shell line.
	if cfg.DockerTool == "enabled" {
		dockerTimeout, err := cfg.ToolTimeout("docker")
		if err != nil {
			fmt.Fprintln(os.Stderr, "config:", err)
			os.Exit(1)
		}
		if err := reg.Register(docker.New(dockerTimeout, cfg.OutputMaxBytes)); err != nil {
			fmt.Fprintln(os.Stderr, "tools:", err)
			os.Exit(1)
		}
	}
	if cfg.AptTool == "enabled" {
		aptTimeout, err := cfg.ToolTimeout("apt")
		if err != nil {
			fmt.Fprintln(os.Stderr, "config:", err)
			os.Exit(1)
		}
		if err := reg.Register(apttools.New(aptTimeout, cfg.OutputMaxBytes)); err != nil {
			fmt.Fprintln(os.Stderr, "tools:", err)
			os.Exit(1)
		}
	}
	// M7: skills (SPEC §11.1). One shared skills.Set bonds the loop and the
	// skill_run tool; --skill loads are validated against the registry before
	// any turn (fail-closed activation). skill_run is always registered but the
	// loop only exposes it when a scripted skill is active in run mode.
	set := skills.NewSet()
	exec := sandbox.NewExecutorLive(rootRef.Workdir, cfg.RunUser, 60*time.Second, cfg.OutputMaxBytes)
	if err := reg.Register(skillstool.New(set, exec, cfg.OutputMaxBytes)); err != nil {
		fmt.Fprintln(os.Stderr, "tools:", err)
		os.Exit(1)
	}
	loaded := make([]*skills.Skill, 0, len(skillFlags))
	for _, name := range skillFlags {
		s, err := skills.Load(cfg.SkillsDir, name)
		if err != nil {
			fmt.Fprintln(os.Stderr, "skills:", err)
			os.Exit(1)
		}
		loaded = append(loaded, s)
	}
	// --run is non-interactive: a scripted skill would demand the human gate
	// and hang/deny; refuse startup instead of a silent partial run (mirrors
	// the shell/docker/apt restrictions above).
	if *flagRun && setHasScripts(loaded) {
		fmt.Fprintln(os.Stderr, "erro: --run e incompativel com skills que possuem scripts (skill_run exige aprovacao humana)")
		os.Exit(1)
	}

	// M4: append-only audit (SPEC §5.7). Fail-closed: only the hash of the
	// output and the redacted args go to disk; never the model's secrets.
	rec, err := audit.NewRecorder(filepath.Join(cfg.Workdir, "audit.jsonl"), 10<<20, 5)
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit:", err)
		os.Exit(1)
	}

	in := bufio.NewReader(os.Stdin)

	// M8: raw-mode line editing (↑/↓ history, per-D-37) is enabled only when
	// stdin is a char device AND the raw driver is available (linux). Pipes,
	// files and --run keep the byte-stable cooked path. M9: the TUI manages its
	// own terminal through Bubble Tea, so the REPL raw mode is skipped for it.
	var rt *rawTerm
	var rr *rawReader
	if isCharDevice(os.Stdin) && !*flagTUI {
		if t, err := enableRawInput(); err == nil {
			rt = t
			defer rt.restore()
			rr = &rawReader{in: in, fd: os.Stdin, tty: true}
		}
	}

	// The approver is the frontend boundary: the TUI answers through tuiGate
	// (dialog rendered on screen), the REPL through terminalApprover. Both
	// implement the same loop.Approver — the core never knows which one.
	var appr loop.Approver
	var gate *tuiGate
	if *flagTUI {
		gate = newTuiGate()
		appr = gate
	} else {
		ta := &terminalApprover{in: in, vetoed: make(map[string]bool)}
		if rr != nil {
			ta.confirm = func(ctx context.Context, prompt string) (string, error) {
				line, kind := confirmRaw(ctx, rr, prompt)
				if kind == lineCtrlC {
					return "", context.Canceled
				}
				return strings.TrimSpace(line), nil
			}
		}
		appr = ta
	}

	agent := loop.New(client, store, cfg, reg, appr)
	// M14 §3.7: the model chosen with /models is remembered per provider, so
	// returning to a provider restores it. Best-effort: a state.json that
	// cannot be opened must not stop a session that works without it.
	if st, serr := stateStore(); serr == nil {
		agent.SetModelMemory(st)
	}
	agent.SetAudit(rec)
	// M7: adopt the shared skill set and activate every requested skill.
	if err := agent.SetSkills(set); err != nil {
		fmt.Fprintln(os.Stderr, "skills:", err)
		os.Exit(1)
	}
	for _, s := range loaded {
		if err := agent.SetSkill(s); err != nil {
			fmt.Fprintln(os.Stderr, "skills:", err)
			os.Exit(1)
		}
	}
	// M9: the TUI frontend owns the terminal; everything above (config, tools,
	// audit, approver) is shared with the REPL. runTUI blocks until exit.
	if *flagTUI {
		tuiWs := ws
		tuiPersist := cdPersister(cfg)
		runTUI(agent, gate, cfg, version, filepath.Base(sessionPath), credSvcForTUI(), tuiWs, tuiPersist)
		return
	}
	// M8: structured observer replaces the D-35 status string on TTYs — the
	// Phase labels keep the same vocabulary; pipes stay silent and clean.
	if ttyOut := isCharDevice(os.Stdout); ttyOut {
		agent.SetObserver(func(ev loop.Event) { renderStatus(ev.Phase.Label()) })
	}

	fmt.Fprint(os.Stderr, banner(version, agent, filepath.Base(sessionPath), cfg.ToolboxMode, ws.ProjectRoot()))

	// Ctrl+C semantics (M8, D-37): a signal during a turn cancels the turn
	// context (tools never run post-cancel, loop-level guarantee); a signal at
	// the prompt exits. ISIG stays enabled in raw mode so this is the only
	// Ctrl+C path — the editor never sees the byte.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	var (
		inTurn  int32
		turnCxl atomic.Value // holds context.CancelFunc while a turn is running
	)
	go func() {
		for range sigCh {
			if atomic.LoadInt32(&inTurn) == 1 {
				if f, _ := turnCxl.Load().(context.CancelFunc); f != nil {
					f()
				}
				continue
			}
			os.Exit(130)
		}
	}()

	ctx := context.Background()

	// M14: the credential store. A failure to open it must NOT be fatal: the
	// harness still works through api_key_env alone, which is the pre-M14 path.
	// Only /connect and `auth` are unavailable, and they say so when invoked.
	var credService *connect.Service
	if svc, err := credSvc(); err == nil {
		credService = svc
	}

	// M12: the entries shown by the last /models, so "/model <N>" resolves the
	// number the operator actually saw.
	var lastCatalog []catalog.Entry

	// withTurn runs one bounded agent interaction under the signal guard and
	// returns the model answer.
	withTurn := func(fn func(context.Context) (string, error)) (string, error) {
		tctx, cancel := context.WithTimeout(ctx, turnTimeout)
		turnCxl.Store(cancel)
		atomic.StoreInt32(&inTurn, 1)
		resp, err := fn(tctx)
		atomic.StoreInt32(&inTurn, 0)
		turnCxl.Store((context.CancelFunc)(nil))
		cancel()
		return resp, err
	}
	handleTurnErr := func(err error) {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "turno cancelado (Ctrl+C) — voce pode dar /retry ou /continue")
			return
		}
		fmt.Fprintln(os.Stderr, "erro:", err)
	}

	// askLine reads a short answer (plan flow) honouring the input mode.
	askLine := func(prompt string) (string, error) {
		if rr != nil {
			line, kind := promptLineRaw(rr, prompt, nil)
			if kind == lineCtrlC || kind == lineCtrlD {
				return "", io.EOF
			}
			return line, nil
		}
		line := readLine(in, prompt)
		if lastErr == io.EOF {
			return "", io.EOF
		}
		return line, nil
	}

	for {
		var line string
		var kind lineDoneKind
		var readErr error
		if rr != nil {
			line, kind = promptLineRaw(rr, "> ", agent.PromptList())
		} else {
			line = readLine(in, "> ")
			kind, readErr = lineEnter, lastErr
		}
		if kind == lineCtrlD || (readErr != nil && errors.Is(readErr, io.EOF)) {
			break
		}
		if kind == lineCtrlC {
			return // Ctrl+C at the prompt exits the REPL
		}
		if line == "" {
			continue
		}

		if cmd, isCmd := parseCommand(line); isCmd {
			switch cmd.kind {
			case cmdExit:
				return
			case cmdReset:
				if err := store.Reset(); err != nil {
					fmt.Fprintln(os.Stderr, "model:", err)
				} else {
					fmt.Fprintln(os.Stderr, "session reset")
				}
				continue
			case cmdHelp:
				fmt.Println(helpText)
				continue
			case cmdHistory:
				for i, p := range agent.PromptList() {
					fmt.Printf("%2d: %s\n", i+1, p) // redacted by the domain (M8 §4)
				}
				continue
			case cmdContext:
				u := agent.ContextUsage()
				if u.Pct < 0 {
					fmt.Printf("ctx: off (%d bytes; session_context_bytes=0)\n", u.SentBytes)
				} else {
					fmt.Printf("ctx: %d%% (%d/%d bytes)\n", u.Pct, u.SentBytes, u.Budget)
				}
				continue
			case cmdRetry, cmdContinue:
				resp, err := withTurn(func(tctx context.Context) (string, error) { return agent.Continue(tctx, "") })
				if err != nil {
					handleTurnErr(err)
				} else {
					fmt.Println(resp)
				}
				continue
			case cmdModel:
				if cmd.arg == "" {
					fmt.Println("model:", agent.CurrentModel())
				} else if i, ok := modelIndexArg(cmd.arg); ok {
					// M12: a number selects from the last /models listing, so a
					// stale number cannot switch to an unintended model
					// (SelectModel refuses out-of-range and changes nothing).
					if len(lastCatalog) == 0 {
						fmt.Fprintln(os.Stderr, "erro: /model <numero> precisa de um /models anterior nesta sessao")
						continue
					}
					if err := agent.SelectModel(lastCatalog, i); err != nil {
						fmt.Fprintln(os.Stderr, "erro:", err)
					} else {
						fmt.Println("model:", agent.CurrentModel(), "(vale nesta sessao — nao persiste no yaml)")
					}
				} else if err := agent.SwitchModel(cmd.arg); err != nil {
					fmt.Fprintln(os.Stderr, "erro:", err)
				} else {
					fmt.Println("model:", agent.CurrentModel(), "(vale nesta sessao — nao persiste no yaml)")
				}
				continue
			case cmdModels:
				entries, err := agent.ListModels(ctx)
				if err != nil {
					// Fail-closed: a failed listing never changes the model.
					fmt.Fprintln(os.Stderr, "erro:", err)
					continue
				}
				want := resolveCatalogPage(cmd.opts, catalogPage.cur)
				if want < 1 {
					want = 1
				}
				lines, visible, pg := catalogLines(entries, cmd.opts, agent.CurrentModel(), want)

				// --check runs AFTER the page is resolved, because the consent must
				// name the real cost: 40 probes for a page, not 377 for the catalog.
				// Before this, --check asked that question and then did nothing.
				var checked []loop.CheckOutcome
				didCheck := false
				if cmd.opts.check {
					fmt.Fprint(os.Stderr, checkConsentMessage(pg))
					line, _ := askLine("")
					if !strings.EqualFold(strings.TrimSpace(line), "y") {
						fmt.Fprintln(os.Stderr, "verificacao cancelada; nada foi executado")
					} else {
						var cerr error
						checked, cerr = runCheckPage(ctx, agent, entries, pg)
						if cerr != nil {
							fmt.Fprintln(os.Stderr, "erro:", cerr)
						} else {
							didCheck = true
						}
					}
				}

				// Re-render: the rows now carry the verdicts, and verified entries
				// rank higher, so the page content genuinely moves. Printing the
				// pre-check list would hide the very result asked for.
				if didCheck {
					// Re-render from `entries`: the probe wrote the verdicts there,
					// and re-filtering from the stale `visible` would drop them.
					lines, visible, pg = catalogLines(entries, cmd.opts, agent.CurrentModel(), want)
					for _, l := range checkOutcomeLines(entries, pg, checked) {
						fmt.Println(redactCatalogEcho(l))
					}
					fmt.Println()
				}
				catalogPage.page, catalogPage.cur = want, pg.Number
				for _, l := range lines {
					fmt.Println(redactCatalogEcho(l))
				}
				lastCatalog = visible
				continue
			case cmdProvider:
				if cmd.arg == "" {
					fmt.Println("provider base_url:", agent.CurrentProviderBaseURL())
					fmt.Println("opcoes:", strings.Join(agent.ProviderNames(), ", "))
					if credService != nil {
						st := credService.StatusFor(agent.CurrentProvider(), cfg.APIKeyEnv)
						fmt.Println(st.Describe())
					}
				} else if err := agent.SwitchProvider(cmd.arg); err != nil {
					fmt.Fprintln(os.Stderr, "erro:", err)
				} else {
					fmt.Println("provider:", strings.ToLower(strings.TrimSpace(cmd.arg)), "ativo (vale nesta sessao)")
					if credService != nil {
						st := credService.StatusFor(agent.CurrentProvider(), cfg.APIKeyEnv)
						fmt.Println(st.Describe())
					}
				}
				continue
			case cmdCd:
				runCdREPL(ws, cmd, askLine, cdPersister(cfg))
				continue
			case cmdGrant:
				msg, gerr := grantCommand(ws, cmd.arg)
				if gerr != nil {
					fmt.Fprintln(os.Stderr, "erro:", msg)
					continue
				}
				fmt.Println(msg)
				continue
			case cmdConnect:
				runConnectREPL(ctx, cmd, credService, agent, cfg)
				continue
			case cmdResend:
				if cmd.index < 0 {
					fmt.Fprintln(os.Stderr, "erro: !N exige numero positivo (ex.: !1 = mais recente)")
					continue
				}
				p, ok := agent.PromptAt(cmd.index - 1)
				if !ok {
					fmt.Fprintf(os.Stderr, "erro: sem prompt !%d (historico tem %d entradas)\n", cmd.index, len(agent.PromptList()))
					continue
				}
				line = p // resend below
			}
		}

		agent.AddPrompt(line) // redacted at the domain boundary (M8 §4)
		resp, err := withTurn(func(tctx context.Context) (string, error) { return agent.Turn(tctx, line) })
		if err != nil {
			handleTurnErr(err)
			continue
		}
		if cfg.ToolboxMode == "plan" {
			decided := runPlanFlow(ctx, agent, askLine, resp)
			if !decided {
				continue
			}
			cfg.ToolboxMode = "run"
			continue
		}
		fmt.Println(resp)
	}
}

// setHasScripts reports whether any loaded skill ships scripts/ (a scripted
// skill would expose skill_run, which --run cannot approve non-interactively).
func setHasScripts(loaded []*skills.Skill) bool {
	for _, s := range loaded {
		if len(s.ScriptNames) > 0 {
			return true
		}
	}
	return false
}

// runPlanFlow presents the plan and resolves it against the operator: y runs
// it (switching the session to run mode), n discards, feedback:<msg> feeds the
// model back and replans. Returns true when the session should keep the turn.
// ask reads one answer line honouring the input mode (raw TTY or cooked).
// resolveCatalogPage decides which page a /models invocation should show.
//
// It was inline in the command loop, which made it untestable: a test could
// check that the state variable survived, but not that --next actually resolved
// to the NEXT page — and a mutation that pinned --next to page 1 stayed green.
//
// Two rules:
//   - explicit paging (--next/--prev) moves relative to where the operator is;
//   - anything else (a bare /models, or a new search) starts at the top, because
//     a search landing on page 7 of a stale list reads as "nothing matches".
func resolveCatalogPage(opt catalogOpts, cur int) int {
	want := cur
	switch {
	case opt.page == -1:
		want = cur + 1
	case opt.page == -2:
		want = cur - 1
	case !opt.pageSet || opt.query != "":
		want = 1
	}
	if want < 1 {
		want = 1
	}
	return want
}

func runPlanFlow(ctx context.Context, agent *loop.Loop, ask func(prompt string) (string, error), plan string) bool {
	for {
		fmt.Println(plan)
		ans, err := ask("[plano] executar? [y/N, feedback:<msg>] ")
		if err != nil || ans == "" {
			return false // EOF or Ctrl+D/C at the plan gate discards
		}
		ans = strings.TrimSpace(ans)
		switch {
		case strings.EqualFold(ans, "y") || strings.EqualFold(ans, "yes"):
			tctx, cancel := context.WithTimeout(ctx, turnTimeout)
			err := agent.SetToolboxMode("run")
			var out string
			if err == nil {
				out, err = agent.Continue(tctx, "Aprovado: execute o plano que voce preparou, passo a passo, sem repetir o plano.")
			}
			cancel()
			if err != nil {
				fmt.Fprintln(os.Stderr, "erro:", err)
				return true
			}
			fmt.Println(out)
			return true
		case strings.HasPrefix(ans, "feedback:"):
			msg := strings.TrimSpace(strings.TrimPrefix(ans, "feedback:"))
			tctx, cancel := context.WithTimeout(ctx, turnTimeout)
			newPlan, err := agent.Continue(tctx, "Feedback do operador sobre o plano: "+msg)
			cancel()
			if err != nil {
				fmt.Fprintln(os.Stderr, "erro:", err)
				return false
			}
			plan = newPlan
		default:
			// "n"/"no"/garbage: discard the plan.
			return false
		}
	}
}

var lastErr error

func readLine(r *bufio.Reader, prompt string) string {
	fmt.Print(prompt)
	s, err := r.ReadString('\n')
	lastErr = err
	return strings.TrimSpace(s)
}

// isCharDevice reports whether f is a terminal-class device (console on
// Windows, tty on Unix). Pipes, files and captures return false.
func isCharDevice(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// renderStatus writes a single status line over the current terminal line and
// clears it when phase == "". Errors are ignored: a dead terminal must never
// break the agent loop.
func renderStatus(phase string) {
	if phase == "" {
		fmt.Print("\r" + strings.Repeat(" ", 70) + "\r")
		return
	}
	fmt.Printf("\r>>> %-50s", phase)
}
