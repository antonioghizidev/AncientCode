// Pure projection helpers for the M9 TUI (spec 09-m9-tui.md §3/§5). This
// module is stdlib-only and deterministic on purpose: footer, session and
// activity panels are built from redacted inputs and are fully unit-testable
// on any host without a terminal. Bubble Tea never touches this file — the
// render boundary lives in tui.go (View).
package main

import (
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
		return "* gate: " + ev.Tool
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
	Session  []string
	Activity []string
	Prompt   string
	Cursor   int
	Footer   []string
	Width    int
	GateText string
}

// buildView lays out the TUI: session panel, activity panel, prompt window,
// divider, and footer. Pure string projection — deterministic for a given
// input (tested host-side); Bubble Tea only calls it from View.
func buildView(v viewData) string {
	if v.Width <= 0 {
		v.Width = 120
	}
	var b strings.Builder
	if len(v.Session) > 0 {
		b.WriteString("== sessao ==")
	}
	for _, l := range truncated(v.Session, v.Width/2) {
		b.WriteString("\n" + l)
	}
	if len(v.Activity) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("== atividade ==")
		for _, l := range truncated(v.Activity, v.Width/2) {
			b.WriteString("\n" + l)
		}
	}
	b.WriteString("\n" + strings.Repeat("-", v.Width))
	if v.GateText != "" {
		b.WriteString("\n" + v.GateText)
	}
	b.WriteString("\n> " + v.Prompt)
	for i := 0; i < v.Cursor; i++ {
		b.WriteString(" ")
	}
	b.WriteString("\xA0")
	for _, l := range v.Footer {
		b.WriteString("\n" + l)
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
