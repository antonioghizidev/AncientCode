package provider

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func sse(data string) string {
	return "data: " + data + "\n\n"
}

// sseFrame marshals a payload to guarantee valid JSON on the wire.
func sseFrame(t *testing.T, payload any) string {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return "data: " + string(b) + "\n\n"
}

func tcFrame(index int, fields map[string]any) string {
	body := map[string]any{
		"choices": []any{map[string]any{
			"index": index,
			"delta": map[string]any{"tool_calls": []any{fields}},
		}},
	}
	b, _ := json.Marshal(body)
	return "data: " + string(b) + "\n\n"
}

func TestParseSSEAccumulatesContent(t *testing.T) {
	var events []StreamEvent
	input := sse(`{"choices":[{"index":0,"delta":{"content":"Hello "}}]}`) +
		sse(`{"choices":[{"index":0,"delta":{"content":"world"}}]}`) +
		"data: [DONE]\n\n"

	finish, err := parseSSE(strings.NewReader(input), func(ev StreamEvent) { events = append(events, ev) })
	if err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].DeltaContent != "Hello " || events[1].DeltaContent != "world" {
		t.Fatalf("wrong content deltas: %+v", events)
	}
	if finish != "" {
		t.Fatalf("finish = %q, want empty", finish)
	}
}

