// M9 TUI (specs/milestones/09-m9-tui.md): a Bubble Tea frontend that consumes
// the exact contract M8 stabilized — the pure command parser (commands.go), the
// loop.Approver gate and the structured Event stream (loop.SetObserver). The
// render boundary is here (View); every projection lives in tui_panels.go and
// is stdlib-pure so it is unit-tested without a terminal.
//
// Security invariants (SPEC M9 §4):
//   - The gate (tuiGate) implements loop.Approver and is the ONLY approval
//     path: no tool executes unless the operator answered y/n/d in the dialog.
//   - Everything painted re-redacts defensively via internal/redact before
//     leaving the model (a raw secret that somehow reached a session line can
//     never appear on screen).
//   - The model never calls agent.Turn/Continue on a keypress: it returns a
//     tea.Cmd which the Bubble Tea runtime runs, and results come back as
//     messages, so the input loop stays reactive and Ctrl+C works mid-turn.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"ancientai/internal/config"
	"ancientai/internal/loop"
	"ancientai/internal/redact"
)

// turnRunner is the tiny port the TUI drives turns through. Internal/loop is
// injected behind it (dependency inversion, same as every other frontend); a
// fakeRunner in tests replaces it for deterministic, network-free coverage.
type turnRunner interface {
	run(ctx context.Context, user string) (string, error)
	cont(ctx context.Context) (string, error)
}

// loopRunner adapts *loop.Loop to turnRunner.
type loopRunner struct {
	agent *loop.Loop
}

func (r loopRunner) run(ctx context.Context, user string) (string, error) {
	return r.agent.Turn(ctx, user)
}

func (r loopRunner) cont(ctx context.Context) (string, error) {
	return r.agent.Continue(ctx, "")
}

const (
	maxSessionLines  = 200 // session panel scrollback bound
	maxActivityLines = 30  // activity panel scrollback bound
)

// turnResultMsg carries one finished turn back to Update (from the tea.Cmd).
type turnResultMsg struct {
	out   string
	err   error
	retry bool
}

// gateOpenMsg tells the model a gate dialog must be shown (bridged from
// tuiGate.opened by the wiring in runTUI).
type gateOpenMsg struct {
	dialog *gateOpen
}

// gateClosedMsg clears the dialog (bridged from tuiGate.closed).
type gateClosedMsg struct{}

// agentEventMsg carries a redacted loop event onto the activity panel.
type agentEventMsg struct {
	ev loop.Event
}

// tuiModel is the TUI state machine. It is a pure message handler: every
// side effect (turn, gate answer, model switch) is a returned tea.Cmd or a
// response sent through the injected tuiGate.
type tuiModel struct {
	agent   *loop.Loop
	gate    *tuiGate
	usageFn func() loop.Usage // nil in tests; real wiring binds agent.ContextUsage

	runner      turnRunner
	version     string
	provider    string
	model       string
	toolbox     string
	sessionName string
	width       int

	input      string
	cursor     int
	promptHist []string
	histIdx    int // -1 = editing a fresh line
	draft      string

	session  []string // session panel lines (redacted before paint)
	activity []string // activity panel lines (from the observer)
	banner   string   // one error/info line
	usage    loop.Usage

	inTurn     bool
	turnCancel func() // canceled on Ctrl+C mid-turn
	dialog     *gateOpen
}

func (m *tuiModel) resetPrompt() {
	m.input = ""
	m.cursor = 0
	m.histIdx = -1
	m.draft = ""
}

func (m *tuiModel) refreshUsage() {
	if m.usageFn != nil {
		m.usage = m.usageFn()
	}
}

func (m *tuiModel) statusTone() statusTone {
	switch {
	case m.banner != "":
		return statusErr
	case m.inTurn || m.dialog != nil:
		return statusWait
	default:
		return statusOK
	}
}

// Init satisfies tea.Model.
func (m *tuiModel) Init() tea.Cmd { return nil }

// Update satisfies tea.Model: it maps messages to model state and returns the
// side effects (tea.Cmd) the runtime should execute.
func (m *tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case gateOpenMsg:
		m.dialog = msg.dialog
		return m, nil
	case gateClosedMsg:
		m.dialog = nil
		return m, nil
	case agentEventMsg:
		if line := activityFromEvent(msg.ev); line != "" {
			m.activity = append(m.activity, line)
			m.activity = capLines(m.activity, maxActivityLines)
		}
		m.refreshUsage()
		return m, nil
	case turnResultMsg:
		m.inTurn = false
		m.turnCancel = nil
		if msg.err != nil {
			m.banner = "erro: " + msg.err.Error()
		} else {
			m.banner = ""
			m.session = append(m.session, "< "+redact.Redact(truncatedLine(msg.out)))
			m.session = capLines(m.session, maxSessionLines)
			m.refreshUsage()
		}
		return m, nil
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}

