// Package provider defines the transport contract between AncientAI and
// remote model providers.
//
// The only mandatory adapter is the OpenAI-compatible one (SPEC §4.1): it
// covers OpenRouter, NVIDIA NIM and LiteLLM behind a single base URL.
// Chat adopts a streaming, delta-based protocol. The identity of a secret
// never leaves this package and is never echoed in an error.
package provider

import (
	"context"
	"encoding/json"
)

// ChatMessage is one message in OpenAI chat format.
type ChatMessage struct {
	Role       string            `json:"role"` // system | user | assistant | tool
	Content    string            `json:"content,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	ToolCalls  []RequestToolCall `json:"tool_calls,omitempty"`
}

// RequestToolCall is the request-side representation of a function call an
// assistant message claims it made.
type RequestToolCall struct {
	ID       string                  `json:"id"`
	Type     string                  `json:"type"`
	Function RequestToolCallFunction `json:"function"`
}

type RequestToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDef advertises a callable tool to the model.
type ToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

// NewToolDef builds a ToolDef for a function with a JSON-schema parameters
// document.
func NewToolDef(name, description string, parameters json.RawMessage) ToolDef {
	d := ToolDef{Type: "function"}
	d.Function.Name = name
	d.Function.Description = description
	d.Function.Parameters = parameters
	return d
}

// ToolCallDelta is one incremental fragment of a tool call. Per SPEC §4.2, id
// and name are set only on the first frame of a given index and arguments are
// accumulated across frames, so partial JSON must never be parsed.
type ToolCallDelta struct {
	Index             int
	ID                string
	FunctionName      string
	FunctionArguments string
}

// CompletedToolCall is a tool call whose arguments have been fully assembled.
type CompletedToolCall struct {
	ID   string
	Name string
	Args string // complete JSON document, valid only after [DONE]
}

// StreamEvent is delivered for each logical SSE delta.
type StreamEvent struct {
	DeltaContent   string
	Reasoning      string // "thinking" content from reasoning models
	ToolCallDeltas []ToolCallDelta
}

// ChatResult is the aggregated outcome of one Chat round.
type ChatResult struct {
	FinalContent string
	ToolCalls    []CompletedToolCall
	FinishReason string
}

// Provider streams the model response to onEvent and returns the aggregated
// result. Implementations MUST:
//   - verify TLS (never InsecureSkipVerify);
//   - never log or echo authorization material;
//   - bound response reads;
//   - redact any quote of remote error text before returning it.
type Provider interface {
	Chat(ctx context.Context, msgs []ChatMessage, tools []ToolDef, onEvent func(StreamEvent)) (ChatResult, error)
}

// ModelController extends Provider with session-lifetime runtime switching
// (M8, D-2026-09-16-37). It is the sub-contract the REPL (and later the TUI)
// drives through Loop.SwitchModel / Loop.SwitchProvider. Providers that do not
// implement it keep working — switches then fail closed with a clear error.
type ModelController interface {
	Provider
	Model() string
	SetModel(model string) error
	BaseURL() string
	SetBaseURL(baseURL, apiKeyEnv string) error
}
