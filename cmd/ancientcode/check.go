package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/antonioghizidev/ancientcode/internal/catalog"
	"github.com/antonioghizidev/ancientcode/internal/loop"
)

// modelChecker is the slice of the loop this package needs, so the --check
// helpers are testable without a provider and without spending quota.
type modelChecker interface {
	CheckModels(ctx context.Context, entries []catalog.Entry, idxs ...int) []loop.CheckOutcome
}

// checkConsentMessage states the real cost before the operator approves it.
//
// "1 request per model" without a number is how a 40-model page turns into 40
// surprise bills. The count is computed from the page the operator is actually
// looking at, not from the whole catalog.
func checkConsentMessage(pg catalog.Page) string {
	return fmt.Sprintf("/models --check vai enviar %d requisicao(oes) reais (1 por modelo desta pagina) e gastar tokens. Continuar? [y/N] ",
		len(pg.Entries))
}

// runCheckPage verifies the entries of the CURRENT page.
//
// It takes the SOURCE slice, not the filtered one, and that is load-bearing.
// catalog.Filter returns copies, so verdicts written into the filtered slice die
// with it — and the REPL re-renders from the source right after checking, which
// would silently drop every "verificado" tag it just paid for.
//
// It takes the page rather than the whole source on purpose. /models shows 40
// of 377: probing all 377 because the operator asked to check "the list" would
// spend nine times what the confirmation promised, which is the same class of
// surprise the consent exists to prevent.
func runCheckPage(ctx context.Context, ag modelChecker, entries []catalog.Entry, pg catalog.Page) ([]loop.CheckOutcome, error) {
	idxs := make([]int, 0, len(pg.Entries))
	for _, e := range pg.Entries {
		if idx := indexOfEntry(entries, e.Info.ID); idx >= 0 {
			idxs = append(idxs, idx)
		}
	}
	if len(idxs) == 0 {
		return nil, nil
	}

	out := ag.CheckModels(ctx, entries, idxs...)

	// An unsupported provider is a capability gap, not a per-model finding: 40
	// identical rows would bury it. Detect it from the outcomes instead of
	// probing one entry twice up front, which would double-bill the first
	// model just to learn something the sweep already reveals.
	for _, o := range out {
		if errors.Is(o.Err, loop.ErrCheckUnsupported) {
			return nil, loop.ErrCheckUnsupported
		}
	}
	return out, nil
}

// runCheckPageIfConfirmed asks, and only probes on an explicit yes. The bool is
// the answer; anything but "y" is a no, so a stray Enter spends nothing.
func runCheckPageIfConfirmed(ctx context.Context, ag modelChecker, entries []catalog.Entry, pg catalog.Page, confirmed bool) ([]loop.CheckOutcome, bool) {
	if !confirmed {
		return nil, false
	}
	out, err := runCheckPage(ctx, ag, entries, pg)
	if err != nil {
		return nil, false
	}
	return out, true
}

// indexOfEntry finds a model in the filtered list by ID. IDs are unique per
// provider catalog; a duplicate would be a provider bug, and returning the
// first match is the conservative choice.
func indexOfEntry(entries []catalog.Entry, id string) int {
	for i := range entries {
		if entries[i].Info.ID == id {
			return i
		}
	}
	return -1
}

// checkOutcomeLines renders one line per probed model.
//
// The per-model line is the difference between "verification ran" and
// "verification happened": 40 probes that all silently failed look exactly like
// 40 probes that worked until the operator re-runs and compares.
func checkOutcomeLines(entries []catalog.Entry, pg catalog.Page, out []loop.CheckOutcome) []string {
	lines := make([]string, 0, len(out))
	for _, o := range out {
		if o.Index < 0 || o.Index >= len(entries) {
			continue
		}
		e := entries[o.Index]
		num := (pg.Number-1)*pg.PageSize + pageOffsetOf(pg, e.Info.ID) + 1
		var verdict string
		switch {
		case o.Err != nil && errors.Is(o.Err, loop.ErrCheckInconclusive):
			verdict = "inconclusivo"
		case o.Err != nil:
			verdict = "erro: " + o.Err.Error()
		case o.Check == catalog.CheckOK:
			verdict = "verificado (tool call real)"
		default:
			verdict = "sem tool calling (respondeu sem tool_calls)"
		}
		lines = append(lines, fmt.Sprintf("  %3d. %s: %s", num, e.Info.ID, verdict))
	}
	return lines
}

func pageOffsetOf(pg catalog.Page, id string) int {
	for i, e := range pg.Entries {
		if e.Info.ID == id {
			return i
		}
	}
	return 0
}

// fakeChecker records probed IDs and can force per-model probe failures.
type fakeChecker struct {
	probed  map[string]bool
	fail    map[string]error
	noProbe bool
	// results overrides the verdict per model; nil means "supports tools".
	results map[string]catalog.Check
}

func (f *fakeChecker) CheckModel(ctx context.Context, entries []catalog.Entry, i int) error {
	if f.noProbe {
		return loop.ErrCheckUnsupported
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.probed[entries[i].Info.ID] = true
	return f.fail[entries[i].Info.ID]
}

func (f *fakeChecker) CheckModels(ctx context.Context, entries []catalog.Entry, idxs ...int) []loop.CheckOutcome {
	out := make([]loop.CheckOutcome, 0, len(idxs))
	for _, i := range idxs {
		o := loop.CheckOutcome{Index: i, Check: entries[i].Check}
		if f.noProbe {
			o.Err = loop.ErrCheckUnsupported
			out = append(out, o)
			continue
		}
		f.probed[entries[i].Info.ID] = true
		o.Err = f.fail[entries[i].Info.ID]
		switch {
		case o.Err != nil:
			// stays unverified
		case f.results != nil:
			entries[i].Check = f.results[entries[i].Info.ID]
			o.Check = entries[i].Check
		default:
			entries[i].Check = catalog.CheckOK
			o.Check = entries[i].Check
		}
		out = append(out, o)
	}
	return out
}
