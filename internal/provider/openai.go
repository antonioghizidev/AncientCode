package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/antonioghizidev/ancientcode/internal/redact"
)

// maxResponseBytesDefault bounds the SSE response body read to fail closed on
// runaway streams (SPEC §7.1 "bomba de zip/streams").
const maxResponseBytesDefault = 16 << 20 // 16 MiB

// maxErrorResponseBytes bounds how much of a non-2xx body is consumed for the
// error message.
const maxErrorResponseBytes = 64 << 10 // 64 KiB

// Client is a streaming OpenAI-compatible chat client. It is the single
// adapter that covers OpenRouter, NVIDIA NIM and LiteLLM (SPEC §4.1). M8:
// base URL, model and API key env are session-switchable via ModelController.
type Client struct {
	mu           sync.RWMutex
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
		// Default request ceiling sized for slow real providers (NVIDIA NIM /
		// OpenRouter first-token latency on low-end hardware). Applies per
		// HTTP request; the loop cap and SSE limits still bound the turn.
		hc = &http.Client{Timeout: 5 * time.Minute}
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

// Model returns the active model name (M8 runtime switch).
func (c *Client) Model() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.model
}

// SetModel switches the model for subsequent requests. The switch is per
// session (D-37) and never persists to any YAML. A blank name is refused.
func (c *Client) SetModel(model string) error {
	if strings.TrimSpace(model) == "" {
		return errors.New("provider: model vazio; switch recusado (fail-closed)")
	}
	c.mu.Lock()
	c.model = model
	c.mu.Unlock()
	return nil
}

// BaseURL returns the active base URL (M8 runtime switch).
func (c *Client) BaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

// SetBaseURL switches the endpoint and its API key env var for subsequent
// requests. Mirroring config.Validate and the security contract (SPEC §8):
//   - the env var must ALREADY be set, else the switch is refused before any
//     state changes (fail-closed, state unchanged);
//   - TLS posture is never relaxed: plain http is allowed only for loopback
//     hosts (self-hosted LiteLLM), exactly like the startup validation.
func (c *Client) SetBaseURL(baseURL, apiKeyEnv string) error {
	baseURL = strings.TrimSpace(baseURL)
	apiKeyEnv = strings.TrimSpace(apiKeyEnv)
	if baseURL == "" || apiKeyEnv == "" {
		return errors.New("provider: base_url e api_key_env sao obrigatorios; switch recusado")
	}
	if os.Getenv(apiKeyEnv) == "" {
		return fmt.Errorf("provider: API key env %q nao definida; switch recusado (fail-closed)", apiKeyEnv)
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("provider: base_url invalida: %w", err)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return errors.New("provider: base_url http nao-loopback e insegura; switch recusado")
		}
	default:
		return fmt.Errorf("provider: schema %q deve ser http(s)", u.Scheme)
	}
	c.mu.Lock()
	c.baseURL = strings.TrimRight(baseURL, "/")
	c.apiKeyEnv = apiKeyEnv
	c.mu.Unlock()
	return nil
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c *Client) Chat(ctx context.Context, msgs []ChatMessage, tools []ToolDef, onEvent func(StreamEvent)) (ChatResult, error) {
	var baseURL, model, apiKeyEnv string
	c.mu.RLock()
	baseURL, model, apiKeyEnv = c.baseURL, c.model, c.apiKeyEnv
	c.mu.RUnlock()

	key := os.Getenv(apiKeyEnv)
	if key == "" {
		// Report the variable NAME, never anything derived from its value.
		return ChatResult{}, fmt.Errorf("provider: API key env %q is not set", apiKeyEnv)
	}

	payload := struct {
		Model    string        `json:"model"`
		Messages []ChatMessage `json:"messages"`
		Tools    []ToolDef     `json:"tools,omitempty"`
		Stream   bool          `json:"stream"`
	}{
		Model:    model,
		Messages: msgs,
		Tools:    tools,
		Stream:   true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ChatResult{}, fmt.Errorf("provider: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ChatResult{}, fmt.Errorf("provider: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("User-Agent", "ancientcode/0.1")

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
