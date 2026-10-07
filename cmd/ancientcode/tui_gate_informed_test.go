package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/antonioghizidev/ancientcode/internal/loop"
)

// Small helpers so the M11 tests read like the scenario, not like the plumbing.
func contextTODO() context.Context { return context.Background() }
func teaKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}
func enterKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }

var errBoom = errors.New("boom")

// --- M11: gate informado ------------------------------------------------------

func TestGateDetailExtractsCommand(t *testing.T) {
	// The loop hands the Approver "<tool> <args>". The decisive argument key
	// differs per tool (see each tool's JSON schema), so the gate must read the
	// real field — not guess "command". A shape it cannot read falls back to the
	// raw args rather than hiding the payload.
	cases := []struct {
		name    string
		summary string
		want    string
	}{
		{"shell", `shell {"cmd":"id -un"}`, "id -un"},
		{"shell rm", `shell {"cmd":"rm -rf /tmp/x"}`, "rm -rf /tmp/x"},
		{"docker", `docker {"subcommand":"ps","args":["-a"]}`, "ps -a"},
		{"apt", `apt {"action":"install","packages":["nginx"]}`, "install nginx"},
		{"write_file", `write_file {"path":"a/b.txt","content":"ola"}`, "a/b.txt"},
		{"edit_file", `edit_file {"path":"x.go","old":"a","new":"b"}`, "x.go"},
		{"scaffold", `code_scaffold {"template":"cli","name":"app"}`, "cli app"},
		{"skill_run", `skill_run {"skill":"smoke","script":"status.sh","argv":["-v"]}`, "smoke status.sh -v"},
		{"unknown tool", `weird_thing {"cmd":"zzz"}`, "zzz"},
		{"unrecognized", `shell {"foo":"bar"}`, `{"foo":"bar"}`},
		{"invalid json", `shell {nao e json}`, "{nao e json}"},
		{"empty", `shell `, ""},
	}
	for _, c := range cases {
		got := strings.Join(gateDetail(c.summary, 60, 2), "\n")
		if c.want == "" {
			if got != "" {
				t.Errorf("%s: gateDetail(%q) = %q, want nothing", c.name, c.summary, got)
			}
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: gateDetail(%q) = %q, want it to contain %q", c.name, c.summary, got, c.want)
		}
		if !strings.Contains(c.want, "{") && strings.Contains(got, "{") {
			t.Errorf("%s: gate must not show raw JSON when it can summarize: %q", c.name, got)
		}
	}
}

func TestGateDetailReadsRealShellSchemaKey(t *testing.T) {
	// Regression (found by the Inspiron smoke): shell's schema key is "cmd",
	// so a gate that looks only for "command" falls back to raw JSON and the
	// operator still cannot see the command line.
	got := strings.Join(gateDetail(`shell {"cmd":"docker ps -la"}`, 60, 2), "\n")
	if got != "docker ps -la" {
		t.Errorf("shell gate must show the command alone, got %q", got)
	}
	if strings.Contains(got, "{") || strings.Contains(got, "cmd") {
		t.Errorf("shell gate must not leak the JSON envelope: %q", got)
	}
}

func TestGateDetailBoundedByWidthAndLines(t *testing.T) {
	long := `shell {"cmd":"` + strings.Repeat("rm ", 200) + `-rf /tmp"}`
	got := gateDetail(long, 24, 2)
	if len(got) > 2 {
		t.Errorf("gateDetail must respect maxLines, got %d lines: %q", len(got), got)
	}
	for i, l := range got {
		if n := len([]rune(l)); n > 24 {
			t.Errorf("line %d over width: %d runes: %q", i, n, l)
		}
	}
}

