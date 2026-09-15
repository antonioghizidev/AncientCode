package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"ancientai/internal/redact"
)

// maxResponseBytesDefault bounds the SSE response body read to fail closed on
// runaway streams (SPEC §7.1 "bomba de zip/streams").
const maxResponseBytesDefault = 16 << 20 // 16 MiB

// maxErrorResponseBytes bounds how much of a non-2xx body is consumed for the
// error message.
const maxErrorResponseBytes = 64 << 10 // 64 KiB

// Client is a streaming OpenAI-compatible chat client. It is the single
// adapter that covers OpenRouter, NVIDIA NIM and LiteLLM (SPEC §4.1).
type Client struct {
	baseURL      string
	model        string
	apiKeyEnv    string
	http         *http.Client
	maxRespBytes int64
}

// NewOpenAICompatible returns a client for the given base URL. The API key is
// read from the environment variable apiKeyEnv on every request — the secret
// never lives in the configuration.
func NewOpenAICompatible(baseURL, model, apiKeyEnv string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 2 * time.Minute}
	}
	return &Client{
		baseURL:      strings.TrimRight(baseURL, "/"),
		model:        model,
		apiKeyEnv:    apiKeyEnv,
		http:         hc,
		maxRespBytes: maxResponseBytesDefault,
	}
}

// redactedError redacts the message while keeping the original error in the
// unwrap chain, so errors.Is and errors.As keep working (e.g. against
// ErrProviderError).
type redactedError struct {
	msg   string
	cause error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.cause }

func (c *Client) Chat(ctx context.Context, msgs []ChatMessage, tools []ToolDef, onEvent func(StreamEvent)) (ChatResult, error) {
	key := os.Getenv(c.apiKeyEnv)
	if key == "" {
		// Report the variable NAME, never anything derived from its value.
		return ChatResult{}, fmt.Errorf("provider: API key env %q is not set", c.apiKeyEnv)
	}

	payload := struct {
		Model    string        `json:"model"`
		Messages []ChatMessage `json:"messages"`
		Tools    []ToolDef     `json:"tools,omitempty"`
		Stream   bool          `json:"stream"`
	}{
		Model:    c.model,
		Messages: msgs,
		Tools:    tools,
		Stream:   true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ChatResult{}, fmt.Errorf("provider: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ChatResult{}, fmt.Errorf("provider: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("User-Agent", "ancientai/0.1")

	resp, err := c.http.Do(req)
	if err != nil {
		return ChatResult{}, fmt.Errorf("provider: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorResponseBytes))
		// Redact in case the remote echoes secret material back at us.
		return ChatResult{}, fmt.Errorf("provider: HTTP %d: %s", resp.StatusCode, redact.Redact(string(raw)))
	}

	acc := newAccumulator()
	finish, err := parseSSE(io.LimitReader(resp.Body, c.maxRespBytes), func(ev StreamEvent) {
		acc.apply(ev)
		if onEvent != nil {
			onEvent(ev)
		}
	})
	if err != nil {
		// Redact the message from remote failures, while preserving the
		// sentinel error (e.g. ErrProviderError) in the cause chain.
		return ChatResult{}, &redactedError{msg: redact.Redact(err.Error()), cause: err}
	}
	res, err := acc.result()
	if err != nil {
		return ChatResult{}, err
	}
	res.FinishReason = finish
	return res, nil
}
