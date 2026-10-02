package main

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"ancientai/internal/catalog"
	"ancientai/internal/provider"
)

// pickerCatalog builds a catalog with the two shapes measured in production.
func pickerCatalog() []catalog.Entry {
	return catalog.Build([]provider.ModelInfo{
		{ID: "nvidia/nemotron-3.5-lightning-30b-a3b"},
		{ID: "meta/llama-3.1-8b-instruct"},
		{ID: "nvidia/nv-embedqa-e5-v5"},
		{ID: "deepseek/deepseek-chat", PricingPrompt: "0", PricingCompletion: "0",
			SupportedParameters: []string{"tools"}},
		{ID: "openai/gpt-4o-mini", PricingPrompt: "0.0000001", PricingCompletion: "0.0000002",
			InputModalities: []string{"text"}, SupportedParameters: []string{"tools"}},
	})
}

func mkPickerModel(t *testing.T) *tuiModel {
	t.Helper()
	m := mkModel(t, nil)
	m.picker = &picker{active: "nvidia/nemotron-3.5-lightning-30b-a3b"}
	return m
}

func feedCatalog(t *testing.T, m *tuiModel, entries []catalog.Entry) {
	t.Helper()
	m, _ = upd(m, pickerMsg{entries: entries})
}

func TestPickerTypingFiltersTheList(t *testing.T) {
	m := mkPickerModel(t)
	feedCatalog(t, m, pickerCatalog())
	if len(m.picker.vis) < 4 {
		t.Fatalf("expected chat-ish entries, got %d", len(m.picker.vis))
	}
	// Typing narrows the list instead of reaching the prompt.
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("llama")})
	if m.input != "" {
		t.Errorf("typing must not go to the prompt, got %q", m.input)
	}
	if len(m.picker.vis) != 1 || !strings.Contains(m.picker.vis[0].Info.ID, "llama") {
		t.Errorf("filter failed: %+v", m.picker.vis)
	}
	// Backspace narrows the query back out one character at a time.
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.picker.opts.query != "llam" {
		t.Errorf("backspace query = %q", m.picker.opts.query)
	}
	// Clearing the whole query restores the unfiltered list.
	for i := 0; i < 5; i++ {
		m, _ = upd(m, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	if m.picker.opts.query != "" {
		t.Errorf("query should be empty, got %q", m.picker.opts.query)
	}
	if len(m.picker.vis) < 2 {
		t.Errorf("clearing the query must restore the list, got %d", len(m.picker.vis))
	}
}

func TestPickerArrowsMoveHighlightNotPrompt(t *testing.T) {
	m := mkPickerModel(t)
	feedCatalog(t, m, pickerCatalog())
	m.picker.cur = 0
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.picker.cur != 1 {
		t.Errorf("Down must move the highlight, got %d", m.picker.cur)
	}
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.picker.cur != 0 {
		t.Errorf("Up must move back, got %d", m.picker.cur)
	}
	if m.input != "" {
		t.Errorf("arrows must not type into the prompt: %q", m.input)
	}
}

func TestPickerEnterSelectsAndCloses(t *testing.T) {
	m := mkPickerModel(t)
	feedCatalog(t, m, pickerCatalog())
	m.picker.cur = 1
	want, _ := m.picker.selected()
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.picker != nil {
		t.Error("Enter must close the picker")
	}
	// With a nil agent the switch fails and must be reported, not swallowed —
	// either way the picker is gone and nothing silently changed.
	if m.banner == "" {
		t.Errorf("selection outcome must be reported, banner empty (want id %q)", want.Info.ID)
	}
}

func TestPickerEscAndCtrlCCancelWithoutSelecting(t *testing.T) {
	for _, k := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyCtrlC}} {
		m := mkPickerModel(t)
		feedCatalog(t, m, pickerCatalog())
		m, _ = upd(m, k)
		if m.picker != nil {
			t.Errorf("%v must close the picker", k.Type)
		}
	}
}

func TestPickerCtrlFTogglesFreeOnlyWhenPriced(t *testing.T) {
	m := mkPickerModel(t)
	// A priced catalog (OpenRouter-like) supports the toggle.
	feedCatalog(t, m, catalog.Build([]provider.ModelInfo{
		{ID: "a/free", PricingPrompt: "0", PricingCompletion: "0"},
		{ID: "b/paid", PricingPrompt: "0.1", PricingCompletion: "0.2"},
	}))
	if !m.picker.freeAvailable {
		t.Fatal("priced catalog must enable the free filter")
	}
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyCtrlF})
	if !m.picker.free {
		t.Error("Ctrl+F must enable the free filter")
	}
	if len(m.picker.vis) != 1 || m.picker.vis[0].Info.ID != "a/free" {
		t.Errorf("free filter = %+v", m.picker.vis)
	}
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyCtrlF})
	if m.picker.free {
		t.Error("Ctrl+F must toggle back off")
	}
}

