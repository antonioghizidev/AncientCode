package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/antonioghizidev/ancientcode/internal/config"
	"github.com/antonioghizidev/ancientcode/internal/model"
	"github.com/antonioghizidev/ancientcode/internal/provider"
)

// switchableProvider is scriptedProvider + the M8 runtime-switch contract.
type switchableProvider struct {
	*scriptedProvider
	model   string
	baseURL string
	env     string
}

func newSwitchable(t *testing.T, script []provider.ChatResult) *switchableProvider {
	t.Helper()
	return &switchableProvider{scriptedProvider: &scriptedProvider{script: script, repeat: -1}, model: "base/model", baseURL: "https://base.invalid/v1", env: "TEST_SWITCH_KEY"}
}

// Chat delegates to the scripted fake and simulates a content delta so the
// loop's ModelStreaming transition fires (the scripted fake does not stream).
func (s *switchableProvider) Chat(ctx context.Context, msgs []provider.ChatMessage, defs []provider.ToolDef, onEvent func(provider.StreamEvent)) (provider.ChatResult, error) {
	res, err := s.scriptedProvider.Chat(ctx, msgs, defs, onEvent)
	if err == nil && onEvent != nil && res.FinalContent != "" {
		onEvent(provider.StreamEvent{DeltaContent: res.FinalContent})
	}
	return res, err
}

func (s *switchableProvider) Model() string   { return s.model }
func (s *switchableProvider) BaseURL() string { return s.baseURL }
func (s *switchableProvider) SetModel(m string) error {
	if strings.TrimSpace(m) == "" {
		return fmt.Errorf("empty")
	}
	s.model = m
	return nil
}
func (s *switchableProvider) SetBaseURL(u, env string) error {
	if os.Getenv(env) == "" {
		return fmt.Errorf("env %q not set", env)
	}
	s.baseURL, s.env = u, env
	return nil
}

