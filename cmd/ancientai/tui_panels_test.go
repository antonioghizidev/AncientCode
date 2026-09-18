package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ancientai/internal/audit"
	"ancientai/internal/loop"
)

func TestCtxBarCells(t *testing.T) {
	cases := []struct {
		pct  int
		want string
	}{
		{-1, "--"},
		{0, "\u2591\u2591\u2591\u2591\u2591"},
		{1, "\u2593\u2591\u2591\u2591\u2591"},
		{20, "\u2593\u2591\u2591\u2591\u2591"},
		{21, "\u2593\u2593\u2591\u2591\u2591"},
		{42, "\u2593\u2593\u2593\u2591\u2591"},
		{80, "\u2593\u2593\u2593\u2593\u2591"},
		{100, "\u2593\u2593\u2593\u2593\u2593"},
	}
	for _, c := range cases {
		if got := ctxBar(c.pct); got != c.want {
			t.Errorf("ctxBar(%d) = %q, want %q", c.pct, got, c.want)
		}
	}
}

func TestBuildFooter(t *testing.T) {
	usage := loop.Usage{SentBytes: 8400, Budget: 20000, Pct: 42}
	got := buildFooter(footerInfo{
		Version:  "0.9.0-m9",
		Provider: "openrouter",
		Model:    "model-x",
		Toolbox:  "run",
		Session:  "default.jsonl",
		Status:   statusOK,
		Usage:    usage,
	})
	if len(got) != 2 {
		t.Fatalf("footer must have 2 lines, got %d: %q", len(got), got)
	}
	if got[0] != "ancientai 0.9.0-m9 | openrouter | model-x | run | default.jsonl" {
		t.Errorf("line1 = %q", got[0])
	}
	if got[1] != "ctx \u2593\u2593\u2593\u2591\u2591 42% | \u25c9 ok" {
		t.Errorf("line2 = %q", got[1])
	}
}

func TestBuildFooterOffAndError(t *testing.T) {
	got := buildFooter(footerInfo{Version: "v", Status: statusErr, Usage: loop.Usage{SentBytes: 5, Pct: -1}})
	if got[1] != "ctx -- | \u2717 erro" {
		t.Errorf("off/erro line = %q", got[1])
	}
	got = buildFooter(footerInfo{Version: "v", Status: statusWait, Usage: loop.Usage{Pct: 0}})
	if got[1] != "ctx \u2591\u2591\u2591\u2591\u2591 0% | \u25e6 espera" {
		t.Errorf("espera line = %q", got[1])
	}
}

func TestFooterWarnOver80(t *testing.T) {
	got := buildFooter(footerInfo{Version: "v", Status: statusOK, Usage: loop.Usage{SentBytes: 100, Budget: 100, Pct: 100}})
	if !strings.Contains(got[1], "100%") {
		t.Errorf("pct missing: %q", got[1])
	}
	if got[1][len(got[1])-1] != '!' {
		t.Errorf("high-usage marker missing: %q", got[1])
	}
}

func TestFooterTruncatesModel(t *testing.T) {
	got := buildFooter(footerInfo{
		Version:  "0.9.0-m9",
		Provider: "nvidia",
		Model:    "nvidia/nemotron-3.5-lightning-30b-a3b",
		Toolbox:  "run",
		Session:  "very-long-session-id.jsonl",
		Status:   statusOK,
		Usage:    loop.Usage{Pct: 0},
	})
	// Line 1 must be bounded (header hygiene); line 2 always short.
	for _, l := range got {
		if len(l) > 90 {
			t.Errorf("footer line over 90 chars: %q", l)
		}
	}
}

func TestSessionLineRendering(t *testing.T) {
	if got := sessLine("user", "oi"); got != "> oi" {
		t.Errorf("user = %q", got)
	}
	if got := sessLine("assistant", "vou executar"); got != "< vou executar" {
		t.Errorf("assistant = %q", got)
	}
	if got := sessLine("info", "nota"); got != "  nota" {
		t.Errorf("info = %q", got)
	}
}