// NVIDIA NIM publishes no pricing: Ctrl+F must say so, not show an empty list.
func TestPickerCtrlFExplainsWhenProviderHasNoPricing(t *testing.T) {
	m := mkPickerModel(t)
	// Pure NIM shape: ids only, no pricing, no capabilities.
	nim := catalog.Build([]provider.ModelInfo{
		{ID: "nvidia/nemotron-3.5-lightning-30b-a3b"},
		{ID: "meta/llama-3.1-8b-instruct"},
		{ID: "nvidia/nv-embedqa-e5-v5"},
	})
	feedCatalog(t, m, nim)
	if m.picker.freeAvailable {
		t.Fatal("NIM catalog must report free filter unavailable")
	}
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyCtrlF})
	if m.picker.free {
		t.Error("free filter must not engage on an unpriced provider")
	}
	if !strings.Contains(m.banner, "preco") {
		t.Errorf("banner must explain why, got %q", m.banner)
	}
}

func TestPickerFetchFailureIsFailClosed(t *testing.T) {
	m := mkPickerModel(t)
	m, _ = upd(m, pickerMsg{err: errors.New("provider: HTTP 500")})
	if m.picker != nil {
		t.Error("a failed fetch must close the picker")
	}
	if !strings.Contains(m.banner, "500") {
		t.Errorf("failure must be visible, banner = %q", m.banner)
	}
}

func TestPickerClosedWhileFetchingDoesNotResurrect(t *testing.T) {
	m := mkPickerModel(t)
	m.picker = nil // operator cancelled before the response
	entries := pickerCatalog()
	m, _ = upd(m, pickerMsg{entries: entries})
	if m.picker != nil {
		t.Error("a late response must not reopen a closed picker")
	}
}

func TestPickerEmptyListEnterDoesNotSwitch(t *testing.T) {
	m := mkPickerModel(t)
	m.picker = &picker{active: "x/y"}
	m.picker.all = []catalog.Entry{}
	m.picker.visible()
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(m.banner, "nenhum modelo") {
		t.Errorf("empty selection must be reported, banner = %q", m.banner)
	}
}

func TestPickerViewShowsFilterAndCounters(t *testing.T) {
	m := mkPickerModel(t)
	feedCatalog(t, m, pickerCatalog())
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	view := stripANSI(m.View())
	if !strings.Contains(view, "filtrando") {
		t.Errorf("picker must show the query line: %q", view)
	}
	if !strings.Contains(view, "llama") {
		t.Errorf("picker must show the filtered match: %q", view)
	}
	// The activity panel is replaced while the picker is open.
	if strings.Contains(view, "modelo: aguardando") {
		t.Errorf("picker should replace the activity panel: %q", view)
	}
}

func TestPickerHomeEndClampWithinList(t *testing.T) {
	m := mkPickerModel(t)
	feedCatalog(t, m, pickerCatalog())
	n := len(m.picker.vis)
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyEnd})
	if m.picker.cur != n-1 {
		t.Errorf("End must go to the last entry, got %d of %d", m.picker.cur, n)
	}
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyHome})
	if m.picker.cur != 0 {
		t.Errorf("Home must go to the first, got %d", m.picker.cur)
	}
}

// While the catalog request is in flight the panel must not claim the list is
// empty: nothing has been filtered yet, so "no models match" would be a lie.
func TestPickerShowsLoadingNotEmptyWhileFetching(t *testing.T) {
	m := mkPickerModel(t)
	m.picker.loading = true
	view := stripANSI(m.View())
	if !strings.Contains(view, "carregando") {
		t.Errorf("panel must say it is loading: %q", view)
	}
	if strings.Contains(view, "nenhum modelo") {
		t.Errorf("must not claim an empty result before the catalog arrives: %q", view)
	}
}

func TestPickerModelIndexArgParsesOnlyPositiveInts(t *testing.T) {
	if i, ok := modelIndexArg("3"); !ok || i != 2 {
		t.Errorf("modelIndexArg(3) = %d,%v", i, ok)
	}
	for _, s := range []string{"", "0", "-1", "abc", "3.5"} {
		if _, ok := modelIndexArg(s); ok {
			t.Errorf("modelIndexArg(%q) must not be an index", s)
		}
	}
}
