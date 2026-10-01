// Pure projection helpers for the M9 TUI (spec 09-m9-tui.md §3/§5). This
// module is stdlib-only and deterministic on purpose: footer, session and
// activity panels are built from redacted inputs and are fully unit-testable
// on any host without a terminal. Bubble Tea never touches this file — the
// render boundary lives in tui.go (View).
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"ancientai/internal/audit"
	"ancientai/internal/loop"
	"ancientai/internal/redact"
)

// statusTone marks the footer health indicator (D-37 vocabulary).
type statusTone int

const (
	statusOK   statusTone = iota // ok
	statusWait                   // espera (gate/modelo)
	statusErr                    // erro de turno
)

// panelID selects which of the two viewport panels owns the ↑↓/PgUp/PgDn/
// scroll keys. The prompt keeps keyboard focus for typing regardless; the
// "focus" here only routes the viewport navigation (M10, D-2026-09-30-50).
type panelID int

const (
	panelPrompt   panelID = iota // arrow keys edit the line (history)
	panelSession                 // ↑↓ scroll the session viewport
	panelActivity                // ↑↓ scroll the activity viewport
)

const (
	focusCyan = "\x1b[36m" // focused panel title highlight (ANSI SGR 36)
	focusEnd  = "\x1b[0m"
	focusMark = "▸"       // focus marker, visible without color support
	cursorOn  = "\x1b[7m" // reverse video on the caret cell
	cursorOff = "\x1b[27m"
	errOn     = "\x1b[31m" // error banner (M11)
	errOff    = "\x1b[0m"
	gateOn    = "\x1b[33m" // approval prompt: what is about to be executed (M11)
	gateOff   = "\x1b[0m"
)

// footerInfo is everything the footer projection needs (deterministic).
type footerInfo struct {
	Version  string
	Provider string
	Model    string
	Toolbox  string
	Session  string
	Status   statusTone
	Usage    loop.Usage
}

const (
	footerBarCells = 5
	footerWarnPct  = 80
	lineCap        = 90
)

// ctxBar renders the context meter as a 5-cell bar (▓ filled / ░ empty).
// Pct < 0 (budget 0 → trimming off) is shown as "--" (D-37).
func ctxBar(pct int) string {
	if pct < 0 {
		return "--"
	}
	filled := (pct + 19) / 20 // ceil(pct/20): 1 cell per 20%
	if filled > footerBarCells {
		filled = footerBarCells
	}
	return strings.Repeat("\u2593", filled) + strings.Repeat("\u2591", footerBarCells-filled)
}

// buildFooter returns the two footer lines (state + meter, D-37). Line 1
// carries version|provider model|mode|session; line 2 the ctx bar and health.
// The model name and session are truncated to keep the header bounded.
func buildFooter(f footerInfo) []string {
	line1 := fmt.Sprintf("ancientai %s | %s | %s | %s", f.Version, f.Provider, f.Model, f.Toolbox)
	if f.Session != "" {
		line1 += " | " + f.Session
	}
	if len(line1) > lineCap {
		line1 = line1[:lineCap-2] + ".."
	}

	statusTxt := "\u25c9 ok" // ok
	switch f.Status {
	case statusWait:
		statusTxt = "\u25e6 espera"
	case statusErr:
		statusTxt = "\u2717 erro"
	}
	line2 := "ctx " + ctxBar(f.Usage.Pct)
	if f.Usage.Pct >= 0 {
		line2 += fmt.Sprintf(" %d%%", f.Usage.Pct)
	}
	line2 += " | " + statusTxt
	if f.Usage.Pct > footerWarnPct {
		line2 += " !"
	}
	if len(line2) > lineCap {
		line2 = line2[:lineCap]
	}
	return []string{line1, line2}
}

// sessLine renders one session-panel line from a role marker.
func sessLine(role, text string) string {
	switch role {
	case "user":
		return "> " + text
	case "assistant":
		return "< " + text
	default:
		return "  " + text
	}
}