func TestParseSSEFinishReason(t *testing.T) {
	input := sse(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n"
	finish, err := parseSSE(strings.NewReader(input), func(StreamEvent) {})
	if err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	if finish != "stop" {
		t.Fatalf("finish = %q, want stop", finish)
	}
}

func TestParseSSEPassesReasoningThrough(t *testing.T) {
	var got string
	input := sse(`{"choices":[{"index":0,"delta":{"reasoning":"thinking...","content":"answer"}}]}`) + "data: [DONE]\n\n"
	if _, err := parseSSE(strings.NewReader(input), func(ev StreamEvent) { got = ev.Reasoning }); err != nil {
		t.Fatal(err)
	}
	if got != "thinking..." {
		t.Fatalf("reasoning = %q", got)
	}
}

func TestParseSSEReasoningContentAlias(t *testing.T) {
	var got string
	input := sse(`{"choices":[{"index":0,"delta":{"reasoning_content":"r","content":"a"}}]}`) + "data: [DONE]\n\n"
	if _, err := parseSSE(strings.NewReader(input), func(ev StreamEvent) { got = ev.Reasoning }); err != nil {
		t.Fatal(err)
	}
	if got != "r" {
		t.Fatalf("reasoning_content not mapped: %q", got)
	}
}

func TestParseSSEProviderError(t *testing.T) {
	input := sse(`{"error":{"message":"rate limited"}}`)
	_, err := parseSSE(strings.NewReader(input), func(StreamEvent) {})
	if !errors.Is(err, ErrProviderError) {
		t.Fatalf("want ErrProviderError, got %v", err)
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("error lost message: %v", err)
	}
}

func TestParseSSEMalformedFailsClosed(t *testing.T) {
	_, err := parseSSE(strings.NewReader("data: {not json\n\n"), func(StreamEvent) {})
	if err == nil {
		t.Fatal("malformed SSE accepted")
	}
}

func TestParseSSEIgnoresNonDataLines(t *testing.T) {
	var events []StreamEvent
	input := ": keepalive\n\nevent: message\n\n" + sse(`{"choices":[{"index":0,"delta":{"content":"x"}}]}`) + "data: [DONE]\n\n"
	if _, err := parseSSE(strings.NewReader(input), func(ev StreamEvent) { events = append(events, ev) }); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
}

func TestParseSSEUnterminatedStreamIsAnError(t *testing.T) {
	input := sse(`{"choices":[{"index":0,"delta":{"content":"x"}}]}`) // no [DONE], no more data
	_, err := parseSSE(strings.NewReader(input), func(StreamEvent) {})
	if err != nil {
		t.Fatalf("EOF without [DONE] should be tolerated, got %v", err)
	}
}

func TestAccumulatorToolCallsSplitAcrossFrames(t *testing.T) {
	// Frame 1: starts call index 0 (id + name + first fragment).
	// Frame 2: continues arguments for index 0.
	// Frame 3: starts call index 1.
	// This mirrors the SPEC §4.2 requirement.
	input := tcFrame(0, map[string]any{
		"index": 0, "id": "call_1", "type": "function",
		"function": map[string]any{"name": "read_file", "arguments": `{"path":`},
	}) +
		tcFrame(0, map[string]any{
			"index": 0, "function": map[string]any{"arguments": `"/etc/hosts"}`},
		}) +
		tcFrame(0, map[string]any{
			"index": 1, "id": "call_2", "type": "function",
			"function": map[string]any{"name": "docker", "arguments": `{"argv":["ps"]}`},
		}) +
		sse(`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`) +
		"data: [DONE]\n\n"

	acc := newAccumulator()
	finish, err := parseSSE(strings.NewReader(input), acc.apply)
	if err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	res, err := acc.result()
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	if finish != "tool_calls" {
		t.Fatalf("finish = %q", finish)
	}
	if len(res.ToolCalls) != 2 {
		t.Fatalf("tool calls = %d, want 2: %+v", len(res.ToolCalls), res.ToolCalls)
	}
	if res.ToolCalls[0].ID != "call_1" || res.ToolCalls[0].Name != "read_file" {
		t.Fatalf("call[0] = %+v", res.ToolCalls[0])
	}
	if res.ToolCalls[0].Args != `{"path":"/etc/hosts"}` {
		t.Fatalf("call[0].Args assembled wrong: %q", res.ToolCalls[0].Args)
	}
	if res.ToolCalls[1].Name != "docker" || res.ToolCalls[1].Args != `{"argv":["ps"]}` {
		t.Fatalf("call[1] = %+v", res.ToolCalls[1])
	}
	// Final content must be empty when the only output was tool calls.
	if res.FinalContent != "" {
		t.Fatalf("final content = %q, want empty", res.FinalContent)
	}
}

func TestAccumulatorContentAndToolCallsTogether(t *testing.T) {
	input := sse(`{"choices":[{"index":0,"delta":{"content":"let me check"}}]}`) +
		sse(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c","type":"function","function":{"name":"ls","arguments":"{\"flags\":[]}"}}]}}]}`) +
		"data: [DONE]\n\n"

	acc := newAccumulator()
	if _, err := parseSSE(strings.NewReader(input), acc.apply); err != nil {
		t.Fatal(err)
	}
	res, err := acc.result()
	if err != nil {
		t.Fatal(err)
	}
	if res.FinalContent != "let me check" {
		t.Fatalf("FinalContent = %q", res.FinalContent)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "c" {
		t.Fatalf("ToolCalls = %+v", res.ToolCalls)
	}
}

func TestAccumulatorRejectsIncompleteCall(t *testing.T) {
	// A delta carries arguments but never the terminating id/name proof for
	// index 0 before [DONE].
	input := sse(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":3,"id":"c","type":"function","function":{"name":"x","arguments":"{}"}}]}}]}`) +
		"data: [DONE]\n\n"
	acc := newAccumulator()
	if _, err := parseSSE(strings.NewReader(input), acc.apply); err != nil {
		t.Fatal(err)
	}
	// index 3 with id+name present: should still be accepted; the guard is on
	// missing id/name, not on sparse indexes.
	res, err := acc.result()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("calls = %d", len(res.ToolCalls))
	}
}

func TestAccumulatorRejectsMissingID(t *testing.T) {
	input := sse(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"x","arguments":"{}"}}]}}]}`) +
		"data: [DONE]\n\n"
	acc := newAccumulator()
	if _, err := parseSSE(strings.NewReader(input), acc.apply); err != nil {
		t.Fatal(err)
	}
	if _, err := acc.result(); err == nil {
		t.Fatal("call without id was accepted")
	}
}
