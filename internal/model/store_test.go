package model

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/antonioghizidev/ancientcode/internal/provider"
)

const testSecret = "sk-abcdefGHIJKLMNOPQRSTUVWXyz123456"

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s", "session.jsonl")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	msgs := []provider.ChatMessage{
		{Role: "system", Content: "be brief"},
		{Role: "user", Content: "hello"},
	}
	if err := s.Append(msgs...); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("loaded %d messages", len(got))
	}
	if got[1].Content != "hello" || got[1].Role != "user" {
		t.Fatalf("unexpected message: %+v", got[1])
	}
}

func TestStoreMissingFileLoadsEmpty(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "nope.jsonl"))
	got, err := s.Load()
	if err != nil || got != nil {
		t.Fatalf("got=%v err=%v, want nil,nil", got, err)
	}
}

// Security: a secret pasted by the user must never reach disk verbatim.
func TestStorePersistsOnlyRedactedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s", "session.jsonl")
	s, _ := Open(path)
	msg := provider.ChatMessage{Role: "user", Content: "use key " + testSecret + " now"}
	if err := s.Append(msg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), testSecret) {
		t.Fatal("secret reached disk verbatim")
	}
	if !strings.Contains(string(raw), "***") {
		t.Fatal("redacted form not found on disk")
	}
	// Load returns the masked form only.
	loaded, _ := s.Load()
	if strings.Contains(loaded[0].Content, testSecret) {
		t.Fatal("secret leaked through Load")
	}
}

// Security: tool-call arguments are masked too — they can echo secrets.
func TestStoreRedactsToolCallArguments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s", "session.jsonl")
	s, _ := Open(path)
	msg := provider.ChatMessage{
		Role: "assistant",
		ToolCalls: []provider.RequestToolCall{
			{ID: "1", Type: "function", Function: provider.RequestToolCallFunction{Name: "write", Arguments: `{"data":"` + testSecret + `"}`}},
		},
	}
	if err := s.Append(msg); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), testSecret) {
		t.Fatal("secret leaked inside tool-call arguments")
	}
}

func TestStoreConcurrentAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s", "session.jsonl")
	s, _ := Open(path)

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_ = s.Append(provider.ChatMessage{Role: "user", Content: "m"})
		}(i)
	}
	wg.Wait()

	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != n {
		t.Fatalf("loaded %d messages, want %d", len(got), n)
	}
}

func TestStoreReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s", "session.jsonl")
	s, _ := Open(path)
	_ = s.Append(provider.ChatMessage{Role: "user", Content: "x"})
	if err := s.Reset(); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Load()
	if got != nil {
		t.Fatalf("expected empty after reset, got %d messages", len(got))
	}
}

// SECURITY: a corrupt session file must cause a load error (fail-closed).
func TestStoreCorruptSessionRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s", "session.jsonl")
	s, _ := Open(path)
	// Overwrite with invalid JSONL directly.
	os.WriteFile(path, []byte("not json\n{\"Role\":\"user\",\"Content\":\"ok\"}\n"), 0o600)
	if _, err := s.Load(); err == nil {
		t.Fatal("corrupt session file was accepted")
	}
}

// --- Reduce / ReduceMessages tests (M5) ---

func TestReduceEmptyIsNoop(t *testing.T) {
	got := ReduceMessages(nil, 1024)
	if got != nil {
		t.Fatalf("want nil, got %d", len(got))
	}
}

func TestReduceKeepsNewestTurnWhenBudgetTiny(t *testing.T) {
	hist := []provider.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "resp1"},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "resp2"},
		{Role: "user", Content: "third"},
		{Role: "assistant", Content: "resp3"},
	}
	got := ReduceMessages(hist, 200)
	// The most recent turn (user third + assistant resp3) must survive even at
	// a tiny budget.  The leading system message is treated as part of the
	// first turn (no preceding user message).
	if len(got) == 0 {
		t.Fatal("nothing kept")
	}
	if got[len(got)-1].Role != "assistant" || !strings.Contains(got[len(got)-1].Content, "resp3") {
		t.Fatalf("newest message not retained: %+v", got[len(got)-1])
	}
}

func TestReduceKeepsFullTurns(t *testing.T) {
	var hist []provider.ChatMessage
	for i := 0; i < 20; i++ {
		hist = append(hist, provider.ChatMessage{Role: "user", Content: "u" + string(rune('a'+i))})
		hist = append(hist, provider.ChatMessage{Role: "assistant", Content: "a" + string(rune('a'+i)), ToolCalls: []provider.RequestToolCall{{ID: "tc_" + string(rune('0'+i))}}, ToolCallID: ""})
		hist = append(hist, provider.ChatMessage{Role: "tool", Content: "tool_" + string(rune('0'+i)), ToolCallID: "tc_" + string(rune('0'+i))})
	}
	got := ReduceMessages(hist, 1500)
	if len(got) == 0 {
		t.Fatal("nothing kept")
	}
	// Every tool message must have a preceding tool_call in the result.
	var tcActive bool
	for _, m := range got {
		if len(m.ToolCalls) > 0 {
			tcActive = true
		}
		if m.Role == "tool" {
			if !tcActive {
				t.Fatalf("tool message without preceding tool_call in retained slice")
			}
			tcActive = false
		}
	}
}

func TestReduceKeepsToolCallsWithTheirResponse(t *testing.T) {
	hist := []provider.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "", ToolCalls: []provider.RequestToolCall{{ID: "tc1"}}},
		{Role: "tool", Content: "tool1", ToolCallID: "tc1"},
		{Role: "assistant", Content: "final resp"},
	}
	got := ReduceMessages(hist, 200)
	// tc1+tool1 must not be split
	var sawToolCall, sawToolResp bool
	for _, m := range got {
		if len(m.ToolCalls) > 0 && m.ToolCalls[0].ID == "tc1" {
			sawToolCall = true
		}
		if m.ToolCallID == "tc1" {
			sawToolResp = true
		}
	}
	if sawToolCall != sawToolResp {
		t.Fatalf("tool_call or tool response split: sawToolCall=%v sawToolResp=%v", sawToolCall, sawToolResp)
	}
}

// SummaryMessage must include the non-authoritative marker.
func TestSummaryMessageAnnotatesNonAuthoritative(t *testing.T) {
	sm := SummaryMessage("resumo sintético")
	if !strings.Contains(sm.Content, "resumo sintético") {
		t.Fatal("summary content lost")
	}
	if !strings.Contains(sm.Content, "nao autoritativo") {
		t.Fatalf("non-authoritative marker missing: %s", sm.Content)
	}
}

// Store.Reduce calls Load and then ReduceMessages.
func TestStoreReduce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s", "session.jsonl")
	s, _ := Open(path)
	for i := 0; i < 50; i++ {
		_ = s.Append(
			provider.ChatMessage{Role: "user", Content: "turn" + string(rune('A'+i%26))},
			provider.ChatMessage{Role: "assistant", Content: "resp"},
		)
	}
	got, err := s.Reduce(3000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || len(got) >= 100 {
		t.Fatalf("unexpected keep: %d messages", len(got))
	}
}