// handleKey routes one key event. When a gate dialog is open only y/n/d (or
// Ctrl+C = n) are meaningful; everything else is routed to editing/turns as
// the spec keymap (09 §5) requires.
func (m *tuiModel) handleKey(k tea.KeyMsg) tea.Cmd {
	if m.dialog != nil {
		return m.handleGateKey(k)
	}
	switch k.Type {
	case tea.KeyEnter:
		return m.submit()
	case tea.KeyCtrlC:
		if m.inTurn {
			if m.turnCancel != nil {
				m.turnCancel()
			}
			m.inTurn = false
			m.turnCancel = nil
			m.banner = "turno cancelado (Ctrl+C)"
			return nil
		}
		return tea.Quit
	case tea.KeyUp:
		m.histStep(-1)
	case tea.KeyDown:
		m.histStep(+1)
	case tea.KeyLeft:
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyRight:
		if m.cursor < len([]rune(m.input)) {
			m.cursor++
		}
	case tea.KeyHome:
		m.cursor = 0
	case tea.KeyEnd:
		m.cursor = len([]rune(m.input))
	case tea.KeyBackspace:
		m.backspace()
	case tea.KeyDelete:
		m.deleteAt()
	case tea.KeySpace:
		m.insertRunes([]rune{' '})
	case tea.KeyRunes:
		return m.handleRunes(k.Runes)
	}
	return nil
}

// handleGateKey resolves the open dialog. Only y/n/d/Ctrl+C do anything; d
// registers a permanent veto for the tool (operator last word, D-37).
func (m *tuiModel) handleGateKey(k tea.KeyMsg) tea.Cmd {
	if k.Type == tea.KeyCtrlC {
		m.dialog.Respond(loop.DecisionDeny)
		m.dialog = nil
		return nil
	}
	if k.Type != tea.KeyRunes || len(k.Runes) != 1 {
		return nil
	}
	switch k.Runes[0] {
	case 'y', 'Y':
		m.dialog.Respond(loop.DecisionApprove)
	case 'n', 'N':
		m.dialog.Respond(loop.DecisionDeny)
	case 'd', 'D':
		m.dialog.Respond(loop.DecisionDeny)
		m.gate.registerVeto(m.dialog.Tool)
	default:
		return nil // any other key keeps the dialog open
	}
	m.dialog = nil
	return nil
}

// handleRunes processes printable key runs, with the special ^R/retry and
// ^C-ish clear affordances the spec maps (a banner error makes 'r' retry and
// 'c' clear).
func (m *tuiModel) handleRunes(rs []rune) tea.Cmd {
	if len(rs) == 1 {
		switch rs[0] {
		case 'r', 'R':
			if m.banner != "" {
				m.banner = ""
				m.inTurn = true
				ctx, cancel := context.WithCancel(context.Background())
				m.turnCancel = cancel
				return func() tea.Msg {
					out, err := m.runner.cont(ctx)
					return turnResultMsg{out: out, err: err, retry: true}
				}
			}
		case 'c', 'C':
			if m.banner != "" {
				m.banner = ""
				return nil
			}
		}
	}
	m.insertRunes(rs)
	return nil
}

func (m *tuiModel) insertRunes(rs []rune) {
	for _, r := range rs {
		if r < 0x20 {
			continue
		}
		buf := []rune(m.input)
		if m.cursor < 0 {
			m.cursor = 0
		}
		if m.cursor > len(buf) {
			m.cursor = len(buf)
		}
		buf = append(buf, 0)
		copy(buf[m.cursor+1:], buf[m.cursor:])
		buf[m.cursor] = r
		m.input = string(buf)
		m.cursor++
	}
}

func (m *tuiModel) backspace() {
	if m.cursor <= 0 {
		return
	}
	buf := []rune(m.input)
	buf = append(buf[:m.cursor-1], buf[m.cursor:]...)
	m.input = string(buf)
	m.cursor--
}

func (m *tuiModel) deleteAt() {
	buf := []rune(m.input)
	if m.cursor >= len(buf) {
		return
	}
	buf = append(buf[:m.cursor], buf[m.cursor+1:]...)
	m.input = string(buf)
}

