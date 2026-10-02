package loop

import (
	"context"
	"errors"
	"testing"

	"ancientai/internal/catalog"
	"ancientai/internal/provider"
)

// catalogProvider is a fake provider that can both list a catalog and report the
// active model, so the tests can assert what a switch actually changed.
type catalogProvider struct {
	model   string
	models  []provider.ModelInfo
	listErr error
	listed  bool
}

func (c *catalogProvider) Chat(ctx context.Context, msgs []provider.ChatMessage, tools []provider.ToolDef, onEvent func(provider.StreamEvent)) (provider.ChatResult, error) {
	return provider.ChatResult{}, errors.New("nao usado")
}

func (c *catalogProvider) ListModels(ctx context.Context) ([]provider.ModelInfo, error) {
	c.listed = true
	if c.listErr != nil {
		return nil, c.listErr
	}
	return c.models, nil
}

func (c *catalogProvider) Model() string { return c.model }
func (c *catalogProvider) SetModel(m string) error {
	c.model = m
	return nil
}
func (c *catalogProvider) BaseURL() string { return "https://example.invalid/v1" }
func (c *catalogProvider) SetBaseURL(u, e string) error {
	return nil
}

func testCatalogEntries() []catalog.Entry {
	return catalog.Build([]provider.ModelInfo{
		{ID: "nvidia/nemotron-3.5-lightning-30b-a3b"},
		{ID: "meta/llama-3.1-8b-instruct"},
	})
}

func TestListModelsClassifiesCatalog(t *testing.T) {
	p := &catalogProvider{model: "nvidia/nemotron-3.5-lightning-30b-a3b", models: []provider.ModelInfo{
		{ID: "nvidia/nemotron-3.5-lightning-30b-a3b"},
		{ID: "nvidia/nv-embedqa-e5-v5"},
	}}
	l := newTestLoopA(t, p, nil, nil, t.TempDir())
	got, err := l.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("catalog size = %d, want 2", len(got))
	}
	// The embedding family must be present in the raw catalog and classified,
	// so the frontends can filter it out with one shared predicate.
	idx := catalog.Index(got, "nvidia/nv-embedqa-e5-v5")
	if idx < 0 || got[idx].Kind != catalog.KindEmbedding {
		t.Errorf("embedding entry = %+v", got)
	}
}

func TestListModelsFailsClosedOnProviderError(t *testing.T) {
	p := &catalogProvider{model: "keep/me", listErr: errors.New("provider: HTTP 500")}
	l := newTestLoopA(t, p, nil, nil, t.TempDir())
	if _, err := l.ListModels(context.Background()); err == nil {
		t.Fatal("provider error must surface")
	}
	// Security: a failed listing must not change the active model.
	if l.CurrentModel() != "keep/me" {
		t.Errorf("model changed on a failed listing: %q", l.CurrentModel())
	}
}

func TestListModelsUnsupportedProvider(t *testing.T) {
	// A provider that cannot list must fail closed with a clear message rather
	// than returning an empty catalog that looks like "no models exist".
	l := newTestLoopA(t, &scriptedProvider{}, nil, nil, t.TempDir())
	_, err := l.ListModels(context.Background())
	if !errors.Is(err, ErrCatalogUnsupported) {
		t.Errorf("err = %v, want ErrCatalogUnsupported", err)
	}
}

func TestSelectModelRangeIsCheckedBeforeAnySwitch(t *testing.T) {
	p := &catalogProvider{model: "keep/me"}
	l := newTestLoopA(t, p, nil, nil, t.TempDir())
	entries := testCatalogEntries()

	for _, bad := range []int{-1, 2, 999} {
		if err := l.SelectModel(entries, bad); err == nil {
			t.Errorf("index %d must fail closed", bad)
		}
		if p.model != "keep/me" {
			t.Fatalf("index %d changed the model to %q", bad, p.model)
		}
	}
	if err := l.SelectModel(entries, 1); err != nil {
		t.Fatalf("valid index refused: %v", err)
	}
	if p.model != "meta/llama-3.1-8b-instruct" {
		t.Errorf("model = %q, want the second entry", p.model)
	}
}

func TestSelectModelEmptyListRefuses(t *testing.T) {
	p := &catalogProvider{model: "keep/me"}
	l := newTestLoopA(t, p, nil, nil, t.TempDir())
	if err := l.SelectModel(nil, 0); err == nil {
		t.Error("empty catalog must refuse")
	}
	if p.model != "keep/me" {
		t.Errorf("model changed: %q", p.model)
	}
}
