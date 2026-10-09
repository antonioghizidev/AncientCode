// M12: shared rendering of the model catalog (D-2026-10-01-53).
//
// Pure stdlib projection, so both frontends show the exact same row format and
// the REPL numbering matches the TUI picker positions. Every string here is
// provider-supplied and was already redacted + sanitized in
// internal/provider (defense in depth: a second pass here costs nothing).
package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/antonioghizidev/ancientcode/internal/catalog"
	"github.com/antonioghizidev/ancientcode/internal/redact"
)

// catalogNote explains, per catalog, what could and could not be verified. The
// honest part matters: on NVIDIA NIM nothing discloses capabilities, so the UI
// must not imply the list is "known good".
func catalogNote(all []catalog.Entry, opt catalogOpts) string {
	c := catalog.Summarize(all, catalog.Filter(all, catalogOptsOf(opt)))
	if !catalog.FreeFilterAvailable(all) {
		return "provedor nao informa preco: filtro --free indisponivel aqui"
	}
	return fmt.Sprintf("%d de %d visiveis | %d gratuitos | %d sem tool calling confirmado | %d nao-chat ocultos",
		c.Visible, c.Total, c.Free, c.Verified, c.NonChat)
}

// catalogOptsOf adapts the parsed command flags to the domain filter.
func catalogOptsOf(opt catalogOpts) catalog.Options {
	return catalog.Options{
		Query:          opt.query,
		FreeOnly:       opt.free,
		IncludeNonChat: opt.includeNonChat,
		RequireTools:   opt.agentViable,
		AgentViable:    opt.agentViable,
	}
}

// catalogRow renders one numbered row. The leading marker distinguishes the
// active model (*) from a verified one (✓); both are informational only.
func catalogRow(i int, e catalog.Entry, active string) string {
	marker := " "
	if e.Info.ID == active {
		marker = "*"
	}
	label := e.Info.Name
	if label == "" {
		label = e.Info.ID
	} else if label != e.Info.ID {
		label = e.Info.ID + " (" + label + ")"
	}
	tags := []string{e.Kind.String(), e.Tools.String()}
	if e.Info.Free() {
		tags = append(tags, "free")
	}
	if e.Info.ContextLength > 0 {
		tags = append(tags, "ctx "+strconv.Itoa(e.Info.ContextLength))
	}
	if e.Check == catalog.CheckOK {
		tags = append(tags, "verificado")
	}
	return fmt.Sprintf("%s %3d. %s  [%s]", marker, i+1,
		sanitizeLine(label), strings.Join(tags, ", "))
}

// catalogLines renders the visible slice plus a trailing note and the next
// action. Returns the lines and the filtered slice the numbers refer to (so a
// later "/model N" cannot drift onto a different list).
// catalogLines renders one PAGE of the filtered, ranked catalog.
//
// It used to take a `max` and stop with "... +N", which quietly denied access to
// the rest: the operator was told they existed and given no way to reach them.
// Now the remainder is reachable with /models --next / --prev (M14, after
// measuring 395 models that clear this harness's requirements).
func catalogLines(entries []catalog.Entry, opt catalogOpts, active string, page int) ([]string, []catalog.Entry, catalog.Page) {
	visible := catalog.Filter(entries, catalogOptsOf(opt))
	visible = catalog.Rank(visible, active)
	pg := catalog.Paginate(visible, page, catalog.DefaultPageSize)
	lines := make([]string, 0, len(pg.Entries)+4)
	for i, e := range pg.Entries {
		// The number shown is the index in the FULL filtered list, not in the
		// page: `/model <numero>` selects by that number, so it has to be the
		// number the operator actually sees.
		lines = append(lines, catalogRow((pg.Number-1)*pg.PageSize+i, e, active))
	}
	if len(visible) == 0 {
		lines = append(lines, "  (nenhum modelo corresponde ao filtro)")
	}
	if pg.TotalPages > 1 {
		lines = append(lines, fmt.Sprintf("  pagina %d de %d (%d modelos no total)   /models --next  /models --prev",
			pg.Number, pg.TotalPages, pg.Total))
	}
	lines = append(lines, "  "+catalogNote(entries, opt))
	lines = append(lines, "  selecione com /model <numero>")
	return lines, visible, pg
}

// sanitizeLine mirrors the TUI paint hardening for rows produced here (the REPL
// prints to a real terminal too, where an escape would be interpreted).
func sanitizeLine(s string) string { return sanitize(s) }

// modelIndexArg parses a /model argument that is a position in the last listing.
// ok is false when the argument is not a number, so the caller falls back to
// treating it as a model name.
func modelIndexArg(arg string) (int, bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return 0, false
	}
	n, err := strconv.Atoi(arg)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n - 1, true
}

// redactCatalogEcho is used when echoing provider text back into the session.
func redactCatalogEcho(s string) string { return redact.Redact(s) }
