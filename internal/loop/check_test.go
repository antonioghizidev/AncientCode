package loop

import (
	"context"
	"errors"
	"testing"

	"github.com/antonioghizidev/ancientcode/internal/catalog"
	"github.com/antonioghizidev/ancientcode/internal/provider"
)

// probeProvider records which models were probed, so a test can prove the loop
// probed exactly the requested entries and nothing else.
type probeProvider struct {
	models  []provider.ModelInfo
	results map[string]provider.ToolProbeResult
	errs    map[string]error
	calls   []string
}

func (p *probeProvider) Chat(ctx context.Context, msgs []provider.ChatMessage, tools []provider.ToolDef, onEvent func(provider.StreamEvent)) (provider.ChatResult, error) {
	return provider.ChatResult{}, errors.New("nao usado")
}

func (p *probeProvider) Model() string                { return "m" }
func (p *probeProvider) SetModel(m string) error      { return nil }
func (p *probeProvider) BaseURL() string              { return "https://example.invalid/v1" }
func (p *probeProvider) SetBaseURL(u, e string) error { return nil }

func (p *probeProvider) ListModels(context.Context) ([]provider.ModelInfo, error) {
	return p.models, nil
}

func (p *probeProvider) ToolProbe(ctx context.Context, model string) (provider.ToolProbeResult, error) {
	p.calls = append(p.calls, model)
	if err := p.errs[model]; err != nil {
		return provider.ToolProbeResult{Model: model}, err
	}
	return p.results[model], nil
}

func sampleModels(ids ...string) []provider.ModelInfo {
	out := make([]provider.ModelInfo, 0, len(ids))
	for _, id := range ids {
		out = append(out, provider.ModelInfo{ID: id})
	}
	return out
}

func probeLoop(t *testing.T, p *probeProvider) (*Loop, []catalog.Entry) {
	t.Helper()
	l := newTestLoopA(t, p, nil, nil, t.TempDir())
	entries, err := l.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return l, entries
}

// A real tool call marks that ONE entry verified, and leaves its neighbours alone.
func TestCheckModelMarksVerifiedOnRealToolCall(t *testing.T) {
	p := &probeProvider{
		models: sampleModels("a/one", "b/two"),
		results: map[string]provider.ToolProbeResult{
			"a/one": {SupportsTools: true, FinishReason: "tool_calls"},
			"b/two": {SupportsTools: false, FinishReason: "stop"},
		},
	}
	l, entries := probeLoop(t, p)

	if err := l.CheckModel(context.Background(), entries, 0); err != nil {
		t.Fatal(err)
	}
	if entries[0].Check != catalog.CheckOK {
		t.Errorf("Check = %v, esperava CheckOK", entries[0].Check)
	}
	if entries[1].Check != catalog.CheckUnverified {
		t.Errorf("o vizinho nao devia ser verificado: %v", entries[1].Check)
	}
}

// Prose is not tool support. A 200 must not read as a pass.
func TestCheckModelProseIsCheckFailed(t *testing.T) {
	p := &probeProvider{
		models:  sampleModels("a/prose"),
		results: map[string]provider.ToolProbeResult{"a/prose": {SupportsTools: false, FinishReason: "stop"}},
	}
	l, entries := probeLoop(t, p)

	if err := l.CheckModel(context.Background(), entries, 0); err != nil {
		t.Fatal(err)
	}
	if entries[0].Check != catalog.CheckFailed {
		t.Errorf("prosa veio %v, esperava CheckFailed", entries[0].Check)
	}
}

// A cut-off response is inconclusive, and the operator must be able to SEE that
// it was inconclusive: silently leaving the entry unverified makes a network or
// budget problem look exactly like "we never checked".
func TestCheckModelTruncatedReportsInconclusiveNotVerdict(t *testing.T) {
	p := &probeProvider{
		models:  sampleModels("a/huge"),
		results: map[string]provider.ToolProbeResult{"a/huge": {Truncated: true}},
	}
	l, entries := probeLoop(t, p)

	err := l.CheckModel(context.Background(), entries, 0)
	if !errors.Is(err, ErrCheckInconclusive) {
		t.Fatalf("truncado veio %v, esperava ErrCheckInconclusive", err)
	}
	if entries[0].Check != catalog.CheckUnverified {
		t.Fatalf("Check = %v, esperava CheckUnverified", entries[0].Check)
	}
}

