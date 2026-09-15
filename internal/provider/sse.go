package provider

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrProviderError identifies an error reported by the remote end, not by us.
var ErrProviderError = errors.New("provider reported an error")

// maxSSELineBytes bounds a single SSE line. Tool-call arguments can legally
// grow past the default 64 KiB scanner limit, so the scanner buffer is
// raised — but still bounded to fail closed on runaway streams.
const maxSSELineBytes = 8 << 20 // 8 MiB

type sseDelta struct {
	Content          string `json:"content"`
	Reasoning        string `json:"reasoning"`         // OpenRouter-style
	ReasoningContent string `json:"reasoning_content"` // OpenAI-style
	ToolCalls        []struct {
		Index    int    `json:"index"`
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

type sseChoice struct {
	Index        int      `json:"index"`
	Delta        sseDelta `json:"delta"`
	FinishReason string   `json:"finish_reason"`
}

type ssePayload struct {
	Choices []sseChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// parseSSE reads a Server-Sent-Events body and feeds each logical delta to
// onEvent. It fails closed: malformed payloads and provider error events
// short-circuit the stream with a non-nil error. A provider-declared error is
// wrapped in ErrProviderError.
func parseSSE(r io.Reader, onEvent func(StreamEvent)) (finishReason string, err error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4096), maxSSELineBytes)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue // comments, keep-alives and event: lines are irrelevant here
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

		if data == "[DONE]" {
			return finishReason, nil
		}

		var p ssePayload
		if err := json.Unmarshal([]byte(data), &p); err != nil {
			return finishReason, fmt.Errorf("provider: malformed SSE payload: %w", err)
		}
		if p.Error != nil {
			return finishReason, fmt.Errorf("%w: %s", ErrProviderError, p.Error.Message)
		}
		for _, ch := range p.Choices {
			reasoning := ch.Delta.Reasoning
			if reasoning == "" {
				reasoning = ch.Delta.ReasoningContent
			}
			deltas := make([]ToolCallDelta, 0, len(ch.Delta.ToolCalls))
			for _, tc := range ch.Delta.ToolCalls {
				deltas = append(deltas, ToolCallDelta{
					Index:             tc.Index,
					ID:                tc.ID,
					FunctionName:      tc.Function.Name,
					FunctionArguments: tc.Function.Arguments,
				})
			}
			onEvent(StreamEvent{
				DeltaContent:   ch.Delta.Content,
				Reasoning:      reasoning,
				ToolCallDeltas: deltas,
			})
			if ch.FinishReason != "" {
				finishReason = ch.FinishReason
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return finishReason, fmt.Errorf("provider: SSE read: %w", err)
	}
	return finishReason, nil
}

// accumulator is a parseSSE callback sink that implements the delta
// accumulation rule of SPEC §4.2.
type accumulator struct {
	content   string
	reasoning string
	calls     map[int]*toolCallAcc
	order     []int
}

type toolCallAcc struct {
	id   string
	name string
	args string
}

const maxAccumulatedToolCalls = 128

func newAccumulator() *accumulator {
	return &accumulator{calls: make(map[int]*toolCallAcc)}
}

func (a *accumulator) apply(ev StreamEvent) {
	if ev.DeltaContent != "" {
		a.content += ev.DeltaContent
	}
	if ev.Reasoning != "" && a.reasoning == "" {
		// The final reasoning snapshot is all we need for M1; later streams
		// replace it incrementally — taking the first non-empty chunk keeps
		// output stable.
		a.reasoning = ev.Reasoning
	}
	for _, d := range ev.ToolCallDeltas {
		acc, ok := a.calls[d.Index]
		if !ok {
			acc = &toolCallAcc{}
			a.calls[d.Index] = acc
			a.order = append(a.order, d.Index)
		}
		acc.id += d.ID
		acc.name += d.FunctionName
		acc.args += d.FunctionArguments
	}
}

func (a *accumulator) result() (ChatResult, error) {
	if len(a.order) > maxAccumulatedToolCalls {
		return ChatResult{}, fmt.Errorf("provider: too many tool calls (%d > %d)", len(a.order), maxAccumulatedToolCalls)
	}
	out := ChatResult{FinalContent: a.content}
	for _, idx := range a.order {
		acc := a.calls[idx]
		if acc.id == "" || acc.name == "" {
			return ChatResult{}, fmt.Errorf("provider: incomplete tool call at index %d (missing id or name)", idx)
		}
		out.ToolCalls = append(out.ToolCalls, CompletedToolCall{
			ID:   acc.id,
			Name: acc.name,
			Args: acc.args,
		})
	}
	return out, nil
}
