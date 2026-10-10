package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/antonioghizidev/ancientcode/internal/redact"
)

// ToolProbeResult is the outcome of a real tool-calling probe for one model.
type ToolProbeResult struct {
	Model string
	// SupportsTools is true ONLY when the provider returned an actual tool_calls
	// block. A 200 with a prose answer is false — otherwise "the catalog said
	// tools" and "the model produced a tool call" collapse into one claim.
	SupportsTools bool
	// FinishReason is kept for diagnosis (tool_calls, stop, length…).
	FinishReason string
	// Truncated means the response hit the read ceiling, so the JSON was cut
	// mid-document. It is NOT an error: the memory bound did its job. But it also
	// cannot be reported as "no tool support" — we simply did not get to see
	// the whole answer, and saying otherwise would turn "we ran out of budget"
	// into a verdict about the model.
	Truncated bool
}

// toolProbeTimeout bounds ONE probe request. `--check` runs one request per
// candidate model, and a provider with a cold start (measured: >2 min on the
// Inspiron) must not hang the REPL for every entry in a 469-model catalog.
const toolProbeTimeout = 60 * time.Second

// toolProbeMaxRespBytes bounds the read. The answer we need is a few hundred
// bytes of JSON; anything past this is a provider misbehaving, and reading it
// all would let one model exhaust the 4 GB host.
const toolProbeMaxRespBytes = 1 << 20

// ToolProbe asks one model to make a tool call and reports whether it did.
//
// It is deliberately NOT Chat: Chat streams and mutates nothing, but it needs a
// session model and a streaming accumulator, and this needs a forced
// tool_choice and a non-streaming read. Reusing Chat would also make the probe
// depend on SSE parsing working on every provider, so a provider whose SSE
// differs would report "no tool support" for the wrong reason.
//
// The probe must not change the session's active model — it is a side query,
// and the operator asked about a catalog entry, not about their session.
func (c *Client) ToolProbe(ctx context.Context, model string) (ToolProbeResult, error) {
	var baseURL, apiKeyEnv string
	c.mu.RLock()
	baseURL, apiKeyEnv = c.baseURL, c.apiKeyEnv
	c.mu.RUnlock()

	key := c.resolveKey(apiKeyEnv)
	if key == "" {
		// Name the variable, never anything derived from its value.
		return ToolProbeResult{Model: model}, fmt.Errorf("provider: API key env %q is not set", apiKeyEnv)
	}
	if model == "" {
		return ToolProbeResult{}, fmt.Errorf("provider: sonda sem nome de modelo")
	}

	body, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "Read the file README.md."},
		},
		"tools": []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name":        "read_file",
				"description": "Read a file from disk.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]string{"type": "string"},
					},
					"required": []string{"path"},
				},
			},
		}},
		// Forcing the tool is what makes this a *probe*. Without it a chat model
		// can answer "Sure, I'll read it" in prose and score as supported while
		// the harness — which needs tool_calls to act — would be stuck.
		"tool_choice": map[string]any{
			"type":     "function",
			"function": map[string]string{"name": "read_file"},
		},
		"stream": false,
		// Small: we only need the tool call, not prose. Keeps --check affordable
		// across a 40-model page.
		"max_tokens": 64,
	})
	if err != nil {
		return ToolProbeResult{Model: model}, fmt.Errorf("provider: encode probe: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ToolProbeResult{Model: model}, fmt.Errorf("provider: create probe request: %w", err)
	}
	setVerifyHeaders(req, key)
	req.Header.Set("Content-Type", "application/json")

	var probeTimeout time.Duration
	c.mu.RLock()
	probeTimeout = c.probeTimeout
	c.mu.RUnlock()
	if probeTimeout <= 0 {
		// A zero value Client (or one built by hand) must still be bounded:
		// unbounded is exactly the failure mode this ceiling exists to prevent.
		probeTimeout = toolProbeTimeout
	}
	resp, err := boundedClient(c.http, probeTimeout).Do(req)
	if err != nil {
		return ToolProbeResult{Model: model}, fmt.Errorf("provider: probe request failed: %s", redact.Redact(err.Error()))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, toolProbeMaxRespBytes))
	if err != nil {
		return ToolProbeResult{Model: model}, fmt.Errorf("provider: probe read: %s", redact.Redact(err.Error()))
	}
	if resp.StatusCode != http.StatusOK {
		// A provider may echo the Authorization header back in its error body;
		// redact before it can reach the operator's terminal or the audit log.
		return ToolProbeResult{Model: model}, fmt.Errorf("provider: probe HTTP %d: %s",
			resp.StatusCode, redact.Redact(string(raw)))
	}

	var out probeResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		// A truncated body fails to parse. Read one byte more than the ceiling
		// to tell "the provider sent more than the ceiling" apart from "the
		// provider sent malformed JSON", because the first is a budget outcome
		// and the second is a real fault worth reporting.
		if int64(len(raw)) >= toolProbeMaxRespBytes {
			return ToolProbeResult{Model: model, Truncated: true}, nil
		}
		return ToolProbeResult{Model: model}, fmt.Errorf("provider: probe decode: %w", err)
	}
	res := ToolProbeResult{Model: model, FinishReason: out.finishReason()}
	res.SupportsTools = len(out.Choices) > 0 && len(out.Choices[0].Message.ToolCalls) > 0
	return res, nil
}

type probeResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

func (p probeResponse) finishReason() string {
	if len(p.Choices) == 0 {
		return ""
	}
	return p.Choices[0].FinishReason
}