func TestGateDetailRedactsSecretInCommand(t *testing.T) {
	// Security: a secret inside the command must never reach the gate.
	const secret = "sk-abcdefGHIJKLMNOPQRSTUVWXyz123456"
	summary := `shell {"cmd":"curl -H 'Authorization: ` + secret + `' https://api"}`
	got := strings.Join(gateDetail(summary, 80, 2), "\n")
	if strings.Contains(got, secret) {
		t.Errorf("gate leaked secret: %q", got)
	}
}

func TestGateDialogShowsCommand(t *testing.T) {
	m := mkModel(t, nil)
	m.gate = newTuiGate()
	done := make(chan gateResult, 1)
	go func() {
		d, e := m.gate.Decide(contextTODO(), "shell", `shell {"cmd":"docker ps -la"}`)
		done <- gateResult{d, e}
	}()
	open := <-m.gate.opened()
	m, _ = upd(m, gateOpenMsg{dialog: open})

	view := stripANSI(m.View())
	if !strings.Contains(view, "shell") {
		t.Errorf("gate must name the tool: %q", view)
	}
	if !strings.Contains(view, "docker ps -la") {
		t.Errorf("gate must show the command being authorized: %q", view)
	}
	if !strings.Contains(view, "y aprova") {
		t.Errorf("gate must keep the answer hints: %q", view)
	}
	m, _ = upd(m, teaKey('y'))
	if r := <-done; r.Decision != loop.DecisionApprove {
		t.Errorf("y must approve, got %v", r.Decision)
	}
}

func TestActivityGateLineShowsDetail(t *testing.T) {
	ev := loop.Event{
		Phase:  loop.PhaseGateWaiting,
		Tool:   "shell",
		Detail: `{"cmd":"docker ps -la"}`,
		At:     time.Now(),
	}
	got := activityFromEvent(ev)
	if !strings.Contains(got, "shell") {
		t.Errorf("activity gate line must name the tool: %q", got)
	}
	if !strings.Contains(got, "docker ps -la") {
		t.Errorf("activity gate line must show the command: %q", got)
	}
}

// --- M11: erro visível -------------------------------------------------------

func TestBannerIsRendered(t *testing.T) {
	m := mkModel(t, nil)
	m.banner = "erro: loop: aprovacao de \"shell\" expirou; recusado (fail-closed)"
	view := stripANSI(m.View())
	if !strings.Contains(view, "fail-closed") {
		t.Errorf("banner must be rendered on screen: %q", view)
	}
}

func TestTurnErrorGoesToSessionHistory(t *testing.T) {
	m := mkModel(t, nil)
	m.runner = &fakeRunner{err: errBoom}
	m.input = "pergunta"
	m.cursor = len(m.input)
	m, cmd := upd(m, enterKey())
	if cmd == nil {
		t.Fatal("Enter must return a turn command")
	}
	m = feedTurn(t, m, cmd)

	if m.banner == "" {
		t.Fatal("a failed turn must set the banner")
	}
	found := false
	for _, l := range m.session {
		if strings.HasPrefix(l, "! erro:") && strings.Contains(l, "boom") {
			found = true
		}
	}
	if !found {
		t.Errorf("failed turn must leave a line in the session history: %v", m.session)
	}
}

// --- M11: hardening do paint --------------------------------------------------

func TestSanitizeStripsControlSequences(t *testing.T) {
	// Security: model/tool output must not inject terminal control sequences
	// into the UI (erase the screen, spoof the gate, hide text).
	cases := []string{
		"\x1b[2Jshell docker ps",
		"\x1b]0;falso\x07shell",
		"antes\x00depois",
		"linha\r\rquebra",
		"\x07bell",
	}
	for _, in := range cases {
		got := sanitize(in)
		if strings.ContainsRune(got, 0x1b) {
			t.Errorf("sanitize kept ESC in %q → %q", in, got)
		}
		for _, r := range got {
			if r < 0x20 || r == 0x7f {
				t.Errorf("sanitize kept control %U in %q → %q", r, in, got)
			}
		}
	}
	if got := sanitize("\x1b[2Jshell"); !strings.Contains(got, "shell") {
		t.Errorf("sanitize must keep the readable text: %q", got)
	}
}