// activityFromEvent projects one redacted loop event into an activity-panel
// line (D-35 vocabulary). Only a redacted-ish Detail is ever echoed; raw
// secrets cannot appear because the loop redacts before emission and this
// function never re-classifies content.
func activityFromEvent(ev loop.Event) string {
	var head string
	switch ev.Phase {
	case loop.PhaseModelWaiting:
		return "* modelo: aguardando o primeiro token..."
	case loop.PhaseModelStreaming:
		return "* modelo: recebendo tokens..."
	case loop.PhaseGateWaiting:
		// M11: the operator must see WHAT is being authorized, not just the
		// tool name (the args come from the loop already redacted).
		head := "* gate: " + ev.Tool
		if detail := gateSummaryFor(ev.Tool, ev.Detail); detail != "" {
			head += "  " + sanitize(redact.Redact(detail))
		}
		return head
	case loop.PhaseToolRunning:
		return "* tool: " + ev.Tool + " (executando)"
	case loop.PhaseTurnOK:
		head = "* tool: " + ev.Tool + " ok"
		if ev.Dur > 0 {
			head += fmt.Sprintf(" %.2fs", ev.Dur.Seconds())
		}
		return head
	case loop.PhaseTurnError:
		head = "* " + ev.Tool
		if head == "* " {
			head = "* erro"
		}
		if ev.Detail != "" {
			head += ": " + redact.Redact(ev.Detail)
		}
		return head
	case loop.PhaseProviderSwitch:
		head = "* "
		if ev.Detail != "" {
			head += redact.Redact(ev.Detail)
		} else {
			head += "provider"
		}
		return head
	default:
		return "* (fase desconhecida)"
	}
}

