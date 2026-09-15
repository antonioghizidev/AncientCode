// Command ancientai is the REPL entry point (SPEC §3.3). It wires the
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
	"path/filepath"
	"strings"
	"time"

	"ancientai/internal/audit"
	"ancientai/internal/config"
	"ancientai/internal/loop"
	"ancientai/internal/model"
	"ancientai/internal/provider"
	"ancientai/internal/sandbox"
	"ancientai/internal/tools"
	"ancientai/internal/tools/apttools"
	"ancientai/internal/tools/docker"
	"ancientai/internal/tools/files"
	"ancientai/internal/tools/shell"
)

var (
	flagConfig  = flag.String("config", "ancientai.yaml", "path to the YAML configuration (may not exist)")
	flagSession = flag.String("session", "default", "session id -> <workdir>/sessions/<id>.jsonl")
	flagResume  = flag.String("resume", "", "resume session <id>: reutiliza sessions/<id>.jsonl sem reset (M5 §9)")
	flagReset   = flag.Bool("reset", false, "truncate the session file before starting")
	flagRun     = flag.Bool("run", false, "non-interactive execution: requires approval.write_file=allow in config")
	flagVersion = flag.Bool("version", false, "print version and exit")
	flagCleanup = flag.Int("cleanup", 0, "remove session files under sessions/ older than N days (0=off)")
)

const version = "0.5.0-m5"

// terminalApprover is the human gate (SPEC §5.6.6). It shows the exact command
// before deciding. Answering "d" registers a veto for the tool: from then on
// the tool is refused even on ask/allow paths — the operator is the last word.
type terminalApprover struct {
	in     *bufio.Reader
	vetoed map[string]bool
}

func (a *terminalApprover) Decide(_ context.Context, toolName, summary string) (loop.Decision, error) {
	if a.vetoed[toolName] {
		return loop.DecisionDeny, nil
	}
	fmt.Fprintf(os.Stderr, "\n[aprovacao] tool %q\n  %s\nexecutar? [y/N, d = negar sempre] ", toolName, summary)
	line, err := a.in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return loop.DecisionDeny, err
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

func main() {
	flag.Parse()
	if *flagVersion {
		fmt.Printf("ancientai %s\n", version)
		return
	}

	cfg, err := config.Load(*flagConfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
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

	fs := files.NewFiles(cfg.Workdir, cfg.OutputMaxBytes)
	reg := tools.NewRegistry()
	for _, t := range []tools.Tool{fs.ToolReadFile(), fs.ToolWriteFile(), fs.ToolListDir()} {
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
		exec := sandbox.NewExecutor(cfg.Workdir, cfg.RunUser, shellTimeout, cfg.OutputMaxBytes)
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

	// M4: append-only audit (SPEC §5.7). Fail-closed: only the hash of the
	// output and the redacted args go to disk; never the model's secrets.
	rec, err := audit.NewRecorder(filepath.Join(cfg.Workdir, "audit.jsonl"), 10<<20, 5)
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit:", err)
		os.Exit(1)
	}

	in := bufio.NewReader(os.Stdin)
	appr := &terminalApprover{in: in, vetoed: make(map[string]bool)}

	agent := loop.New(client, store, cfg, reg, appr)
	agent.SetAudit(rec)

	fmt.Fprintf(os.Stderr, "ancientai %s — provider=%s model=%s session=%s\n",
		version, cfg.Provider, cfg.Model, filepath.Base(sessionPath))
	fmt.Fprintf(os.Stderr, "toolbox_mode=%s (plan: nada mutante executa sem o seu 'y')\ntipo /exit para sair\n",
		cfg.ToolboxMode)

	ctx := context.Background()
	for {
		line := readLine(in, "> ")
		if line == "" && errors.Is(lastErr, io.EOF) {
			break
		}
		if line == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "/exit", "/quit":
			return
		case "/reset":
			if err := store.Reset(); err != nil {
				fmt.Fprintln(os.Stderr, "model:", err)
			} else {
				fmt.Fprintln(os.Stderr, "session reset")
			}
			continue
		case "/help":
			fmt.Println("comandos: /exit /quit /reset /help | para 'y' no plano: aprova e executa com toolbox completo")
			continue
		}

		tctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		resp, err := agent.Turn(tctx, strings.TrimSpace(line))
		cancel()
		if err != nil {
			fmt.Fprintln(os.Stderr, "erro:", err)
			continue
		}

		if cfg.ToolboxMode == "plan" {
			decided := runPlanFlow(ctx, agent, in, resp)
			if !decided {
				continue
			}
			cfg.ToolboxMode = "run"
			continue
		}
		fmt.Println(resp)
	}
}

// runPlanFlow presents the plan and resolves it against the operator: y runs
// it (switching the session to run mode), n discards, feedback:<msg> feeds the
// model back and replans. Returns true when the session should keep the turn.
func runPlanFlow(ctx context.Context, agent *loop.Loop, in *bufio.Reader, plan string) bool {
	for {
		fmt.Println(plan)
		ans := readLine(in, "[plano] executar? [y/N, feedback:<msg>] ")
		ans = strings.TrimSpace(ans)
		switch {
		case ans == "" || strings.EqualFold(ans, "n") || strings.EqualFold(ans, "no"):
			return false
		case strings.EqualFold(ans, "y") || strings.EqualFold(ans, "yes"):
			tctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
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
			tctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			newPlan, err := agent.Continue(tctx, "Feedback do operador sobre o plano: "+msg)
			cancel()
			if err != nil {
				fmt.Fprintln(os.Stderr, "erro:", err)
				return false
			}
			plan = newPlan
		default:
			continue
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
