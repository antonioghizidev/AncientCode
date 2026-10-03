// Package model persists session history in a JSONL file, applying secret
// redaction before anything touches disk (SPEC §5.5, §5.6.3 and §9).
package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/antonioghizidev/ancientcode/internal/provider"
	"github.com/antonioghizidev/ancientcode/internal/redact"
)

// Store is an append-only, mutex-guarded session file.
type Store struct {
	path string
	mu   sync.Mutex
}

// Open ensures the parent directory exists and returns a Store bound to path.
func Open(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("model: create session dir: %w", err)
	}
	return &Store{path: path}, nil
}

// RedactMessage masks every secret-carrying field of a message. It is exported
// so the loop can apply the same rule before handing data to the provider.
func RedactMessage(m provider.ChatMessage) provider.ChatMessage {
	m.Content = redact.Redact(m.Content)
	for i := range m.ToolCalls {
		m.ToolCalls[i].Function.Arguments = redact.Redact(m.ToolCalls[i].Function.Arguments)
	}
	return m
}

// Append persists messages atomically (one JSON line each) after redaction.
func (s *Store) Append(msgs ...provider.ChatMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("model: open session for append: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, m := range msgs {
		if err := enc.Encode(RedactMessage(m)); err != nil {
			return fmt.Errorf("model: append message: %w", err)
		}
	}
	return nil
}

// Load returns the full history. A missing file yields an empty (nil) slice.
// Messages are redacted again on load as defense in depth.
func (s *Store) Load() ([]provider.ChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("model: open session for read: %w", err)
	}
	defer f.Close()

	var out []provider.ChatMessage
	dec := json.NewDecoder(f)
	for {
		var m provider.ChatMessage
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("model: corrupt session file %s: %w", s.path, err)
		}
		out = append(out, RedactMessage(m))
	}
	return out, nil
}

// Reset truncates the session file. It is destructive and therefore only
// called through an explicit command, never automatically.
func (s *Store) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("model: reset session: %w", err)
	}
	return nil
}

// ReduceMessages trims old history so the serialized bytes of the remaining
// messages fit within maxBytes, never splitting a turn: a turn is one user
// message plus every assistant/tool message that follows until the next user
// message. A tool message is never separated from the tool_calls it answers
// (SPEC §9). At least the newest turn is always kept, so a tiny budget never
// yields an empty conversation.
func ReduceMessages(hist []provider.ChatMessage, maxBytes int) []provider.ChatMessage {
	var turns [][]provider.ChatMessage
	var cur []provider.ChatMessage
	for _, m := range hist {
		if m.Role == "user" && len(cur) > 0 {
			turns = append(turns, cur)
			cur = nil
		}
		cur = append(cur, m)
	}
	if len(cur) > 0 {
		turns = append(turns, cur)
	}
	if len(turns) == 0 {
		return hist
	}

	total := 0
	var kept [][]provider.ChatMessage
	for i := len(turns) - 1; i >= 0; i-- {
		sz := turnBytes(turns[i])
		if len(kept) > 0 && total+sz > maxBytes {
			break
		}
		total += sz
		kept = append(kept, turns[i])
	}
	out := make([]provider.ChatMessage, 0, total)
	for i := len(kept) - 1; i >= 0; i-- {
		out = append(out, kept[i]...)
	}
	return out
}

// EstimateBytes approximates the serialized footprint of messages (one JSON
// line per message), mirroring what the provider actually receives for a
// history slice. It backs the M8 context meter (Loop.ContextUsage).
func EstimateBytes(msgs []provider.ChatMessage) int {
	n := 0
	for _, m := range msgs {
		if b, err := json.Marshal(m); err == nil {
			n += len(b) + 1 // newline
		}
	}
	return n
}

// turnBytes estimates the on-disk footprint of a full turn (JSON line each).
func turnBytes(turn []provider.ChatMessage) int { return EstimateBytes(turn) }

// Reduce returns a copy of the history trimmed by ReduceMessages.
func (s *Store) Reduce(maxBytes int) ([]provider.ChatMessage, error) {
	hist, err := s.Load()
	if err != nil {
		return nil, err
	}
	return ReduceMessages(hist, maxBytes), nil
}

// SummaryMessage wraps a model-generated summary as an explicitly
// non-authoritative entry: summarization is a heuristic and must never be
// mistaken for faithful conversation (SPEC M5, §9).
func SummaryMessage(summary string) provider.ChatMessage {
	return provider.ChatMessage{
		Role:    "assistant",
		Content: "[resumo de contexto — nao autoritativo]\n" + summary,
	}
}
