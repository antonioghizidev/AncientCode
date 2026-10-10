// M12: model catalog plumbing for the frontends (D-2026-10-01-53).
//
// The loop owns the *semantics* ("which models are usable here", "switch to
// this one") so the REPL and the TUI cannot drift apart; the transport lives in
// internal/provider and the classification/filtering in internal/catalog.
package loop

import (
	"context"
	"errors"
	"fmt"

	"github.com/antonioghizidev/ancientcode/internal/catalog"
	"github.com/antonioghizidev/ancientcode/internal/provider"
)

// modelLister is the optional provider capability for listing the catalog.
// Providers that cannot list keep working: the command then fails closed with a
// clear message instead of pretending the catalog is empty.
type modelLister interface {
	ListModels(ctx context.Context) ([]provider.ModelInfo, error)
}

// modelProber is the optional capability for layer-3 verification (`--check`).
type modelProber interface {
	ToolProbe(ctx context.Context, model string) (provider.ToolProbeResult, error)
}

// ErrCatalogUnsupported is returned when the active provider cannot list models.
var ErrCatalogUnsupported = errors.New("loop: provider atual nao expoe catalogo de modelos")

// ListModels returns the provider catalog classified for this harness.
//
// Fail-closed by construction: it only ever *reads*. Switching stays an explicit
// SwitchModel call, so a failed listing cannot change which model is active.
func (l *Loop) ListModels(ctx context.Context) ([]catalog.Entry, error) {
	ml, ok := l.p.(modelLister)
	if !ok {
		return nil, ErrCatalogUnsupported
	}
	models, err := ml.ListModels(ctx)
	if err != nil {
		// The provider already redacts its own errors; wrap without adding the
		// model list or any request detail.
		return nil, fmt.Errorf("loop: falha ao listar modelos: %w", err)
	}
	if len(models) == 0 {
		return nil, errors.New("loop: catalogo de modelos vazio")
	}
	return catalog.Build(models), nil
}

// SelectModel switches to the model at index i of entries (the last listing the
// operator saw). Refuses anything out of range so a stale number cannot switch
// to an unintended model (fail-closed, D-37 semantics kept).
func (l *Loop) SelectModel(entries []catalog.Entry, i int) error {
	if i < 0 || i >= len(entries) {
		return fmt.Errorf("loop: indice %d fora da lista (%d modelos); nada foi trocado", i+1, len(entries))
	}
	return l.SwitchModel(entries[i].Info.ID)
}

// ErrCheckUnsupported is returned when the provider cannot run tool probes.
var ErrCheckUnsupported = errors.New("loop: provedor atual nao expoe verificacao de tool calling")

// ErrCheckInconclusive marks a probe that did not reach a verdict (truncated
// response, cancelled context). Distinct from a probe failure so the UI can say
// "inconclusivo" instead of implying a negative answer about the model.
var ErrCheckInconclusive = errors.New("loop: verificacao inconclusiva")

// CheckModel verifies ONE entry with a real tool-calling request and records
// the verdict on it.
//
// The mapping matters more than the plumbing. "Provedor respondeu 200" is NOT
// "o modelo tem tool calling": a chat model that answers in prose satisfies the
// first and fails the second, and the whole reason --check exists is that the
// catalog metadata cannot tell them apart. So only an actual tool_calls block
// counts as a pass, and a probe that FAILED to run leaves the entry unverified
// rather than recording a negative — a network error is not a verdict about a
// model, and writing one would show up in the ranking as fact.
func (l *Loop) CheckModel(ctx context.Context, entries []catalog.Entry, i int) error {
	pr, ok := l.p.(modelProber)
	if !ok {
		return ErrCheckUnsupported
	}
	if i < 0 || i >= len(entries) {
		return fmt.Errorf("loop: indice %d fora da lista (%d modelos); nada foi verificado", i+1, len(entries))
	}

	res, err := pr.ToolProbe(ctx, entries[i].Info.ID)
	if err != nil {
		return fmt.Errorf("loop: sonda de tool calling falhou para %s: %w", entries[i].Info.ID, err)
	}
	if res.Truncated {
		// Ran out of read budget: unknowable, not negative.
		return fmt.Errorf("%w: resposta truncada para %s; nada foi alterado", ErrCheckInconclusive, entries[i].Info.ID)
	}
	if res.SupportsTools {
		entries[i].Check = catalog.CheckOK
		return nil
	}
	entries[i].Check = catalog.CheckFailed
	return nil
}

// CheckOutcome is the per-model result of a sweep, so the frontend can print a
// line for each. Without it, 40 probes and 40 silent no-ops look identical.
type CheckOutcome struct {
	Index int
	Check catalog.Check
	Err   error
}

// CheckModels verifies several entries, stopping at the first context
// cancellation so one Escape does not spend the rest of the page's quota.
//
// It deliberately keeps going after a per-model probe error: a 404 on one
// retired model (measured on NVIDIA, whose catalog leads with retired IDs)
// says nothing about the other 39 on the page. Cancelling is the operator
// asking to stop, so that alone halts the sweep.
func (l *Loop) CheckModels(ctx context.Context, entries []catalog.Entry, idxs ...int) []CheckOutcome {
	out := make([]CheckOutcome, 0, len(idxs))
	for _, i := range idxs {
		if ctx.Err() != nil {
			for _, rest := range idxs[len(out):] {
				out = append(out, CheckOutcome{Index: rest, Check: catalog.CheckUnverified, Err: ctx.Err()})
			}
			return out
		}
		o := CheckOutcome{Index: i, Check: entries[i].Check}
		o.Err = l.CheckModel(ctx, entries, i)
		o.Check = entries[i].Check
		out = append(out, o)
	}
	return out
}
