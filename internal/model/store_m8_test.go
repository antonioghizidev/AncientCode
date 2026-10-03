package model

import (
	"testing"

	"github.com/antonioghizidev/ancientcode/internal/provider"
)

func TestEstimateBytesMatchesTurnBytes(t *testing.T) {
	hist := []provider.ChatMessage{
		{Role: "system", Content: "voce e um agente"},
		{Role: "user", Content: "ola mundo"},
		{Role: "assistant", Content: "ok", ToolCalls: []provider.RequestToolCall{
			{ID: "1", Type: "function", Function: provider.RequestToolCallFunction{Name: "shell", Arguments: `{"argv":["id"]}`}},
		}},
	}
	if got := EstimateBytes(hist); got == 0 {
		t.Fatal("EstimateBytes returned 0 for non-empty history")
	}
	if got, want := EstimateBytes(hist), turnBytes(hist); got != want {
		t.Fatalf("EstimateBytes = %d, want %d (must match ReduceMessages accounting)", got, want)
	}
}

func TestEstimateBytesEmpty(t *testing.T) {
	if got := EstimateBytes(nil); got != 0 {
		t.Fatalf("EstimateBytes(nil) = %d, want 0", got)
	}
}
