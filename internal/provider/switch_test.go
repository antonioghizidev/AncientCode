package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSetModelAndGetModel verifies the session-lifetime model switch.
func TestSetModelAndGetModel(t *testing.T) {
	c := NewOpenAICompatible("https://example.invalid/v1", "orig/model", "TEST_OPENAI_KEY", nil)
	if got := c.Model(); got != "orig/model" {
		t.Fatalf("Model() = %q, want orig/model", got)
	}
	if err := c.SetModel("next/model"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if got := c.Model(); got != "next/model" {
		t.Fatalf("Model() = %q after switch, want next/model", got)
	}
}

// TestSetModelBlankRejected ensures a blank model is refused (fail-closed).
func TestSetModelBlankRejected(t *testing.T) {
	c := NewOpenAICompatible("https://example.invalid/v1", "m", "TEST_OPENAI_KEY", nil)
	if err := c.SetModel("   "); err == nil {
		t.Fatal("SetModel(blank) accepted, want error")
	}
	if got := c.Model(); got != "m" {
		t.Fatalf("Model() = %q after failed switch, want unchanged m", got)
	}
}

// TestSetBaseURLMissingKeyRefused worries about fail-closed switch: the env
// var for the new key must already be set; otherwise nothing changes.
func TestSetBaseURLMissingKeyRefused(t *testing.T) {
	c := NewOpenAICompatible("https://old.invalid/v1", "m", "TEST_OPENAI_KEY", nil)
	t.Setenv("TEST_OPENAI_KEY", testKey)
	err := c.SetBaseURL("https://new.invalid/v1", "NONEXISTENT_ENV_VAR_FOR_SWITCH")
	if err == nil {
		t.Fatal("SetBaseURL accepted an env var that is not set; want fail-closed error")
	}
	if got := c.BaseURL(); got != "https://old.invalid/v1" {
		t.Fatalf("BaseURL() = %q after failed switch, want unchanged https://old.invalid/v1", got)
	}
}

// TestSetBaseURLRefusesPlainHTTPNonLoopback keeps TLS posture intact on
// runtime switches (no downgrade, mirroring config.Validate).
func TestSetBaseURLRefusesPlainHTTPNonLoopback(t *testing.T) {
	c := NewOpenAICompatible("https://old.invalid/v1", "m", "TEST_OPENAI_KEY", nil)
	t.Setenv("TEST_SWITCH_KEY", testKey)
	if err := c.SetBaseURL("http://remote.invalid/v1", "TEST_SWITCH_KEY"); err == nil {
		t.Fatal("SetBaseURL accepted plain http non-loopback; want refusal")
	}
}

// TestSetBaseURLLoopbackHTTPAllowed keeps LiteLLM-style local providers usable.
func TestSetBaseURLLoopbackHTTPAllowed(t *testing.T) {
	c := NewOpenAICompatible("https://old.invalid/v1", "m", "TEST_OPENAI_KEY", nil)
	t.Setenv("TEST_SWITCH_KEY", testKey)
	if err := c.SetBaseURL("http://127.0.0.1:4000/v1", "TEST_SWITCH_KEY"); err != nil {
		t.Fatalf("SetBaseURL loopback http: %v", err)
	}
	if got := c.BaseURL(); got != "http://127.0.0.1:4000/v1" {
		t.Fatalf("BaseURL() = %q", got)
	}
}

// TestChatUsesSwitchedModelAndBaseURL proves the switch affects the very next
// request (D-37: "troca vale daqui em diante, sem persistir no YAML").
func TestChatUsesSwitchedModelAndBaseURL(t *testing.T) {
	var gotModel, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var req struct {
			Model string `json:"model"`
		}
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &req); err != nil {
			t.Fatal(err)
		}
		gotModel = req.Model
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, deltaFrame(t, map[string]any{}, "stop")+"data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := NewOpenAICompatible("https://old.invalid/v1", "old/model", "TEST_SWITCH_KEY", nil)
	t.Setenv("TEST_SWITCH_KEY", testKey)
	if err := c.SetBaseURL(srv.URL+"/v9", "TEST_SWITCH_KEY"); err != nil {
		t.Fatalf("SetBaseURL: %v", err)
	}
	if err := c.SetModel("new/model"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if _, err := c.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotPath != "/v9/chat/completions" {
		t.Errorf("path = %q, want /v9/chat/completions", gotPath)
	}
	if gotModel != "new/model" {
		t.Errorf("model = %q, want new/model", gotModel)
	}
}

func TestModelControllerInterface(t *testing.T) {
	if _, ok := any(NewOpenAICompatible("https://x/v1", "m", "TEST_OPENAI_KEY", nil)).(ModelController); !ok {
		t.Fatal("Client must implement ModelController (M8 runtime switch contract)")
	}
}
