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
