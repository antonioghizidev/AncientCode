package main

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"ancientai/internal/loop"
)

// The TUI Model must map each key to the correct core command (spec 09 §5).
// These tests drive Update with synthetic messages only — no terminal. The
// turn runner is injected so tests are deterministic and never touch network.

// fakeRunner records a submitted prompt (or continue) and returns canned output.
type fakeRunner struct {
	turns     []string
	continues int
	out       string
	err       error
}

func (f *fakeRunner) run(ctx context.Context, user string) (string, error) {
	f.turns = append(f.turns, user)
	return f.out, f.err
}

func (f *fakeRunner) cont(ctx context.Context) (string, error) {
	f.continues++
	if f.err != nil {
		return "", f.err
	}
	return f.out, nil
}

func mkModel(t *testing.T, ag *loop.Loop) *tuiModel {
	t.Helper()
	m := &tuiModel{
		agent:       ag,
		gate:        newTuiGate(),
		version:     "0.9.0-m9",
		toolbox:     "run",
		sessionName: "default.jsonl",
		width:       100,
	}
	m.resetPrompt()
	return m
}

func TestModelSubmitRunsTurn(t *testing.T) {
	m := mkModel(t, nil)
	f := &fakeRunner{out: "resposta", err: nil}
	m.runner = f
	m.input = "oi"
	m.cursor = len(m.input)

	var gotCmd tea.Cmd
	m, gotCmd = upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	if gotCmd == nil {
		t.Fatal("Enter must return a turn command")
	}
	if len(f.turns) != 0 {
		t.Fatal("runner must only be called by the command, synchronously")
	}
	// The command is invoked by the Bubble Tea runtime: call it in-process and
	// feed its result back through Update, exactly as the runtime would.
	msg := gotCmd()
	m, _ = upd(m, msg)
	if gotCmd != nil && !sessHas(m, "< resposta") {
		t.Errorf("session panel missing assistant response: %v", m.session)
	}
}

func TestModelRetryKeyCallsContinue(t *testing.T) {
	m := mkModel(t, nil)
	f := &fakeRunner{out: "re-run", err: nil}
	m.runner = f

	// retry is only meaningful after a failed turn (banner set)
	m.banner = "erro: boom"

	var gotCmd tea.Cmd
	m, gotCmd = upd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if gotCmd == nil {
		t.Fatal("r with banner must return continue command")
	}
	msg := gotCmd()
	m, _ = upd(m, msg)
	if f.continues != 1 {
		t.Fatalf("Continue called %d times, want 1", f.continues)
	}
	if !sessHas(m, "< re-run") {
		t.Errorf("session panel missing retry response: %v", m.session)
	}
}

func TestModelRetryWithoutBannerIsNoop(t *testing.T) {
	m := mkModel(t, nil)
	m.runner = &fakeRunner{out: "x", err: nil}
	var gotCmd tea.Cmd
	m, gotCmd = upd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if gotCmd != nil {
		t.Fatal("r without error banner must not trigger retry (a normal letter)")
	}
	if !strings.Contains(m.input, "r") {
		t.Errorf("r should have been typed into the input: %q", m.input)
	}
}

func TestModelClearBannerKey(t *testing.T) {
	m := mkModel(t, nil)
	m.banner = "erro: alguma coisa"
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if m.banner != "" {
		t.Errorf("c must clear the error banner, got %q", m.banner)
	}
}

