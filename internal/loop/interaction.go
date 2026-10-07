package loop

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/antonioghizidev/ancientcode/internal/model"
	"github.com/antonioghizidev/ancientcode/internal/provider"
	"github.com/antonioghizidev/ancientcode/internal/redact"
)

const promptHistoryCap = 20 // D-37: ≥10 prompts kept in memory, redacted.

// promptHistory is a tiny circular ring of redacted prompt strings.
type promptHistory struct {
	mu   sync.Mutex
	buf  []string
	next int // index of the next write slot (cycles once full)
	full bool
}

func (p *promptHistory) add(v string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.buf) < promptHistoryCap {
		p.buf = append(p.buf, "")
	}
	p.buf[p.next] = v
	p.next = (p.next + 1) % promptHistoryCap
	if p.next == 0 {
		p.full = true
	}
}

// list returns a copy, oldest first (insertion order).
func (p *promptHistory) list() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.buf))
	for i := 0; i < len(p.buf); i++ {
		out = append(out, p.buf[(p.next+i)%len(p.buf)])
	}
	return out
}

// at returns the i-th NEWEST prompt (0 = most recent) when it exists. The
// ring's insertion order is preserved even after wraparound.
func (p *promptHistory) at(i int) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if i < 0 || i >= len(p.buf) || len(p.buf) == 0 {
		return "", false
	}
	idx := (p.next - 1 - i + len(p.buf)) % len(p.buf)
	return p.buf[idx], true
}

// AddPrompt records a redacted prompt in the in-memory ring (M8, D-37). The
// redaction happens in the DOMAIN so every consumer (/historico, ↑/↓, a future
// web UI) can only ever see masked text.
func (l *Loop) AddPrompt(prompt string) {
	l.ph.add(redact.Redact(strings.TrimSpace(prompt)))
}

// PromptList returns a copy of the redacted prompts, oldest first.
func (l *Loop) PromptList() []string { return l.ph.list() }

// PromptAt returns the i-th newest redacted prompt (0 = newest).
func (l *Loop) PromptAt(i int) (string, bool) { return l.ph.at(i) }

// Usage reports how many history bytes the provider will receive next turn
// versus the configured budget. Budget 0 disables trimming: Pct is then -1
// ("off"), matching the footer contract of D-37.
func (l *Loop) ContextUsage() Usage {
	hist, err := l.st.Load()
	if err != nil {
		return Usage{SentBytes: 0, Budget: 0, Pct: -1}
	}
	hist = l.reduceHist(hist) // mirrors exactly what run() sends
	sent := model.EstimateBytes(hist)
	budget := l.cfg.SessionCtxBytes
	if budget <= 0 {
		return Usage{SentBytes: sent, Budget: 0, Pct: -1}
	}
	pct := sent * 100 / budget
	if pct > 100 {
		pct = 100
	}
	return Usage{SentBytes: sent, Budget: budget, Pct: pct}
}

// CurrentModel returns the active model ("" when the provider has no runtime
// controller).
func (l *Loop) CurrentModel() string {
	if pc, ok := l.p.(providerModelController); ok {
		return pc.Model()
	}
	return ""
}

// CurrentProviderBaseURL returns the active base URL.
func (l *Loop) CurrentProviderBaseURL() string {
	if pc, ok := l.p.(providerModelController); ok {
		return pc.BaseURL()
	}
	return ""
}

// SwitchModel changes the remote model for the rest of the session (D-37: per
// session, never persisted in the YAML). Fail-closed: blank names and
// controllers that do not support switching are refused.
func (l *Loop) SwitchModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return errors.New("loop: model vazio; switch recusado (fail-closed)")
	}
	pc, ok := l.p.(providerModelController)
	if !ok {
		return errors.New("loop: provider atual nao suporta troca de modelo em tempo de execucao")
	}
	if err := pc.SetModel(model); err != nil {
		return err
	}
	l.emit(Event{Phase: PhaseProviderSwitch, Tool: "model", Detail: "model: " + model})
	return nil
}

// SwitchProvider changes the session endpoint to one of the built-in named
// providers (openrouter|nvidia|litellm). The switch is per session only; the
// provider's api key env must already be set (the provider side fails closed
// before any state change).
func (l *Loop) SwitchProvider(name string) error {
	base, env, ok := l.cfg.ProviderDefault(name) // name lookup, not the zone
	if !ok {
		return fmt.Errorf("loop: provider %q desconhecido; opcoes: %s", name, strings.Join(l.ProviderNames(), ", "))
	}
	pc, ok := l.p.(providerModelController)
	if !ok {
		return errors.New("loop: provider atual nao suporta troca de provedor em tempo de execucao")
	}
	if err := pc.SetBaseURL(base, env); err != nil {
		return err
	}
	l.emit(Event{Phase: PhaseProviderSwitch, Tool: "provider", Detail: "provider: " + strings.ToLower(strings.TrimSpace(name))})
	return nil
}

// ProviderNames returns the switchable built-in provider names.
func (l *Loop) ProviderNames() []string {
	return l.cfg.ProviderNames()
}

// CurrentProvider returns the active provider id, lowercased. The REPL needs it
// to answer "which key am I using?" without the caller having to remember that
// /provider lowercases its argument (M14).
func (l *Loop) CurrentProvider() string {
	return strings.ToLower(strings.TrimSpace(l.cfg.Provider))
}

// CurrentAPIKeyEnv returns the environment variable name that api_key_env
// resolves to for the active provider. The name is not a secret.
func (l *Loop) CurrentAPIKeyEnv() string {
	return l.cfg.APIKeyEnv
}

// ProviderBaseURLFor returns the default base URL and env name of a named
// provider, so /connect can verify a candidate key against the same endpoint
// the switch would install.
func (l *Loop) ProviderBaseURLFor(name string) (baseURL, apiKeyEnv string, ok bool) {
	return l.cfg.ProviderDefault(name)
}

// providerModelController is the runtime-switch sub-contract the loop depends
// on (DIP, M8 §2): Loop talks to the interface, never to *provider.Client.
type providerModelController = provider.ModelController