// readAuditTail reads the last n entries of the append-only audit (M4). It is
// a no-op (nil, nil) for a missing file so the activity panel degrades
// silently on first run. Entries carry already-redacted args.
func readAuditTail(path string, n int) ([]audit.Entry, error) {
	if path == "" || n <= 0 {
		return nil, nil
	}
	ents, err := audit.ReadEntries(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(ents) > n {
		ents = ents[len(ents)-n:]
	}
	return ents, nil
}

// viewData is the deterministic render input (View projection).
type viewData struct {
	Session     []string
	Activity    []string
	Prompt      string
	Cursor      int
	Footer      []string
	Width       int
	Height      int
	Focus       panelID
	SessionOff  int // viewport top offset for the session panel
	ActivityOff int // viewport top offset for the activity panel
	GateLines   []string
	Banner      string // error/info line shown above the prompt (M11)
}

// panelWidths splits the total width into the two panel allocations: session
// gets ~60%, activity the rest. The two allocations always sum back to width
// (both frames share the middle vertical border). Minimums keep the frames
// legible on tiny terminals.
func panelWidths(width int) (sess, act int) {
	sess = width * 6 / 10
	min := 12
	if sess < min {
		sess = min
	}
	act = width - sess
	if act < min {
		act = min
		sess = width - act
	}
	if sess < 1 {
		sess = 1
	}
	return sess, act
}

// panelInnerWidths returns the content widths for session and activity panels
// given the frame allocations from panelWidths. Layout: left border (1) +
// session inner + shared border (1) + activity inner + right border (1) ==
// width. The shared border is already counted in the session allocation, so
// the activity inner absorbs the extra cell.
func panelInnerWidths(width int) (sess, act int) {
	sessFrame, actFrame := panelWidths(width)
	sess = sessFrame - 2
	act = actFrame - 1 // shared border lives in the session allocation
	return sess, act
}

// viewportFor clamps a suggested offset into the scrollable range of a panel
// of `total` lines shown in `visible` rows. Content that fits entirely keeps
// offset 0 (nothing to scroll).
func viewportFor(off, total, visible, delta int) int {
	if total <= visible || visible <= 0 {
		return 0
	}
	maxOff := total - visible
	if delta < 0 {
		off += delta
		if off < 0 {
			return 0
		}
		return off
	}
	off += delta
	if off > maxOff {
		return maxOff
	}
	return off
}

// scrollThumb maps an offset into the visible row that should carry the thumb
// (0 = top ... visible-1 = bottom). It reports false when the panel does not
// scroll (content fits). The thumb is a proportional indicator: a panel loaded
// to the end puts the thumb on the last visible row.
func scrollThumb(off, total, visible int) (row int, scrollable bool) {
	if total <= visible || visible <= 0 {
		return 0, false
	}
	maxOff := total - visible
	row = 0
	if maxOff > 0 {
		row = off * (visible - 1) / maxOff
	}
	return row, true
}

// scrollbarCol renders the scrollbar column for one panel: a `░` track with
// the `█` thumb on the row given by scrollThumb. A panel whose content fits
// gets a blank column (no track, no thumb) so the text width never jumps.
func scrollbarCol(visible, thumbRow int, scrollable bool) []rune {
	col := make([]rune, visible)
	for i := range col {
		col[i] = ' '
	}
	if !scrollable {
		return col
	}
	for i := range col {
		col[i] = '░'
	}
	if thumbRow >= 0 && thumbRow < visible {
		col[thumbRow] = '█'
	}
	return col
}

// sanitize strips terminal control sequences from UNTRUSTED text before it
// reaches the screen (M11, security).
//
// Model output and tool arguments are data, never terminal commands. Without
// this, a tool returning "\x1b[2J" could erase the approval dialog, and an OSC
// sequence could retitle the window or hide the very line the operator is being
// asked to approve — the gate would stop being trustworthy. `redact` only masks
// secrets; it does not touch control characters.
//
// C0 (0x00–0x1F), DEL (0x7F) and C1 (0x80–0x9F, which includes 8-bit CSI/ESC)
// are removed; a space replaces tab/newline/CR so panel geometry is preserved.
// Printable text — including non-ASCII and box-drawing — is untouched.
func sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f:
			// dropped: control characters carry no readable meaning here
		case r >= 0x80 && r <= 0x9f:
			// C1: 8-bit CSI/SOS/PM/APC, can escape sequences too
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sanitizeWidth is sanitize plus a hard cell cap, for lines that go into a
// fixed-width panel cell (no clipping marker: the panel edge is the boundary).
func sanitizeWidth(s string, width int) string {
	return runeCap(sanitize(s), width)
}

// gateDetail turns the Approver summary ("<tool> <args>") into the lines the
// operator reads before approving (M11). Approving a tool whose payload is
// hidden is not informed consent, so the decisive argument is surfaced: for
// shell that is the command line itself, not a JSON blob.
//
// Output is re-redacted (defense in depth — the loop already redacts) and
// sanitized (the payload is untrusted), then bounded by width and maxLines.
func gateDetail(summary string, width, maxLines int) []string {
	if maxLines <= 0 {
		maxLines = 1
	}
	tool, args, _ := strings.Cut(summary, " ")
	detail := sanitize(redact.Redact(gateSummaryFor(strings.TrimSpace(tool), args)))
	if detail == "" {
		return nil
	}
	return wrapText(detail, width, maxLines)
}

// gateArgKeys are the decisive argument keys per tool, in display order. The
// names come from each tool's JSON schema (internal/tools/*), so the gate shows
// the real payload instead of guessing a field name.
var gateArgKeys = map[string][]string{
	"shell":         {"cmd"},
	"docker":        {"subcommand"},
	"apt":           {"action"},
	"write_file":    {"path"},
	"read_file":     {"path"},
	"list_dir":      {"path"},
	"edit_file":     {"path"},
	"code_review":   {"path"},
	"code_scaffold": {"template", "name"},
	"skill_run":     {"skill", "script"},
}

// gateListKeys are list-valued arguments appended after the decisive key, so
// `docker ps -a` and `apt install nginx` read as real command lines.
var gateListKeys = map[string][]string{
	"docker":    {"args"},
	"apt":       {"packages"},
	"skill_run": {"argv"},
}

// gateFallbackKeys are tried (in order) for a tool the UI does not know yet, so
// a new tool still gets a readable gate instead of raw JSON.
var gateFallbackKeys = []string{"cmd", "command", "path", "template", "action", "subcommand", "skill", "script", "name"}

// gateSummaryFor renders the tool args as a compact one-line description.
// Falls back to the raw args when nothing recognizable is found (never hides
// the payload silently).
func gateSummaryFor(tool, args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return ""
	}
	m := map[string]any{}
	if !strings.HasPrefix(args, "{") || json.Unmarshal([]byte(args), &m) != nil {
		return args
	}
	keys, known := gateArgKeys[tool]
	if !known {
		keys = gateFallbackKeys
	}
	parts := make([]string, 0, 4)
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			parts = append(parts, s)
		}
	}
	for _, k := range gateListKeys[tool] {
		arr, ok := m[k].([]any)
		if !ok || len(arr) == 0 {
			continue
		}
		items := make([]string, 0, len(arr))
		for _, it := range arr {
			if s, ok := it.(string); ok {
				items = append(items, s)
			}
		}
		if len(items) > 0 {
			parts = append(parts, strings.Join(items, " "))
		}
	}
	if len(parts) == 0 {
		return args // unrecognized shape: show it raw rather than hide it
	}
	return strings.Join(parts, " ")
}

