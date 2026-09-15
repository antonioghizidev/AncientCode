package loop

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"ancientai/internal/config"
	"ancientai/internal/model"
	"ancientai/internal/provider"
)

const testSecret = "sk-abcdefGHIJKLMNOPQRSTUVWXyz123456"

type fakeProvider struct {
	gotMsgs []provider.ChatMessage
	res     provider.ChatResult
	err     error
}

func (f *fakeProvider) Chat(_ context.Context, msgs []provider.ChatMessage, _ []provider.ToolDef, _ func(provider.StreamEvent)) (provider.ChatResult, error) {
	f.gotMsgs = append([]provider.ChatMessage{}, msgs...)
	return f.res, f.err
}

func newTestLoop(t *testing.T, fp *fakeProvider) *Loop {
	t.Helper()
	st, err := model.Open(filepath.Join(t.TempDir(), "s", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{} // cfg is not consulted by the loop in milestone 1
	return New(fp, st, cfg)
}

func TestTurnAppendsAndReturns(t *testing.T) {
	fp := &fakeProvider{res: provider.ChatResult{FinalContent: "oi!", FinishReason: "stop"}}
	l := newTestLoop(t, fp)

	got, err := l.Turn(context.Background(), "quem era o primeiro presidente?")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if got != "oi!" {
		t.Fatalf("content = %q", got)
	}
	if len(fp.gotMsgs) != 1 || fp.gotMsgs[0].Role != "user" {
		t.Fatalf("provider received %+v", fp.gotMsgs)
	}
	hist, _ := l.st.Load()
	if len(hist) != 2 || hist[0].Role != "user" || hist[1].Role != "assistant" {
		t.Fatalf("persisted history = %+v", hist)
	}
}

// Security: a user secret typed at the prompt must reach the provider masked.
func TestTurnRedactsUserInputBeforeProvider(t *testing.T) {
	fp := &fakeProvider{res: provider.ChatResult{FinalContent: "ok"}}
	l := newTestLoop(t, fp)

	_, err := l.Turn(context.Background(), "segredo "+testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fp.gotMsgs[0].Content, "***") {
		t.Fatalf("provider did not receive redacted form: %q", fp.gotMsgs[0].Content)
	}
	if strings.Contains(fp.gotMsgs[0].Content, testSecret) {
		t.Fatal("secret reached the provider verbatim")
	}
	// And the same secret never reached disk.
	hist, _ := l.st.Load()
	if strings.Contains(hist[0].Content, testSecret) {
		t.Fatal("secret persisted verbatim")
	}
}

func TestTurnEmptyMessageFails(t *testing.T) {
	fp := &fakeProvider{}
	l := newTestLoop(t, fp)
	if _, err := l.Turn(context.Background(), "   "); err == nil {
		t.Fatal("empty message accepted")
	}
	if len(fp.gotMsgs) != 0 {
		t.Fatalf("provider was called for empty message")
	}
}

// Security: tool calling is disabled in M1 and must fail closed — never be
// inferred or half-executed.
func TestTurnFailsClosedOnToolCall(t *testing.T) {
	fp := &fakeProvider{res: provider.ChatResult{
		ToolCalls: []provider.CompletedToolCall{{ID: "c", Name: "write_file", Args: `{"path":"/tmp/x"}`}},
	}}
	l := newTestLoop(t, fp)

	_, err := l.Turn(context.Background(), "escreva um arquivo")
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("expected fail-closed error, got %v", err)
	}
	hist, _ := l.st.Load()
	if len(hist) != 1 {
		t.Fatalf("assistant tool-call turn must not be persisted; history = %d", len(hist))
	}
}

func TestTurnPropagatesProviderError(t *testing.T) {
	fp := &fakeProvider{err: assertErr("network down")}
	l := newTestLoop(t, fp)
	if _, err := l.Turn(context.Background(), "oi"); err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("error not propagated: %v", err)
	}
}

func TestTurnSendsFullHistory(t *testing.T) {
	fp := &fakeProvider{res: provider.ChatResult{FinalContent: "1"}}
	l := newTestLoop(t, fp)
	if _, err := l.Turn(context.Background(), "primeira"); err != nil {
		t.Fatal(err)
	}
	if len(fp.gotMsgs) != 1 {
		t.Fatalf("first call history = %d messages, want 1", len(fp.gotMsgs))
	}
	fp.res = provider.ChatResult{FinalContent: "2"}
	if _, err := l.Turn(context.Background(), "segunda"); err != nil {
		t.Fatal(err)
	}
	// History is now [user, assistant, user] — the whole session carries over.
	if len(fp.gotMsgs) != 3 {
		t.Fatalf("second call history = %d messages, want 3", len(fp.gotMsgs))
	}
	if fp.gotMsgs[2].Role != "user" || fp.gotMsgs[2].Content != "segunda" {
		t.Fatalf("history passed wrong: %+v", fp.gotMsgs[2])
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