func TestEventJSONRoundtripStable(t *testing.T) {
	orig := Event{Phase: PhaseToolRunning, Tool: "shell", Detail: "executando [REDACTED]", Dur: 1523 * time.Millisecond, At: time.Now()}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Event
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Phase != orig.Phase || back.Tool != orig.Tool || back.Detail != orig.Detail || back.Dur != orig.Dur {
		t.Fatalf("roundtrip mismatch: %+v", back)
	}
	if back.At.IsZero() {
		t.Fatal("At lost in roundtrip")
	}
	// Stability: field names must not change under future refactors.
	for _, want := range []string{`"phase"`, `"tool"`, `"detail"`, `"dur"`, `"at"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("serialized event missing %s: %s", want, b)
		}
	}
}

// TestEventStreamPhaseOrderingAndToolDur verifies the M8 §6 stream contract:
// the fake emits a tool call (write_file, ask-gated), the run must produce
// ModelWaiting → ModelStreaming → GateWaiting → ToolRunning → TurnOK in order,
// with Dur on ToolRunning > 0.
func TestEventStreamPhaseOrderingAndToolDur(t *testing.T) {
	fp := newSwitchable(t, []provider.ChatResult{
		{
			FinalContent: "vou gravar",
			ToolCalls:    []provider.CompletedToolCall{{ID: "c1", Name: "write_file", Args: `{"path":"a.txt","content":"x"}`}},
		},
		{FinalContent: "pronto", FinishReason: "stop"},
	})
	appr := func(ctx context.Context, name, summary string) (bool, error) { return true, nil }
	l := newTestLoop(t, fp, runConfig(), appr, t.TempDir())

	var evs []Event
	l.SetObserver(func(ev Event) { evs = append(evs, ev) })

	if _, err := l.Turn(context.Background(), "grave a.txt"); err != nil {
		t.Fatalf("Turn: %v", err)
	}

	var phases []Phase
	for _, ev := range evs {
		phases = append(phases, ev.Phase)
	}
	want := []Phase{PhaseModelWaiting, PhaseModelStreaming, PhaseGateWaiting, PhaseToolRunning, PhaseTurnOK}
	if len(phases) < len(want) {
		t.Fatalf("phases = %v, want prefix %v", phases, want)
	}
	for i := range want {
		if phases[i] != want[i] {
			t.Fatalf("phase[%d] = %v, want %v (got %v)", i, phases[i], want[i], phases)
		}
	}
	// ToolRunning must carry the tool name and a positive elapsed duration.
	var toolEv Event
	for _, ev := range evs {
		if ev.Phase == PhaseToolRunning {
			toolEv = ev
		}
	}
	if toolEv.Tool != "write_file" {
		t.Fatalf("ToolRunning tool = %q, want write_file", toolEv.Tool)
	}
	if toolEv.Dur < 0 {
		t.Fatalf("ToolRunning Dur = %v, must be >= 0", toolEv.Dur)
	}
	// The follow-up Chat emits a new ModelWaiting afterwards (multi-round).
	var sawSecondWaiting bool
	for _, ev := range evs {
		if ev.Phase == PhaseModelWaiting {
			sawSecondWaiting = true
		}
	}
	if !sawSecondWaiting {
		t.Fatal("second round did not emit ModelWaiting again")
	}
}

// TestEventTurnErrorRedacted drives a provider that fails and asserts the
// TurnError payload is redacted before reaching the observer.
func TestEventTurnErrorRedacted(t *testing.T) {
	fp := newSwitchable(t, nil)
	fp.err = fmt.Errorf("boom: %s", testSecret)
	l := newTestLoop(t, fp, runConfig(), nil, "")

	var evs []Event
	l.SetObserver(func(ev Event) { evs = append(evs, ev) })
	if _, err := l.Turn(context.Background(), "oi"); err == nil {
		t.Fatal("Turn should have failed")
	}
	for _, ev := range evs {
		if ev.Phase == PhaseTurnError {
			if strings.Contains(ev.Detail, testSecret) {
				t.Fatalf("TurnError leaked secret: %q", ev.Detail)
			}
			return
		}
	}
	t.Fatal("no TurnError event emitted")
}

// TestContextUsageBounds checks the context meter against the reduction
// guarantees of ReduceMessages: with a budget the OLD turns are dropped, the
// sent estimate stays within budget, and the percentage caps at 100 even when
// a single newest turn alone exceeds the budget (the newest turn is always
// kept, so sent can surpass budget only in that irreducible case).
func TestContextUsageBounds(t *testing.T) {
	st, err := model.Open(filepath.Join(t.TempDir(), "s.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Append(
		provider.ChatMessage{Role: "user", Content: strings.Repeat("x", 3000)},
		provider.ChatMessage{Role: "assistant", Content: "primeiro"},
		provider.ChatMessage{Role: "user", Content: "segundo"},
		provider.ChatMessage{Role: "assistant", Content: "resposta curta"},
	); err != nil {
		t.Fatal(err)
	}

	// Budget off: Pct=-1, Budget=0, and the sent estimate is the full history.
	off := &config.Config{ToolboxMode: "run", SessionCtxBytes: 0}
	uOff := New(noopProvider{}, st, off, nil, nil).ContextUsage()
	if uOff.Pct != -1 || uOff.Budget != 0 || uOff.SentBytes == 0 {
		t.Fatalf("off usage = %+v, want Pct=-1 Budget=0 SentBytes>0", uOff)
	}

	// Budget on, reducible history: old turn dropped → sent within budget.
	cfg := &config.Config{ToolboxMode: "run", SessionCtxBytes: 1000}
	uOn := New(noopProvider{}, st, cfg, nil, nil).ContextUsage()
	if uOn.SentBytes > cfg.SessionCtxBytes {
		t.Fatalf("sentBytes = %d > budget %d (old turn should have been dropped)", uOn.SentBytes, cfg.SessionCtxBytes)
	}
	if uOn.Pct < 0 || uOn.Pct > 100 {
		t.Fatalf("pct = %d out of range", uOn.Pct)
	}

	// Iron ceiling: a budget smaller than the newest turn reports 100%.
	bigSt, _ := model.Open(filepath.Join(t.TempDir(), "b.jsonl"))
	if err := bigSt.Append(
		provider.ChatMessage{Role: "user", Content: strings.Repeat("y", 3000)},
		provider.ChatMessage{Role: "assistant", Content: "longa"},
	); err != nil {
		t.Fatal(err)
	}
	tiny := &config.Config{ToolboxMode: "run", SessionCtxBytes: 1}
	uTiny := New(noopProvider{}, bigSt, tiny, nil, nil).ContextUsage()
	if uTiny.Pct != 100 {
		t.Fatalf("tiny-budget pct = %d, want 100 (ceiling)", uTiny.Pct)
	}
	// And this irreducible case is the ONLY one allowed to exceed the budget.
	if uTiny.SentBytes <= tiny.SessionCtxBytes {
		t.Fatalf("single newest turn (%d bytes) should exceed budget 1", uTiny.SentBytes)
	}
}

type noopProvider struct{}

func (noopProvider) Chat(context.Context, []provider.ChatMessage, []provider.ToolDef, func(provider.StreamEvent)) (provider.ChatResult, error) {
	return provider.ChatResult{FinalContent: "ok", FinishReason: "stop"}, nil
}

// TestSwitchModelChangesNextChat proves the switch reaches the provider used
// by the very next Turn.
func TestSwitchModelChangesNextChat(t *testing.T) {
	fp := newSwitchable(t, []provider.ChatResult{{FinalContent: "ok", FinishReason: "stop"}})
	l := newTestLoop(t, fp, runConfig(), nil, "")
	if err := l.SwitchModel("nova/model"); err != nil {
		t.Fatalf("SwitchModel: %v", err)
	}
	if got := l.CurrentModel(); got != "nova/model" {
		t.Fatalf("CurrentModel() = %q, want nova/model", got)
	}
	if _, err := l.Turn(context.Background(), "oi"); err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if fp.model != "nova/model" {
		t.Fatalf("provider model = %q, want nova/model", fp.model)
	}
}

func TestSwitchModelBlankRejectedAndStateUnchanged(t *testing.T) {
	fp := newSwitchable(t, nil)
	l := newTestLoop(t, fp, runConfig(), nil, "")
	if err := l.SwitchModel("  "); err == nil {
		t.Fatal("SwitchModel(blank) accepted; want fail-closed error")
	}
	if got := l.CurrentModel(); got != "base/model" {
		t.Fatalf("CurrentModel() = %q, want unchanged base/model", got)
	}
}

// TestSwitchProviderMissingEnvFailClosed proves the switch refuses before any
// state change when the target provider's key env is absent.
func TestSwitchProviderMissingEnvFailClosed(t *testing.T) {
	fp := newSwitchable(t, nil)
	cfg := runConfig()
	l := newTestLoop(t, fp, cfg, nil, "")
	// openrouter needs OPENROUTER_API_KEY; make sure it is NOT set.
	t.Setenv("OPENROUTER_API_KEY", "")
	if err := l.SwitchProvider("openrouter"); err == nil {
		t.Fatal("SwitchProvider(openrouter) with missing key env accepted; want error")
	}
	if got := l.CurrentProviderBaseURL(); got != "https://base.invalid/v1" {
		t.Fatalf("base_url = %q, want unchanged https://base.invalid/v1", got)
	}
}

func TestSwitchProviderUnknownFailClosed(t *testing.T) {
	l := newTestLoop(t, newSwitchable(t, nil), runConfig(), nil, "")
	if err := l.SwitchProvider("nao-existe"); err == nil {
		t.Fatal("unknown provider accepted; want fail-closed error")
	}
}

// TestSwitchProviderApplies confirms a wired provider + set env yields a new
// base URL used by the next Chat.
func TestSwitchProviderApplies(t *testing.T) {
	t.Setenv("NVIDIA_API_KEY", "nvkey-abc")
	fp := newSwitchable(t, []provider.ChatResult{{FinalContent: "ok", FinishReason: "stop"}})
	l := newTestLoop(t, fp, runConfig(), nil, "")
	if err := l.SwitchProvider("nvidia"); err != nil {
		t.Fatalf("SwitchProvider: %v", err)
	}
	if got := l.CurrentProviderBaseURL(); got != "https://integrate.api.nvidia.com/v1" {
		t.Fatalf("base_url = %q, want nvidia default", got)
	}
	if _, err := l.Turn(context.Background(), "oi"); err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if fp.baseURL != "https://integrate.api.nvidia.com/v1" {
		t.Fatalf("provider base_url = %q, want nvidia default", fp.baseURL)
	}
}

// TestContinueDoesNotDuplicateUser asserts retry/continue idempotence (REPL
// /retry /continue semantics): Continue("") leaves the history untouched apart
// from the new assistant answer, so the user message is never re-appended.
func TestContinueDoesNotDuplicateUser(t *testing.T) {
	fp := newSwitchable(t, []provider.ChatResult{
		{FinalContent: "ok1", FinishReason: "stop"},
		{FinalContent: "ok2", FinishReason: "stop"},
	})
	l := newTestLoop(t, fp, runConfig(), nil, "")
	if _, err := l.Turn(context.Background(), "primeiro"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Continue(context.Background(), ""); err != nil {
		t.Fatalf("Continue: %v", err)
	}
	hist, err := l.st.Load()
	if err != nil {
		t.Fatal(err)
	}
	users := 0
	for _, m := range hist {
		if m.Role == "user" {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("user messages = %d, want 1 (Continue(\"\") must not duplicate)", users)
	}
}

// TestPromptHistoryRedactedAndCircular covers the M8 §4 prompt ring: /historico
// and ↑/↓ only ever see redacted text, capacity 20, oldest drops. Redact keeps
// a 6-char prefix + 4-char tail ("sk-abc***3456"), so the assertion checks the
// middle of the secret never leaks.
func TestPromptHistoryRedactedAndCircular(t *testing.T) {
	l := newTestLoop(t, noopProvider{}, &config.Config{}, nil, "")
	for i := 0; i < 25; i++ {
		l.AddPrompt(fmt.Sprintf("prompt %d com %s", i, testSecret))
	}
	list := l.PromptList()
	if len(list) != promptHistoryCap {
		t.Fatalf("len(PromptList) = %d, want %d", len(list), promptHistoryCap)
	}
	masked := "sk-abc***" + testSecret[len(testSecret)-4:]
	if list[0] != "prompt 5 com "+masked {
		t.Fatalf("oldest entry = %q, want prompt 5 (oldest dropped)", list[0])
	}
	if list[len(list)-1] != "prompt 24 com "+masked {
		t.Fatalf("newest entry = %q", list[len(list)-1])
	}
	joined := strings.Join(list, " ")
	if strings.Contains(joined, "GHIJKLMNOPQRSTUVWXyz") {
		t.Fatal("history leaked the secret body")
	}
	// PromptAt(0) = newest; invalid indexes fail closed.
	newest, ok := l.PromptAt(0)
	if !ok || newest != "prompt 24 com "+masked {
		t.Fatalf("PromptAt(0) = %q, %v", newest, ok)
	}
	if _, ok := l.PromptAt(99); ok {
		t.Fatal("PromptAt(99) resolved; must fail closed")
	}
}

// TestHistoricalPromptRedactionMatchesLoop asserts the loop-level redaction is
// applied at ADD time in the domain (defense in depth, one rule for every
// consumer — /historico, ↑/↓, a future web UI).
func TestHistoricalPromptRedactionMatchesLoop(t *testing.T) {
	l := newTestLoop(t, noopProvider{}, &config.Config{}, nil, "")
	l.AddPrompt("gire a chave " + testSecret)
	hist := l.PromptList()
	if len(hist) != 1 {
		t.Fatalf("len = %d, want 1", len(hist))
	}
	if strings.Contains(hist[0], testSecret) || strings.Contains(hist[0], "GHIJKLMNOPQRSTUVWXyz") {
		t.Fatalf("prompt not redacted: %q", hist[0])
	}
	if !strings.Contains(hist[0], "sk-abc***") {
		t.Fatalf("expected masked remnant in %q", hist[0])
	}
}

// TestCanceledTurnExecutesNothing is the loop-level cancellation contract
// (M8 §5): a canceled turn fails cleanly and the pending tool NEVER executes —
// planCalls demands its Decide within the turn, and a canceled context makes
// that gate fail before any WriteFile happens.
func TestCanceledTurnExecutesNothing(t *testing.T) {
	root := t.TempDir()
	fp := &scriptedProvider{
		script: []provider.ChatResult{{
			FinalContent: "vou gravar",
			ToolCalls:    []provider.CompletedToolCall{{ID: "c1", Name: "write_file", Args: `{"path":"a.txt","content":"x"}`}},
		}},
		repeat: -1,
	}
	appr := func(ctx context.Context, name, summary string) (bool, error) {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
			return true, nil
		}
	}
	l := newTestLoop(t, fp, runConfig(), appr, root)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.Turn(ctx, "interrompa"); err == nil {
		t.Fatal("Turn on canceled ctx should fail")
	}
	// The write was never executed: the gate failed inside the canceled turn.
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("tool executed on a canceled turn (a.txt exists)")
	}
	hist, err := l.st.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range hist {
		if m.Role == "tool" {
			t.Fatal("tool message persisted on canceled turn")
		}
	}
}
