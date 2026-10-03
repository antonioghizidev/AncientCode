package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/antonioghizidev/ancientcode/internal/config"
	"github.com/antonioghizidev/ancientcode/internal/loop"
	"github.com/antonioghizidev/ancientcode/internal/model"
	"github.com/antonioghizidev/ancientcode/internal/provider"
)

// switchableCatalogProvider is a real provider from the TUI's point of view: it
// lists a catalog and switches models, so the picker path can be exercised
// end-to-end without a network.
type switchableCatalogProvider struct {
	model  string
	models []provider.ModelInfo
}

func (c *switchableCatalogProvider) Chat(ctx context.Context, msgs []provider.ChatMessage, tools []provider.ToolDef, onEvent func(provider.StreamEvent)) (provider.ChatResult, error) {
	return provider.ChatResult{}, nil
}
func (c *switchableCatalogProvider) ListModels(ctx context.Context) ([]provider.ModelInfo, error) {
	return c.models, nil
}
func (c *switchableCatalogProvider) Model() string { return c.model }
func (c *switchableCatalogProvider) SetModel(m string) error {
	c.model = m
	return nil
}
func (c *switchableCatalogProvider) BaseURL() string              { return "https://example.invalid/v1" }
func (c *switchableCatalogProvider) SetBaseURL(u, e string) error { return nil }

// mkPickerLiveModel builds a tuiModel backed by a real Loop with a fake provider.
func mkPickerLiveModel(t *testing.T, p *switchableCatalogProvider) *tuiModel {
	t.Helper()
	st, err := model.Open(filepath.Join(t.TempDir(), "s", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	l := loop.New(p, st, &config.Config{ToolboxMode: "run"}, nil, nil)
	m := mkModel(t, l)
	return m
}

func TestPickerEnterActuallySwitchesTheModel(t *testing.T) {
	p := &switchableCatalogProvider{
		model: "nvidia/nemotron-3.5-lightning-30b-a3b",
		models: []provider.ModelInfo{
			{ID: "nvidia/nemotron-3.5-lightning-30b-a3b"},
			{ID: "meta/llama-3.1-8b-instruct"},
			{ID: "microsoft/phi-4"},
		},
	}
	m := mkPickerLiveModel(t, p)
	m.runner = &fakeRunner{} // commands need the runner wired, as in production

	// /models opens the picker and returns the fetch command.
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/models")})
	m, cmd := upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.picker == nil {
		t.Fatal("/models must open the picker")
	}
	if cmd == nil {
		t.Fatal("/models must return the async fetch command")
	}
	// Feed the catalog back as the Bubble Tea runtime would.
	msg := cmd()
	m, _ = upd(m, msg)
	if m.picker == nil || m.picker.loading {
		t.Fatalf("catalog not applied: %+v", m.picker)
	}

	// Narrow to llama and select it with Enter.
	for _, r := range "llama" {
		m, _ = upd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(m.picker.vis) != 1 {
		t.Fatalf("filter failed: %d entries", len(m.picker.vis))
	}
	var switchCmd tea.Cmd
	m, switchCmd = upd(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.picker != nil {
		t.Fatal("Enter must close the picker")
	}
	// Regression (deadlock found on the Inspiron): the switch must NOT have
	// happened yet inside Update — it runs as a Bubble Tea command, otherwise
	// SwitchModel's event -> tea.Program.Send blocks the event loop forever.
	if p.model != "nvidia/nemotron-3.5-lightning-30b-a3b" {
		t.Fatalf("model switched synchronously inside Update: %q", p.model)
	}
	if switchCmd == nil {
		t.Fatal("Enter must return a switch command")
	}
	switched, ok := switchCmd().(modelSwitchedMsg)
	if !ok {
		t.Fatalf("Enter must return a switch command, got %T", switchCmd())
	}
	m, _ = upd(m, switched)

	if p.model != "meta/llama-3.1-8b-instruct" {
		t.Errorf("model = %q, want the filtered entry", p.model)
	}
	if m.model != "meta/llama-3.1-8b-instruct" {
		t.Errorf("TUI model not refreshed: %q", m.model)
	}
	if m.banner != "" {
		t.Errorf("unexpected banner: %q", m.banner)
	}
	found := false
	for _, l := range m.session {
		if strings.Contains(l, "meta/llama-3.1-8b-instruct") && strings.Contains(l, "vale nesta sessao") {
			found = true
		}
	}
	if !found {
		t.Errorf("selection must be reported in the session panel: %v", m.session)
	}
}

// A command consumes the input line: otherwise the next keystrokes are appended
// to the command text ("/modelsoi"). Found by the Inspiron smoke.
func TestCommandClearsThePromptLine(t *testing.T) {
	p := &switchableCatalogProvider{model: "start/model"}
	m := mkPickerLiveModel(t, p)
	m.runner = &fakeRunner{}

	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/model novo/model")})
	if m.input != "/model novo/model" {
		t.Fatalf("typing did not land: %q", m.input)
	}
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.input != "" || m.cursor != 0 {
		t.Errorf("command must clear the prompt, got %q (cursor %d)", m.input, m.cursor)
	}
	// The next thing typed must stand on its own.
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("oi")})
	if m.input != "oi" {
		t.Errorf("typing after a command must start fresh, got %q", m.input)
	}
}

// The same deadlock shape applies to /model and /provider: both emit events, so
// both must switch through a command instead of inline.
func TestModelAndProviderSwitchesAreAsync(t *testing.T) {
	p := &switchableCatalogProvider{model: "start/model"}
	m := mkPickerLiveModel(t, p)
	m.runner = &fakeRunner{}

	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/model novo/model")})
	m, cmd := upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	if p.model != "start/model" {
		t.Fatalf("/model switched synchronously: %q", p.model)
	}
	if cmd == nil {
		t.Fatal("/model must return a switch command")
	}
	m, _ = upd(m, cmd())
	if p.model != "novo/model" {
		t.Errorf("/model async switch did not apply: %q", p.model)
	}
}

// The picker replaces the activity panel's content, so the activity viewport
// offset must not leak into it — otherwise the panel opens scrolled and the
// first rows (the filter line and the highlighted entry) are invisible.
func TestPickerIgnoresActivityViewportOffset(t *testing.T) {
	m := mkPickerModel(t)
	feedCatalog(t, m, pickerCatalog())
	// Simulate the activity panel being scrolled down from earlier events.
	m.activityOff = 3
	view := stripANSI(m.View())
	// The first content row of the panel must be the filter line, and the
	// highlighted (first) entry must be visible right after it.
	idxFilter := strings.Index(view, "filtrando:")
	idxFirst := strings.Index(view, "1. ")
	if idxFilter < 0 || idxFirst < 0 || idxFirst < idxFilter {
		t.Fatalf("picker must render from the top (filter line, then entry 1): %q", view)
	}
	if strings.Contains(view[:idxFilter+40], "nemotron-4-340b") {
		t.Error("panel opened scrolled: later rows appear before the filter line")
	}
}

// After the picker closes, the activity panel must come back from the top
// instead of resuming a stale offset.
func TestPickerRestoresActivityFromTopOnClose(t *testing.T) {
	m := mkPickerModel(t)
	feedCatalog(t, m, pickerCatalog())
	m.activityOff = 5
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.activityOff != 0 {
		t.Errorf("activity offset must reset when the picker closes, got %d", m.activityOff)
	}
}
