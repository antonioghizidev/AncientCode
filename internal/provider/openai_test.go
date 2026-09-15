package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testKey = "sk-abcdefGHIJKLMNOPQRSTUVWXyz123456"

func newTestClient(t *testing.T, srvURL string) *Client {
	t.Helper()
	t.Setenv("TEST_OPENAI_KEY", testKey)
	return NewOpenAICompatible(srvURL, "test/model", "TEST_OPENAI_KEY", nil)
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// deltaFrame builds one valid SSE "data:" line for a single choice with the
// given delta; finish is the finish_reason when non-empty.
func deltaFrame(t *testing.T, delta any, finish string) string {
	t.Helper()
	choice := map[string]any{"index": 0, "delta": delta}
	if finish != "" {
		choice["finish_reason"] = finish
	}
	return "data: " + mustMarshal(t, map[string]any{"choices": []any{choice}}) + "\n\n"
}

func TestChatStreamsContent(t *testing.T) {
	body := deltaFrame(t, map[string]any{"content": "Hello "}, "") +
		deltaFrame(t, map[string]any{"content": "world"}, "") +
		deltaFrame(t, map[string]any{}, "stop") +
		"data: [DONE]\n\n"

	var gotAuth, gotModel string
	var gotStream bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var req struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &req); err != nil {
			t.Fatal(err)
		}
		gotModel, gotStream = req.Model, req.Stream
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, body)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL+"/v1")
	var streamed []StreamEvent
	res, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil, func(ev StreamEvent) {
		streamed = append(streamed, ev)
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotAuth != "Bearer "+testKey {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotModel != "test/model" || !gotStream {
		t.Errorf("payload model=%q stream=%v", gotModel, gotStream)
	}
	if res.FinalContent != "Hello world" {
		t.Fatalf("FinalContent = %q", res.FinalContent)
	}
	if res.FinishReason != "stop" {
		t.Errorf("FinishReason = %q", res.FinishReason)
	}
	if len(streamed) != 3 {
		t.Errorf("streamed events = %d", len(streamed))
	}
}

func TestChatAssemblesToolCallsOverWire(t *testing.T) {
	body := deltaFrame(t, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call_x", "type": "function", "function": map[string]any{"name": "docker", "arguments": `{"argv":["`}}}}, "") +
		deltaFrame(t, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": `ps"]}`}}}}, "") +
		deltaFrame(t, map[string]any{}, "tool_calls") +
		"data: [DONE]\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, body)
	}))
	defer srv.Close()

	res, err := newTestClient(t, srv.URL+"/v1").Chat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d", len(res.ToolCalls))
	}
	tc := res.ToolCalls[0]
	if tc.Name != "docker" || tc.ID != "call_x" {
		t.Fatalf("tool call = %+v", tc)
	}
	if tc.Args != `{"argv":["ps"]}` {
		t.Fatalf("args assembled = %q", tc.Args)
	}
	if res.FinishReason != "tool_calls" {
		t.Errorf("finish = %q", res.FinishReason)
	}
}

func TestChatSendsToolsInPayload(t *testing.T) {
	var gotTools []struct {
		Type string `json:"type"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools []struct {
				Type string `json:"type"`
			} `json:"tools"`
		}
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &req); err != nil {
			t.Fatal(err)
		}
		gotTools = req.Tools
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "[DONE]")
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv.URL+"/v1").Chat(context.Background(), nil,
		[]ToolDef{NewToolDef("ping", "pings", nil)}, nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(gotTools) != 1 || gotTools[0].Type != "function" {
		t.Fatalf("tools payload = %+v", gotTools)
	}
}

func TestChatNon200ErrorDoesNotLeakKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hostile echo: pretend the server echoes the Authorization header.
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "auth: "+r.Header.Get("Authorization"))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv.URL+"/v1").Chat(context.Background(), nil, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should carry status code: %v", err)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatalf("error leaked the API key: %v", err)
	}
}

func TestChatProviderErrorEventIsRedacted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Echo a fake secret in the provider error message.
		io.WriteString(w, `data: {"error":{"message":"bananas nvapi-abcdefGHIJKLMNOPQRSTUVWXYZ123456"}}`)
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv.URL+"/v1").Chat(context.Background(), nil, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrProviderError) {
		t.Fatalf("want ErrProviderError, got %v", err)
	}
	if strings.Contains(err.Error(), "nvapi-abcdefGHIJKLMNOPQRSTUVWXYZ123456") {
		t.Fatalf("error echoed a secret: %v", err)
	}
}

func TestChatRespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-make(chan struct{}): // never
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newTestClient(t, srv.URL+"/v1").Chat(ctx, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestChatFailsClosedOnOversizedBody(t *testing.T) {
	big := strings.Repeat("x", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 2000; i++ {
			io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":"`+big+`"}}]}`+"\n")
		}
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL+"/v1")
	client.maxRespBytes = 64 * 1024 // tiny ceiling for the test
	_, err := client.Chat(context.Background(), nil, nil, nil)
	if err == nil {
		t.Fatal("oversized stream was accepted")
	}
}

func TestChatMissingAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	client := NewOpenAICompatible(srv.URL+"/v1", "m", "UNSET_ENV_VARIABLE_987654", nil)
	_, err := client.Chat(context.Background(), nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	if !strings.Contains(err.Error(), "UNSET_ENV_VARIABLE_987654") {
		t.Errorf("error should name the env var: %v", err)
	}
}