// wrapText hard-wraps s to width cells, returning at most maxLines lines. The
// last line is ellipsized when the text did not fit, so the operator can tell
// the view was cut (never a silent truncation of what they are approving).
func wrapText(s string, width, maxLines int) []string {
	if width <= 0 {
		width = 1
	}
	if maxLines <= 0 {
		maxLines = 1
	}
	r := []rune(s)
	lines := make([]string, 0, maxLines)
	cut := false
	for i := 0; i < len(r); i += width {
		if len(lines) == maxLines {
			cut = true
			break
		}
		end := i + width
		if end >= len(r) {
			lines = append(lines, string(r[i:]))
			break
		}
		lines = append(lines, string(r[i:end]))
	}
	if cut && len(lines) > 0 {
		last := []rune(lines[len(lines)-1])
		if len(last) > 0 {
			lines[len(lines)-1] = string(last[:len(last)-1]) + "…"
		}
	}
	return lines
}

// fixedRows is the number of screen rows the TUI reserves BELOW the two panels
// (divider + prompt + 2 footer lines, plus one row each for an open gate dialog
// and for the error banner). Single source of truth so the projection and the
// scroll navigation agree on how tall the panels are.
func fixedRows(gateOpen, banner bool) int {
	n := 4 // divider + prompt + footer(2)
	if gateOpen {
		n++
	}
	if banner {
		n++
	}
	return n
}

// centered returns a title segment of exactly `n` cells with `─` fill and the
// title centered, e.g. centered("sessão", 14) → "─── sessão ─────".
func centered(title string, n int) string {
	t := []rune(title)
	if n < 0 {
		n = 0
	}
	if len(t) >= n {
		return string(t[:n])
	}
	fill := n - len(t)
	left := fill / 2
	right := fill - left
	return strings.Repeat("─", left) + string(t) + strings.Repeat("─", right)
}

