// M12: the model picker panel (D-2026-10-01-53).
//
// A dedicated third panel, not the activity log: choosing a model is a distinct
// task with its own keyboard, and mixing a choice list into the event log would
// make both harder to read. While the picker is open it owns the keyboard —
// typing narrows the list instead of reaching the prompt, which is what makes a
// 464-model catalog (OpenRouter) usable at all.
package main

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"ancientai/internal/catalog"
)

// catalogTimeout bounds the catalog fetch: the picker must never hang waiting
// on a slow provider, and the context is cancelled when the picker closes.
const catalogTimeout = 20 * time.Second

// picker is the open model-selection panel.
type picker struct {
	all     []catalog.Entry // the whole catalog (unfiltered)
	vis     []catalog.Entry // what the current filters select
	opts    catalogOpts     // filters chosen on the command line
	cur     int             // highlighted position inside vis
	off     int             // first visible row of the panel
	free    bool            // "free only" toggle
	active  string          // model currently in use
	loading bool            // catalog request in flight
	// freeAvailable is false when the provider publishes no pricing at all
	// (NVIDIA NIM). The footer then says so instead of implying an empty list
	// is a meaningful answer.
	freeAvailable bool
}

// pickerMsg carries the fetched catalog back into the Bubble Tea loop.
type pickerMsg struct {
	entries []catalog.Entry
	err     error
}

// pickerClosedMsg tells the model the panel is gone.
type pickerClosedMsg struct{ selected string }

// openPickerCmd fetches the catalog off the Update goroutine so the UI stays
// responsive (same pattern as the turn commands).
func openPickerCmd(agent *tuiModel) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
		defer cancel()
		entries, err := agent.agent.ListModels(ctx)
		return pickerMsg{entries: entries, err: err}
	}
}

// pickerRows is the maximum rows the picker panel shows; the panel scrolls.
const pickerRows = 14

// visible recomputes the filtered slice and keeps the cursor in range.
func (p *picker) visible() {
	opts := p.opts
	opts.free = p.free
	p.vis = catalog.Filter(p.all, catalogOptsOf(opts))
	p.vis = catalog.Rank(p.vis, p.active)
	if p.cur >= len(p.vis) {
		p.cur = len(p.vis) - 1
	}
	if p.cur < 0 {
		p.cur = 0
	}
	if p.off > p.cur {
		p.off = p.cur
	}
	maxOff := len(p.vis) - pickerRows
	if maxOff < 0 {
		maxOff = 0
	}
	if p.off > maxOff {
		p.off = maxOff
	}
	if p.cur < p.off {
		p.off = p.cur
	}
}

// move moves the highlight by delta, scrolling the window to follow.
func (p *picker) move(delta int) {
	if len(p.vis) == 0 {
		p.cur = 0
		return
	}
	p.cur += delta
	if p.cur < 0 {
		p.cur = 0
	}
	if p.cur >= len(p.vis) {
		p.cur = len(p.vis) - 1
	}
	if p.cur < p.off {
		p.off = p.cur
	}
	if p.cur >= p.off+pickerRows {
		p.off = p.cur - pickerRows + 1
	}
}

// appendRune adds a character to the search query and re-filters.
func (p *picker) appendRune(r rune) {
	p.opts.query += strings.ToLower(string(r))
	p.cur = 0
	p.off = 0
	p.visible()
}

// backspace removes one character from the query and re-filters.
func (p *picker) backspace() {
	r := []rune(p.opts.query)
	if len(r) == 0 {
		return
	}
	p.opts.query = string(r[:len(r)-1])
	p.cur = 0
	p.off = 0
	p.visible()
}

// selected is the model under the cursor, or nil when the list is empty.
func (p *picker) selected() (catalog.Entry, bool) {
	if p.cur < 0 || p.cur >= len(p.vis) {
		return catalog.Entry{}, false
	}
	return p.vis[p.cur], true
}

// pickerLines renders the panel body: the visible slice with the highlighted row
// marked, plus the query line and the counters. Pure projection (no terminal).
func pickerLines(p *picker) []string {
	out := make([]string, 0, pickerRows+2)
	if p.loading {
		// Never claim "no models match" before the catalog arrives: an empty
		// list that has not been fetched is not a filtered-out result.
		out = append(out, "  carregando catalogo de modelos…")
		out = append(out, "  Esc cancela   Ctrl+C cancela")
		return out
	}
	if p.opts.query == "" && !p.free {
		out = append(out, "  filtrando: digite para buscar   Ctrl+F: so gratuitos")
	} else {
		q := p.opts.query
		if q == "" {
			q = "(sem busca)"
		}
		freeTag := ""
		if p.free {
			freeTag = "  [so gratuitos]"
		}
		out = append(out, "  filtrando: "+q+freeTag+"   Ctrl+F: limpar filtro")
	}
	for i := p.off; i < len(p.vis) && i < p.off+pickerRows; i++ {
		marker := "  "
		if i == p.cur {
			marker = "> "
		}
		out = append(out, marker+catalogRow(i, p.vis[i], p.active))
	}
	if len(p.vis) == 0 {
		out = append(out, "  (nenhum modelo corresponde)")
	}
	// Counters: what is shown, what the provider could tell us, and — crucially —
	// whether a "free only" filter is even meaningful for this provider.
	note := catalogNote(p.all, p.opts)
	if !p.freeAvailable && p.free {
		note = "este provedor nao informa preco: --free nao se aplica"
	}
	out = append(out, "  "+note)
	out = append(out, "  Enter seleciona   Esc cancela   Ctrl+C cancela   ↑↓ move")
	return out
}