func TestViewNeverPaintsRawControlSequences(t *testing.T) {
	// Security (end-to-end): a contaminated session/activity line must not
	// inject terminal control sequences into the View. The render does emit
	// its own SGR (caret, focus, error), so the assertion targets the injected
	// sequences specifically, not the absence of ESC altogether.
	const (
		erase   = "\x1b[2J"
		redOn   = "\x1b[31mmascarado"
		reset   = "\x1b[0m"
		injEvil = "\x1b[2J\x1b[31mmascarado\x1b[0m"
	)
	m := mkModel(t, nil)
	m.session = []string{"< " + injEvil}
	m.activity = []string{"* " + injEvil}
	view := m.View()
	for _, bad := range []string{erase, redOn} {
		if strings.Contains(view, bad) {
			t.Errorf("View leaked injected sequence %q from untrusted content: %q", bad, view)
		}
	}
	// The reset from the injected payload must not survive either: our own
	// resets are emitted only around caret/banner cells, never after panel text.
	if n := strings.Count(view, reset); n > 4 {
		t.Errorf("unexpected number of resets (%d) — injected reset leaked: %q", n, view)
	}
	if !strings.Contains(stripANSI(view), "mascarado") {
		t.Errorf("View must still show the readable text: %q", view)
	}
}

func TestGateDetailSanitizes(t *testing.T) {
	// Security: the command shown in the gate is untrusted too.
	got := strings.Join(gateDetail(`shell {"command":"\u001b[2Jrm -rf /"}`, 40, 2), "\n")
	if strings.ContainsRune(got, 0x1b) {
		t.Errorf("gateDetail must sanitize the command: %q", got)
	}
}

func TestFixedRowsSharedByProjectionAndNavigation(t *testing.T) {
	// The panel height and the navigation viewport must agree, or the scroll
	// keys would move by a different amount than what is drawn.
	//
	// M13: the gate reservation is its REAL height, not a boolean — reserving one
	// row for a dialog that renders three pushed the prompt off the screen.
	for _, tc := range []struct {
		gate, banner bool
	}{
		{false, false}, {true, false}, {false, true}, {true, true},
	} {
		m := mkModel(t, nil)
		m.height = 30
		if tc.gate {
			m.dialog = &gateOpen{Tool: "shell"}
		}
		if tc.banner {
			m.banner = "erro: x"
		}
		want := fixedRows(len(m.gateLines()), tc.banner)
		panelRows := m.height - want
		if panelRows < 3 {
			panelRows = 3
		}
		if got := m.panelContentRows(); got != panelRows-2 {
			t.Errorf("gate=%v banner=%v: panelContentRows=%d, projection uses %d",
				tc.gate, tc.banner, got, panelRows-2)
		}
	}
}

// O gate tem que caber inteiro na tela:rompt, rodapé e pelo menos uma faixa de
// painel. Antes do M13 ele crescia sem reserva e empurrava o input para fora.
func TestOpenGateNeverPushesThePromptOffScreen(t *testing.T) {
	for _, h := range []int{14, 20, 30, 60} {
		m := mkModel(t, nil)
		m.height = h
		m.dialog = &gateOpen{Tool: "shell", Summary: "shell: " + strings.Repeat("df -h ", 60)}
		lines := strings.Split(m.View(), "\n")
		// divider + prompt + 2 footer rows must all be on screen.
		if len(lines) > h {
			t.Errorf("height %d: view rendered %d rows — the prompt was pushed off", h, len(lines))
		}
		if !strings.Contains(m.View(), "[aprovar] shell") {
			t.Errorf("height %d: the gate dialog itself is missing", h)
		}
		if !strings.Contains(m.View(), "> ") {
			t.Errorf("height %d: the input line is missing", h)
		}
	}
}