// The sweep reports per-model outcomes so the REPL can print a line each.
// Swallowing errors would make 40 probes indistinguishable from 40 silent no-ops.
func TestCheckModelsReportsOutcomePerModel(t *testing.T) {
	p := &probeProvider{
		models: sampleModels("a/good", "a/prose", "a/ghost", "a/huge"),
		results: map[string]provider.ToolProbeResult{
			"a/good":  {SupportsTools: true},
			"a/prose": {SupportsTools: false},
			"a/huge":  {Truncated: true},
		},
		errs: map[string]error{"a/ghost": errors.New("HTTP 404")},
	}
	l, entries := probeLoop(t, p)

	got := l.CheckModels(context.Background(), entries, 0, 1, 2, 3)
	if len(got) != 4 {
		t.Fatalf("recebeu %d resultados, esperava 4", len(got))
	}
	if got[0].Check != catalog.CheckOK {
		t.Errorf("a/good = %v", got[0].Check)
	}
	if got[1].Check != catalog.CheckFailed {
		t.Errorf("a/prose = %v", got[1].Check)
	}
	if got[2].Check != catalog.CheckUnverified || got[2].Err == nil {
		t.Errorf("a/ghost deveria ter erro e ficar nao verificado: %+v", got[2])
	}
	if got[3].Check != catalog.CheckUnverified || !errors.Is(got[3].Err, ErrCheckInconclusive) {
		t.Errorf("a/huge deveria ser inconclusivo: %+v", got[3])
	}
}

// Cancelling mid-sweep must stop spending quota, and the already-verified
// results must still be reported.
func TestCheckModelsStopsMidSweepAndReportsWhatItDid(t *testing.T) {
	p := &probeProvider{models: sampleModels("a/1", "a/2", "a/3"), results: map[string]provider.ToolProbeResult{}}
	l, entries := probeLoop(t, p)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := l.CheckModels(ctx, entries, 0, 1, 2)

	if len(p.calls) != 0 {
		t.Fatalf("contexto cancelado nao deveria sondar nada, sondou %v", p.calls)
	}
	if len(got) != 3 {
		t.Fatalf("resultado por modelo = %d, esperava 3", len(got))
	}
	for i, o := range got {
		if o.Check != catalog.CheckUnverified {
			t.Errorf("modelo %d = %v, esperava nao verificado", i, o.Check)
		}
	}
}

// A probe failure is a fact about the PROBE, not about the model. Writing
// CheckFailed would present a network hiccup as a verdict the operator acts on.
func TestCheckModelProbeErrorDoesNotWriteFailed(t *testing.T) {
	p := &probeProvider{
		models: sampleModels("a/ghost"),
		errs:   map[string]error{"a/ghost": errors.New("probe: HTTP 404")},
	}
	l, entries := probeLoop(t, p)

	if err := l.CheckModel(context.Background(), entries, 0); err == nil {
		t.Fatal("erro de sonda deveria propagar")
	}
	if entries[0].Check != catalog.CheckUnverified {
		t.Fatalf("Check = %v, esperava CheckUnverified", entries[0].Check)
	}
}

// Out-of-range is refused, so a stale page number cannot verify the wrong model.
func TestCheckModelRefusesOutOfRange(t *testing.T) {
	p := &probeProvider{models: sampleModels("a/one"), results: map[string]provider.ToolProbeResult{}}
	l, entries := probeLoop(t, p)

	if err := l.CheckModel(context.Background(), entries, 5); err == nil {
		t.Fatal("indice fora da lista deveria ser recusado")
	}
	if len(p.calls) != 0 {
		t.Fatalf("nenhuma sonda deveria ter rodado, rodou %v", p.calls)
	}
}

// A cancelled context must stop the sweep before spending any more of the
// operator's quota.
func TestCheckModelsStopsOnContextCancel(t *testing.T) {
	p := &probeProvider{models: sampleModels("a/1", "a/2", "a/3"), results: map[string]provider.ToolProbeResult{}}
	l, entries := probeLoop(t, p)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l.CheckModels(ctx, entries, 0, 1, 2)

	if len(p.calls) != 0 {
		t.Fatalf("contexto cancelado nao deveria sondar nada, sondou %v", p.calls)
	}
}

// The sweep probes exactly the requested entries — not the whole catalog.
func TestCheckModelsProbesOnlyRequestedEntries(t *testing.T) {
	p := &probeProvider{models: sampleModels("a/1", "a/2", "a/3"), results: map[string]provider.ToolProbeResult{}}
	l, entries := probeLoop(t, p)

	l.CheckModels(context.Background(), entries, 0, 2)

	if len(p.calls) != 2 || p.calls[0] != "a/1" || p.calls[1] != "a/3" {
		t.Fatalf("sondou %v, esperava [a/1 a/3]", p.calls)
	}
	if entries[1].Check != catalog.CheckUnverified {
		t.Fatal("a/2 nao foi pedido e nao devia ter Check alterado")
	}
}
