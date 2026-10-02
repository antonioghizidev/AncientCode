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
		Width:    100,
		Height:   24,
	})
	plain := stripANSI(layout)
	if !strings.Contains(plain, "> oi") {
		t.Errorf("session panel missing: %q", layout)
	}
	if !strings.Contains(plain, "* tool: shell ok") {
		t.Errorf("activity panel missing: %q", layout)
	}
	if !strings.Contains(plain, "escreva algo") {
		t.Errorf("prompt missing: %q", layout)
	}
	if !strings.Contains(plain, "ctx --") {
		t.Errorf("footer missing: %q", layout)
	}
}

// stripANSI removes SGR escape sequences so assertions can match the visible
// text. The render is intentionally ANSI-decorated (focus color, caret).
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			j := i + 1
			for j < len(s) && (s[j] == '[' || (s[j] >= '0' && s[j] <= '9') || s[j] == 'm') {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func TestPromptLineKeepsFullTextWithCaretInside(t *testing.T) {
	// The caret cell is highlighted in-band; the text must NOT be truncated
	// or padded (M9 appended cursor-sized spaces, which broke mid-line edits).
	got := stripANSI(promptLine("escreva algo", 7))
	if !strings.Contains(got, "> escreva algo") {
		t.Errorf("prompt text truncated: %q", got)
	}
	if !strings.Contains(promptLine("escreva algo", 7), cursorOn) {
		t.Errorf("caret cell must be highlighted: %q", promptLine("escreva algo", 7))
	}
	// Caret at the end draws a trailing block after the text.
	if tail := stripANSI(promptLine("oi", 2)); !strings.Contains(tail, "> oi") {
		t.Errorf("trailing caret broken: %q", tail)
	}
}

func TestScrollbarColTrackAndThumb(t *testing.T) {
	// A scrolling panel shows a ░ track with the █ thumb on the mapped row.
	col := scrollbarCol(6, 3, true)
	if len(col) != 6 {
		t.Fatalf("column length = %d, want 6", len(col))
	}
	if col[3] != '█' {
		t.Errorf("thumb must sit on row 3, got %q", string(col))
	}
	for i, c := range col {
		if i != 3 && c != '░' {
			t.Errorf("row %d must be track ░, got %q", i, string(c))
		}
	}
	// A non-scrolling panel gets a blank column (no track, no thumb).
	blank := scrollbarCol(4, 0, false)
	for i, c := range blank {
		if c != ' ' {
			t.Errorf("non-scrollable column must be blank at %d, got %q", i, string(c))
		}
	}
}

func TestViewportForClamps(t *testing.T) {
	// viewportFor must clamp offsets into [0, total-visible] and never go
	// negative when the content fits entirely.
	cases := []struct {
		off, total, visible, delta, want int
	}{
		{0, 10, 5, -1, 0},  // at top, up stays at top
		{4, 10, 5, +1, 5},  // scroll down one
		{0, 10, 5, +50, 5}, // huge jump clamps to total-visible
		{5, 10, 5, -50, 0}, // huge jump back clamps to 0
		{0, 3, 5, +1, 0},   // content fits: offset invariant 0
		{3, 3, 0, +1, 0},   // zero visible & degenerate total: never negative
	}
	for _, c := range cases {
		if got := viewportFor(c.off, c.total, c.visible, c.delta); got != c.want {
			t.Errorf("viewportFor(%d,%d,%d,%d) = %d, want %d", c.off, c.total, c.visible, c.delta, got, c.want)
		}
	}
}

func TestScrollThumb(t *testing.T) {
	// Scrollbar is only meaningful when the content overflows the viewport.
	if _, ok := scrollThumb(0, 3, 5); ok {
		t.Error("scrollThumb must report not-scrollable when total <= visible")
	}
	// When scrollable, the thumb must stay inside the visible range.
	for off := 0; off <= 5; off++ {
		thumb, ok := scrollThumb(off, 10, 5)
		if !ok {
			t.Fatalf("scrollThumb(%d,10,5) must be scrollable", off)
		}
		if thumb < 0 || thumb >= 5 {
			t.Errorf("scrollThumb(%d,10,5) thumb=%d out of [0,5)", off, thumb)
		}
	}
	// End of content → thumb at the last visible row; start → row 0.
	if thumb, _ := scrollThumb(5, 10, 5); thumb != 4 {
		t.Errorf("scrollThumb at end = %d, want 4", thumb)
	}
	if thumb, _ := scrollThumb(0, 10, 5); thumb != 0 {
		t.Errorf("scrollThumb at start = %d, want 0", thumb)
	}
}

func TestPanelFrameDrawsBorders(t *testing.T) {
	// The panel layout must draw box-drawing borders and a centered title.
	got := buildView(viewData{
		Session:  []string{"linha-sessao"},
		Activity: []string{"linha-atividade"},
		Prompt:   "> teste",
		Cursor:   6,
		Footer:   []string{"footer1", "footer2"},
		Width:    60,
		Height:   12,
	})
	wantFrames := []string{"\u250c", "\u2510", "\u2514", "\u2518", "\u252c"} // ┌ ┐ └ ┘ ┬
	for _, r := range wantFrames {
		if !strings.Contains(got, r) {
			t.Errorf("panel frame missing box char %q", r)
		}
	}
	if !strings.Contains(got, "sessão") || !strings.Contains(got, "atividade") {
		t.Errorf("panel titles missing: %q", got)
	}
	if !strings.Contains(got, "linha-sessao") || !strings.Contains(got, "linha-atividade") {
		t.Errorf("panel content missing: %q", got)
	}
}

func TestPanelFrameFocusedTitleHighlighted(t *testing.T) {
	// The focused panel's title segment must be wrapped in a color escape; the
	// other panel stays plain. This is how the user sees where the viewport
	// lives.
	ansiSpan := func(view, word string) bool {
		// true iff `word` appears inside a \x1b[36m…\x1b[0m span (the whole
		// title segment is wrapped, dashes included).
		rest := view
		for {
			i := strings.Index(rest, "\x1b[36m")
			if i < 0 {
				return false
			}
			span := rest[i+len("\x1b[36m"):]
			j := strings.Index(span, "\x1b[0m")
			if j < 0 {
				return false
			}
			if strings.Contains(span[:j], word) {
				return true
			}
			rest = span[j:]
		}
	}

	vSession := buildView(viewData{
		Session: []string{"a"}, Activity: []string{"b"}, Prompt: "x",
		Footer: []string{"f1", "f2"}, Width: 60, Height: 12, Focus: panelSession,
	})
	if !ansiSpan(vSession, "sessão") {
		t.Errorf("focused session title not highlighted: %q", vSession)
	}
	if ansiSpan(vSession, "atividade") {
		t.Errorf("unfocused activity title must stay plain: %q", vSession)
	}
	if strings.Count(vSession, "\x1b[36m") != 1 {
		t.Errorf("exactly one panel may be highlighted: %q", vSession)
	}

	vActivity := buildView(viewData{
		Session: []string{"a"}, Activity: []string{"b"}, Prompt: "x",
		Footer: []string{"f1", "f2"}, Width: 60, Height: 12, Focus: panelActivity,
	})
	if !ansiSpan(vActivity, "atividade") {
		t.Errorf("focused activity title not highlighted: %q", vActivity)
	}
	if ansiSpan(vActivity, "sessão") {
		t.Errorf("unfocused session title must stay plain: %q", vActivity)
	}
}

func TestPanelWidthsKeepRatio(t *testing.T) {
	// M13: 70/30 — the conversation needs the room; activity carries short event
	// lines. The sum invariant is unchanged.
	sess, act := panelWidths(100)
	if sess != 70 || act != 30 {
		t.Errorf("panelWidths(100) = %d,%d, want 70,30", sess, act)
	}
	total := sess + act
	if total != 100 {
		t.Errorf("panel widths must sum to 100: %d", total)
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
