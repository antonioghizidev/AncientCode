package model

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"ancientai/internal/provider"
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