// histStep mirrors editor.historyStep (input.go): ↓ past the newest restores
// the fresh draft, ↑ climbs from the newest history entry.
func (m *tuiModel) histStep(delta int) {
	if len(m.promptHist) == 0 {
		return
	}
	h := len(m.promptHist)
	if m.histIdx < -1 || m.histIdx >= h {
		m.histIdx = -1
	}
	switch {
	case m.histIdx == -1 && delta > 0:
		return
	case m.histIdx == -1 && delta < 0:
		m.draft = m.input
		m.histIdx = h - 1
	case m.histIdx == 0 && delta < 0:
		// already at the oldest entry
	case m.histIdx == h-1 && delta > 0:
		m.histIdx = -1
		m.input = m.draft
		m.cursor = len([]rune(m.input))
		return
	default:
		m.histIdx += delta
	}
	if m.histIdx >= 0 && m.histIdx < h {
		m.input = m.promptHist[m.histIdx]
		m.cursor = len([]rune(m.input))
	}
}

// submit runs one turn (or a command). It never blocks this method: the turn
// is a tea.Cmd the runtime executes, and the result comes back as
// turnResultMsg.
func (m *tuiModel) submit() tea.Cmd {
	if m.inTurn || m.dialog != nil {
		return nil
	}
	if m.runner == nil {
		m.banner = "erro: runner nao configurado"
		return nil
	}
	line := strings.TrimSpace(m.input)
	if line == "" {
		return nil
	}
	if cmd, isCmd := parseCommand(m.input); isCmd {
		return m.handleCommand(m.input, cmd)
	}
	m.session = append(m.session, "> "+redact.Redact(strings.TrimSpace(line)))
	m.session = capLines(m.session, maxSessionLines)
	m.promptHist = append(m.promptHist, redact.Redact(line))
	prompt := line
	m.resetPrompt()
	m.inTurn = true
	ctx, cancel := context.WithCancel(context.Background())
	m.turnCancel = cancel
	return func() tea.Msg {
		out, err := m.runner.run(ctx, prompt)
		return turnResultMsg{out: out, err: err}
	}
}

// handleCommand executes a slash-command using the SAME classifier the REPL
// uses (shared parser, one intent). Output goes onto the session banner/panel
// and the footer fields so prompt↔UI stay in parity.
func (m *tuiModel) handleCommand(raw string, cmd command) tea.Cmd {
	switch cmd.kind {
	case cmdExit:
		return tea.Quit
	case cmdHelp:
		m.session = append(m.session, redact.Redact(helpText))
	case cmdReset:
		m.session = nil
		m.session = append(m.session, "session (painel) resetado")
	case cmdHistory:
		if m.agent != nil {
			for i, p := range m.agent.PromptList() {
				m.session = append(m.session, secLine(i+1, p))
			}
		} else {
			m.session = append(m.session, "historico indisponivel")
		}
	case cmdContext:
		m.refreshUsage()
		if m.usage.Pct < 0 {
			m.session = append(m.session, "ctx: off ("+itoa(m.usage.SentBytes)+" bytes)")
		} else {
			m.session = append(m.session, "ctx: "+itoa(m.usage.Pct)+"% ("+itoa(m.usage.SentBytes)+"/"+itoa(m.usage.Budget)+" bytes)")
		}
	case cmdModel:
		if cmd.arg == "" {
			if m.agent != nil {
				m.model = m.agent.CurrentModel()
			}
			m.session = append(m.session, "model: "+m.model)
		} else if m.agent != nil {
			if err := m.agent.SwitchModel(cmd.arg); err != nil {
				m.banner = "erro: " + err.Error()
			} else {
				m.model = m.agent.CurrentModel()
				m.session = append(m.session, "model: "+m.model+" (vale nesta sessao)")
			}
		}
	case cmdProvider:
		if cmd.arg == "" {
			if m.agent != nil {
				m.provider = m.agent.CurrentProviderBaseURL()
			}
			m.session = append(m.session, "provider base_url: "+m.provider)
		} else if m.agent != nil {
			if err := m.agent.SwitchProvider(cmd.arg); err != nil {
				m.banner = "erro: " + err.Error()
			} else {
				m.provider = m.agent.CurrentProviderBaseURL()
				m.session = append(m.session, "provider: "+strings.ToLower(strings.TrimSpace(cmd.arg))+" ativo (vale nesta sessao)")
			}
		}
	case cmdRetry, cmdContinue:
		m.banner = ""
		m.inTurn = true
		ctx, cancel := context.WithCancel(context.Background())
		m.turnCancel = cancel
		return func() tea.Msg {
			out, err := m.runner.cont(ctx)
			return turnResultMsg{out: out, err: err, retry: true}
		}
	case cmdResend:
		if cmd.index <= 0 || m.agent == nil {
			m.banner = "erro: sem prompt !" + itoa(cmd.index) + " (historico vazio)"
			return nil
		}
		p, ok := m.agent.PromptAt(cmd.index - 1)
		if !ok {
			m.banner = "erro: sem prompt !" + itoa(cmd.index)
			return nil
		}
		m.session = append(m.session, "> "+p)
		m.promptHist = append(m.promptHist, p)
		m.resetPrompt()
		m.inTurn = true
		ctx, cancel := context.WithCancel(context.Background())
		m.turnCancel = cancel
		pline := p
		return func() tea.Msg {
			out, err := m.runner.run(ctx, pline)
			return turnResultMsg{out: out, err: err}
		}
	}
	m.session = capLines(m.session, maxSessionLines)
	return nil
}