func TestActivityFromEvent(t *testing.T) {
	now := time.Now()
	evs := []loop.Event{
		{Phase: loop.PhaseModelWaiting, At: now},
		{Phase: loop.PhaseModelStreaming, At: now},
		{Phase: loop.PhaseGateWaiting, Tool: "write_file", At: now},
		{Phase: loop.PhaseToolRunning, Tool: "shell", At: now},
		{Phase: loop.PhaseTurnOK, Tool: "shell", Dur: 300 * time.Millisecond, At: now},
		{Phase: loop.PhaseTurnError, Tool: "apt", Detail: "erro [REDACTED]", At: now},
		{Phase: loop.PhaseProviderSwitch, Tool: "provider", Detail: "provider: nvidia", At: now},
	}
	want := []string{
		"* modelo: aguardando o primeiro token...",
		"* modelo: recebendo tokens...",
		"* gate: write_file",
		"* tool: shell (executando)",
		"* tool: shell ok 0.30s",
		"* apt: erro [REDACTED]",
		"* provider: nvidia",
	}
	for i, ev := range evs {
		if got := activityFromEvent(ev); got != want[i] {
			t.Errorf("event[%d] = %q, want %q", i, got, want[i])
		}
	}
}

func TestActivityFromEventRedactsEcho(t *testing.T) {
	// Security: the activity projection must only reflect the ALREADY-redacted
	// Detail carried by the event. Even a contaminated Detail (raw token) must
	// be re-redacted defense-in-depth before hitting the screen.
	const secret = "sk-abcdefGHIJKLMNOPQRSTUVWXyz123456"
	ev := loop.Event{Phase: loop.PhaseTurnError, Tool: "write_file", Detail: secret}
	got := activityFromEvent(ev)
	if strings.Contains(got, secret) || strings.Contains(got, "sk-abcdefGHIJKLMNOPQ") {
		t.Errorf("activity leaked raw secret: %q", got)
	}
}

func TestReadAuditTail(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "audit.jsonl")
	rec, err := audit.NewRecorder(p, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := rec.Record(audit.Entry{Tool: "shell", Args: "echo x", Status: "executed", Policy: "ask", Decision: "approved", OutputHash: "h"}); err != nil {
			t.Fatal(err)
		}
	}
	rec.Close()

	ents, err := readAuditTail(p, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 3 {
		t.Fatalf("tail len = %d, want 3", len(ents))
	}
	if ents[0].Tool != "shell" {
		t.Errorf("first tail entry = %+v", ents[0])
	}
}

func TestReadAuditTailNilWhenMissingOrEmpty(t *testing.T) {
	if _, err := readAuditTail(filepath.Join(t.TempDir(), "missing.jsonl"), 3); err != nil {
		t.Errorf("missing file must be nil-noop, got %v", err)
	}
	if _, err := readAuditTail("", 3); err != nil {
		t.Errorf("empty path must be nil-noop, got %v", err)
	}
}

func TestProjectionLayoutContainsPanels(t *testing.T) {
	// Deterministic snapshot (View strings): session lines on the left,
	// activity on the right, prompt and footer below.
	layout := buildView(viewData{
		Session:  []string{"> oi", "< vou executar", "  gate: write_file"},
		Activity: []string{"* modelo: recebendo tokens...", "* tool: shell ok"},
		Prompt:   "escreva algo",
		Cursor:   11,
		Footer:   []string{"| ancientai | run |", "ctx --"},
		Width:    80,
	})
	if !strings.Contains(layout, "> oi") {
		t.Errorf("session panel missing: %q", layout)
	}
	if !strings.Contains(layout, "* tool: shell ok") {
		t.Errorf("activity panel missing: %q", layout)
	}
	if !strings.Contains(layout, "escreva algo") {
		t.Errorf("prompt missing: %q", layout)
	}
	if !strings.Contains(layout, "ctx --") {
		t.Errorf("footer missing: %q", layout)
	}
}

func TestCapLines(t *testing.T) {
	in := []string{"a", "b", "c", "d"}
	got := capLines(in, 2)
	if len(got) != 2 || got[0] != "c" || got[1] != "d" {
		t.Fatalf("capLines = %v", got)
	}
	if len(capLines(in, 10)) != 4 {
		t.Fatalf("capLines must not grow")
	}
}

func TestEnvNoTUIOnPipe(t *testing.T) {
	// --tui must be refused on non-char-device stdin (pipe/--run), never a
	// silent fallback to the TUI. canUseTUI(false, true) must report no.
	if canUseTUI(false, true) {
		t.Error("canUseTUI accepted a non-TTY stdin")
	}
	if !canUseTUI(true, true) {
		t.Error("canUseTUI rejected a real TTY pair")
	}
}