// runeCap truncates s to at most n runes.
func runeCap(s string, n int) string {
	if n < 0 {
		n = 0
	}
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// padRight pads s with trailing spaces to exactly n cells (truncating if
// longer).
func padRight(s string, n int) string {
	s = runeCap(s, n)
	r := []rune(s)
	return s + strings.Repeat(" ", n-len(r))
}

// renderPanels draws the lazydocker-style double panel (M10): session on the
// left (~60%), activity on the right, sharing one vertical border, each with
// a box-drawing frame and a scrollbar column on its inner right edge. It
// returns `rows` lines: top border + content rows + bottom border. The
// focused panel's title is rendered in the highlight color.
//
//	┌── sessão ───────────┬── atividade ────────┐
//	│ > oi               │ │ ● modelo: recebendo │
//	│ < vou executar     │ │ tool: shell ok 0.0  │
//	│                    █│                   ░ │
//	└────────────────────┴──────────────────────┘
func renderPanels(sess, act []string, sessOff, actOff, width, rows int, focus panelID) []string {
	if width < 8 {
		width = 8
	}
	if rows < 3 {
		rows = 3
	}
	sessInner, actInner := panelInnerWidths(width)

	// Top border: title segments joined by the shared vertical border ┬. The
	// focused panel gets a ▸ marker plus the highlight color so the scroll
	// target is obvious even without color.
	sessTerm := panelTitle("sessão", sessInner, focus == panelSession)
	actTerm := panelTitle("atividade", actInner, focus == panelActivity)
	top := "┌" + sessTerm + "┬" + actTerm + "┐"

	bottom := "└" + strings.Repeat("─", sessInner) + "┴" + strings.Repeat("─", actInner) + "┘"

	visible := rows - 2
	sessBody, actBody := sessInner-1, actInner-1
	sThumb, sScroll := scrollThumb(sessOff, len(sess), visible)
	aThumb, aScroll := scrollThumb(actOff, len(act), visible)
	sBar := scrollbarCol(visible, sThumb, sScroll)
	aBar := scrollbarCol(visible, aThumb, aScroll)

	lines := make([]string, 0, rows)
	lines = append(lines, top)
	for i := 0; i < visible; i++ {
		sCell := runeCap(sanitize(lineAt(sess, sessOff+i)), sessBody)
		aCell := runeCap(sanitize(lineAt(act, actOff+i)), actBody)
		lines = append(lines,
			"│"+padRight(sCell, sessBody)+string(sBar[i])+"│"+
				padRight(aCell, actBody)+string(aBar[i])+"│")
	}
	lines = append(lines, bottom)
	return lines
}

// panelTitle renders one panel's top-border segment: an optional ▸ marker for
// the focused panel, the title centered over a `─` fill, wrapped in the
// highlight color when focused.
func panelTitle(title string, n int, focused bool) string {
	label := title
	if focused {
		label = focusMark + " " + title
	}
	seg := centered(label, n)
	if focused {
		return focusCyan + seg + focusEnd
	}
	return seg
}

// lineAt returns lines[i] or "" for out-of-range indexes.
func lineAt(lines []string, i int) string {
	if i < 0 || i >= len(lines) {
		return ""
	}
	return lines[i]
}

// buildView lays out the TUI (M10): the two lazydocker-style panels on top
// (session left, activity right, box-drawing frames + scrollbars), a divider,
// the gate line when a dialog is open, the prompt window, and the footer.
// Pure string projection — deterministic for a given input (tested host-side);
// Bubble Tea only calls it from View.
func buildView(v viewData) string {
	if v.Width <= 0 {
		v.Width = 120
	}
	if v.Height <= 0 {
		v.Height = 30
	}
	// Fixed rows below the panels come from the shared budget (see fixedRows)
	// so the drawn height and the scroll navigation never diverge.
	panelRows := v.Height - fixedRows(len(v.GateLines) > 0, v.Banner != "")
	if panelRows < 3 {
		panelRows = 3 // top border + 1 content row + bottom border
	}

	panels := renderPanels(v.Session, v.Activity, v.SessionOff, v.ActivityOff, v.Width, panelRows, v.Focus)

	var b strings.Builder
	for i, l := range panels {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(l)
	}
	b.WriteString("\n" + strings.Repeat("─", v.Width))
	for _, l := range v.GateLines {
		b.WriteString("\n" + l)
	}
	if v.Banner != "" {
		b.WriteString("\n" + errOn + v.Banner + errOff)
	}
	b.WriteString("\n" + promptLine(v.Prompt, v.Cursor))
	for _, l := range v.Footer {
		b.WriteString("\n" + l)
	}
	return b.String()
}

// promptLine renders the input row: "> " + the full typed text with the cursor
// cell highlighted (ANSI reverse video). Bubble Tea hides the real cursor in
// altscreen, so the block is drawn in-band: the character under the cursor
// stays visible and the text is never truncated.
func promptLine(text string, cursor int) string {
	r := []rune(text)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(r) {
		cursor = len(r)
	}
	var b strings.Builder
	b.WriteString("> ")
	for i, ch := range r {
		if i == cursor {
			b.WriteString(cursorOn)
			b.WriteRune(ch)
			b.WriteString(cursorOff)
			continue
		}
		b.WriteRune(ch)
	}
	if cursor == len(r) {
		b.WriteString(cursorOn)
		b.WriteRune('\xA0') // trailing block when the caret is past the text
		b.WriteString(cursorOff)
	}
	return b.String()
}

// truncated keeps the last max lines and clips each line to max width for a
// bounded projection.
func truncated(lines []string, width int) []string {
	if width <= 0 {
		return lines
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		r := []rune(l)
		if len(r) > width {
			l = string(r[:width-1]) + "\u2026"
		}
		out = append(out, l)
	}
	return out
}

// capLines keeps only the newest max lines (panel scrollback bound).
func capLines(in []string, max int) []string {
	if len(in) <= max {
		return in
	}
	return in[len(in)-max:]
}

// canUseTUI reports whether --tui may run: BOTH stdin and stdout must be real
// char devices. Pipes, files and --run captions are refused with a clear error
// by main (spec 09 §4: "tui só roda TTY; pipe/--run rejeitam --tui").
func canUseTUI(stdinTTY, stdoutTTY bool) bool {
	return stdinTTY && stdoutTTY
}