// secLine renders a numbered history/session helper line.
func secLine(i int, s string) string {
	return itoa(i) + ": " + s
}

// truncatedLine caps one output line to the session panel bound.
func truncatedLine(s string) string {
	r := []rune(s)
	if len(r) > lineCap {
		return string(r[:lineCap-1]) + "\u2026"
	}
	return s
}

// View renders the whole TUI screen against the model state (spec 09 layout:
// session panel, activity panel, prompt window, divider, gate, footer).
func (m *tuiModel) View() string {
	info := footerInfo{
		Version:  m.version,
		Provider: m.provider,
		Model:    m.model,
		Toolbox:  m.toolbox,
		Session:  m.sessionName,
		Status:   m.statusTone(),
		Usage:    m.usage,
	}
	sess := capLines(m.session, maxSessionLines)
	redacted := make([]string, len(sess))
	for i, l := range sess {
		redacted[i] = redact.Redact(l)
	}
	act := capLines(m.activity, maxActivityLines)
	gateTxt := ""
	if m.dialog != nil {
		gateTxt = "[aprovar] " + m.dialog.Tool + " | y aprova, n nega, d nega sempre"
	}
	return buildView(viewData{
		Session:  redacted,
		Activity: act,
		Prompt:   m.input,
		Cursor:   m.cursor,
		Footer:   buildFooter(info),
		Width:    m.width,
		GateText: gateTxt,
	})
}

// runTUI is the M9 delivery entrypoint. It builds the Bubble Tea program over
// the shared agent/audit/config, wires the Event observer and the gate bridge
// into tea messages, and blocks until the operator quits. The REPL and this
// TUI consume the exact same core (DI: the loop never knows the frontend).
//
// Wiring notes (spec 09 §4 "nenhuma execucao por atalho"):
//   - The approver is the *tuiGate built by main: only y/n/d in the dialog
//     resolve a gate.Decide; there is no other approval code path.
//   - The observer (loop.SetObserver) forwards redacted Events onto the
//     activity panel via tea.Program.Send — never by poking the loop.
func runTUI(agent *loop.Loop, gate *tuiGate, cfg *config.Config, version, sessionName string) {
	m := &tuiModel{
		agent:       agent,
		gate:        gate,
		runner:      loopRunner{agent: agent},
		usageFn:     agent.ContextUsage,
		version:     version,
		provider:    cfg.Provider,
		model:       agent.CurrentModel(),
		toolbox:     cfg.ToolboxMode,
		sessionName: sessionName,
		width:       80,
		promptHist:  agent.PromptList(),
		activity:    []string{},
		session:     []string{},
		usage:       agent.ContextUsage(),
	}
	m.resetPrompt()
	if m.provider == "" {
		m.provider = "default"
	}

	p := tea.NewProgram(m, tea.WithAltScreen())

	agent.SetObserver(func(ev loop.Event) { p.Send(agentEventMsg{ev: ev}) })
	go bridgeGate(gate, p)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
	}
}

// bridgeGate forwards tuiGate dialogs (opened/closed) into the bubbletea event
// loop. The gate lives on the loop's turn goroutine; the bridge is the only
// channel that touches the UI.
func bridgeGate(gate *tuiGate, p *tea.Program) {
	for {
		select {
		case open, ok := <-gate.opened():
			if !ok {
				return
			}
			p.Send(gateOpenMsg{dialog: open})
		case _, ok := <-gate.closed():
			if !ok {
				return
			}
			p.Send(gateClosedMsg{})
		}
	}
}