func TestModelCtrlCDuringTurnCancels(t *testing.T) {
	m := mkModel(t, nil)
	canceled := false
	m.turnCancel = func() { canceled = true }
	m.inTurn = true
	m, cmds := upd(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if !canceled {
		t.Error("Ctrl+C during turn must cancel the turn context")
	}
	// must NOT quit the app
	if cmds != nil {
		t.Errorf("Ctrl+C cancel must not return Quit: %v", cmds)
	}
	if m.inTurn {
		t.Error("inTurn must be cleared after cancel")
	}
}

func TestModelCtrlCDuringGateDenies(t *testing.T) {
	m := mkModel(t, nil)
	m.gate = newTuiGate()
	done := make(chan gateResult, 1)
	go func() { d, e := m.gate.Decide(context.Background(), "shell", "shell id"); done <- gateResult{d, e} }()
	open := <-m.gate.opened()

	m, _ = upd(m, gateOpenMsg{dialog: open})
	if m.dialog != open {
		t.Fatal("dialog not open after gateOpenMsg")
	}
	m, cmds := upd(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if findQuit(cmds) {
		t.Error("Ctrl+C inside a gate must deny the call, not quit")
	}
	r := <-done
	if r.Decision != loop.DecisionDeny {
		t.Fatalf("Ctrl+C during gate must deny: %v", r.Decision)
	}
	if m.dialog != nil {
		t.Error("dialog must be cleared after Ctrl+C deny")
	}
}

func TestModelCtrlCAtPromptQuits(t *testing.T) {
	m := mkModel(t, nil)
	m, cmds := upd(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if q := findQuit(cmds); !q {
		t.Errorf("Ctrl+C at idle prompt must quit (Quit cmd), got %v", cmds)
	}
}

func TestModelHistoryUpDown(t *testing.T) {
	m := mkModel(t, nil)
	m.promptHist = []string{"ola", "tudo bem"}
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.input != "tudo bem" {
		t.Errorf("Up must load newest history entry, got %q", m.input)
	}
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.input != "ola" {
		t.Errorf("Up must load older entry, got %q", m.input)
	}
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.input != "tudo bem" {
		t.Errorf("Down must step forward, got %q", m.input)
	}
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.input != "" {
		t.Errorf("Down past newest must restore empty line, got %q", m.input)
	}
}

func TestModelSlashCommandSwitchesModel(t *testing.T) {
	// /model/ /provider/ /context must reach the same pure parser (commands.go)
	// the REPL uses (§5 unified parser test).
	cmd, ok := parseCommand("/model  some/model")
	if !ok || cmd.kind != cmdModel || cmd.arg != "some/model" {
		t.Fatalf("parseCommand(/model ...) = %+v ok=%v", cmd, ok)
	}
	cmd, ok = parseCommand("/provider openrouter")
	if !ok || cmd.kind != cmdProvider || cmd.arg != "openrouter" {
		t.Fatalf("parseCommand(/provider) = %+v ok=%v", cmd, ok)
	}
	cmd, ok = parseCommand("/context")
	if !ok || cmd.kind != cmdContext {
		t.Fatalf("parseCommand(/context) = %+v ok=%v", cmd, ok)
	}
	// unknown slash is a command classified by the same parser (REPL shows help)
	cmd, ok = parseCommand("/bogus")
	if !ok || cmd.kind != cmdHelp {
		t.Fatalf("parseCommand(/bogus) = %+v ok=%v", cmd, ok)
	}
}

func TestModelViewRendersFooterAndPanels(t *testing.T) {
	m := mkModel(t, nil)
	m.session = []string{"> oi", "< resposta"}
	m.activity = []string{"* modelo: recebendo tokens..."}
	m.input = "nova msg"
	m.cursor = len(m.input)
	m.usage = loop.Usage{SentBytes: 8400, Budget: 20000, Pct: 42}
	view := m.View()
	if !strings.Contains(view, "> oi") {
		t.Errorf("session missing: %q", view)
	}
	if !strings.Contains(view, "* modelo: recebendo tokens...") {
		t.Errorf("activity missing: %q", view)
	}
	if !strings.Contains(view, "nova msg") {
		t.Errorf("prompt missing: %q", view)
	}
	if !strings.Contains(view, "ancientai 0.9.0-m9") {
		t.Errorf("footer missing: %q", view)
	}
	if !strings.Contains(view, "42%") {
		t.Errorf("ctx pct missing: %q", view)
	}
}

func TestModelViewHidesRawSecret(t *testing.T) {
	// security: a session line that somehow carried a token must still end up
	// re-redacted via the shared projection (defense-in-depth).
	const secret = "sk-abcdefGHIJKLMNOPQRSTUVWXyz123456"
	m := mkModel(t, nil)
	m.session = []string{"> " + secret, "< resposta"}
	m.usage = loop.Usage{Pct: -1}
	view := m.View()
	if strings.Contains(view, secret) {
		t.Errorf("view leaked secret: %q", view)
	}
}

// --- helpers -----------------------------------------------------------------

// upd runs a message through Model.Update and unwraps the tea.Model back to the
// concrete *tuiModel (the interface's result is the Bubble Tea contract; tests
// then keep working on the concrete type).
func upd(m *tuiModel, msg tea.Msg) (*tuiModel, tea.Cmd) {
	model, cmd := m.Update(msg)
	return model.(*tuiModel), cmd
}

func findQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	// tea.Quit is a function; calling it returns tea.QuitMsg.
	if msg := cmd(); msg != nil {
		_, ok := msg.(tea.QuitMsg)
		return ok
	}
	return false
}

func sessHas(m *tuiModel, want string) bool {
	for _, l := range m.session {
		if l == want {
			return true
		}
	}
	return false
}
