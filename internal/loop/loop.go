// Package loop implements the agentic turn loop (SPEC §3.3). In milestone 1
// the loop is deliberately single-turn and fails closed when the model asks
// for tools that are not yet registered: refusing is always safer than
// guessing.
package loop

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"ancientai/internal/config"
	"ancientai/internal/model"
	"ancientai/internal/provider"
	"ancientai/internal/redact"
)

// Loop coordinates a provider, a session store and the configuration.
type Loop struct {
	p   provider.Provider
	st  *model.Store
	cfg *config.Config
}

func New(p provider.Provider, st *model.Store, cfg *config.Config) *Loop {
	return &Loop{p: p, st: st, cfg: cfg}
}

// Turn processes one user message. The message is redacted before it is
// persisted or sent; the assistant reply is persisted too. If the model
// attempts a tool call, Turn fails closed (tool calling arrives in milestone
// 2) and nothing is persisted for it.
func (l *Loop) Turn(ctx context.Context, user string) (string, error) {
	user = redact.Redact(user)
	if strings.TrimSpace(user) == "" {
		return "", errors.New("loop: empty message")
	}

	if err := l.st.Append(provider.ChatMessage{Role: "user", Content: user}); err != nil {
		return "", fmt.Errorf("loop: persist user message: %w", err)
	}

	hist, err := l.st.Load()
	if err != nil {
		return "", fmt.Errorf("loop: load history: %w", err)
	}

	res, err := l.p.Chat(ctx, hist, nil, nil)
	if err != nil {
		return "", fmt.Errorf("loop: chat: %w", err)
	}

	if len(res.ToolCalls) > 0 {
		return "", errors.New("loop: model requested tool calls but tools are not enabled (milestone 2); refusing")
	}

	if err := l.st.Append(provider.ChatMessage{Role: "assistant", Content: res.FinalContent}); err != nil {
		return "", fmt.Errorf("loop: persist assistant message: %w", err)
	}
	return res.FinalContent, nil
}
