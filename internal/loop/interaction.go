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

// ModelMemory is the last-model-per-provider port (M14 §3.7). The loop depends
// on the interface, not on *state.Store, so this package keeps its dependency
// direction (DIP, like Approver and provider.ModelController).
type ModelMemory interface {
	GetModel(provider string) (model string, ok bool, err error)
	SetModel(provider, model string) error
}

// SetModelMemory installs the persistent per-provider model memory. nil
// disables it and restores the pre-M14 behaviour (model is session-only).
//
// Installing it ALSO restores the model remembered for the CURRENT provider.
// The Inspiron smoke caught why that matters: `/model X` wrote state.json
// correctly, and the next session started on the config's model anyway — the
// memory was write-only, so the only way back to X was to switch providers away
// and return. A store nothing reads is not persistence.
//
// The remembered model deliberately outranks config.model: it is the operator's
// LATER explicit choice, and config.model is the default they have not touched.
// Fail-soft throughout: an unreadable state file must not stop a session.
func (l *Loop) SetModelMemory(m ModelMemory) {
	l.modelMemory = m
	l.modelRestored = false
	if m == nil {
		return
	}
	l.restoreRememberedModel(l.CurrentProvider())
}

// ModelRestoredFromMemory reports whether the active model came from state.json
// rather than from the config.
//
// The loop is the single source of truth here on purpose. The banner used to
// re-read state.json to decide, while the loop had been told by the injected
// memory — two sources of truth that can disagree, and the disagreement is
// exactly the "why is this model different from my YAML?" question the banner
// is trying to answer.
func (l *Loop) ModelRestoredFromMemory() bool { return l.modelRestored }

// SwitchModel changes the remote model for the rest of the session (D-37) and —
// unlike the YAML, which never holds a model — remembers it for this provider so
// coming back to it later restores the choice (M14 §3.7).
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
	// Persistence is best-effort AFTER the switch: a failure to write state.json
	// must not fail a switch that already worked, and must never send the
	// operator back to the old model.
	if l.modelMemory != nil {
		_ = l.modelMemory.SetModel(l.CurrentProvider(), model)
	}
	l.emit(Event{Phase: PhaseProviderSwitch, Tool: "model", Detail: "model: " + model})
	return nil
}

// SwitchProvider changes the session endpoint to one of the built-in named
// providers (openrouter|nvidia|litellm), and restores the model this session
// used for that provider last time (M14 §3.7).
//
// The restore is deliberately conservative, and the two refusals are the whole
// point of this function:
//
//   - provider never seen → NO model is chosen. We tell the operator to run
//     /models instead of guessing.
//   - provider refuses the remembered model → we keep the current model and
//     report it. Auto-picking a different one would be the harness deciding
//     alone which model to talk to the provider with, which is the operator's
//     call (same principle as the Approver veto).
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
	id := strings.ToLower(strings.TrimSpace(name))
	l.curProvider = id
	l.emit(Event{Phase: PhaseProviderSwitch, Tool: "provider", Detail: "provider: " + id})

	l.restoreRememberedModel(id)
	return nil
}

// restoreRememberedModel reinstalls the model remembered for a provider, if any.
// Every failure here is reported and swallowed: the provider switch itself
// already succeeded, and there is nothing the operator could do about a stale
// state file other than be told.
func (l *Loop) restoreRememberedModel(providerID string) {
	if l.modelMemory == nil {
		return
	}
	saved, ok, err := l.modelMemory.GetModel(providerID)
	if err != nil {
		l.emit(Event{
			Phase: PhaseProviderSwitch, Tool: "provider",
			Detail: "provider " + providerID + ": estado ilegivel (" + err.Error() + "); modelo mantido",
		})
		return
	}
	if !ok || strings.TrimSpace(saved) == "" {
		l.emit(Event{
			Phase: PhaseProviderSwitch, Tool: "provider",
			Detail: "provider " + providerID + ": sem modelo lembrado; use /models",
		})
		return
	}
	pc, ok := l.p.(providerModelController)
	if !ok {
		return
	}
	l.modelRestored = false
	if err := pc.SetModel(saved); err != nil {
		// The provider said no to the remembered model. We do NOT pick another.
		l.emit(Event{
			Phase: PhaseProviderSwitch, Tool: "provider",
			Detail: "provider " + providerID + ": modelo lembrado recusado (" +
				err.Error() + "); modelo mantido — escolha com /models",
		})
		return
	}
	l.modelRestored = true
	l.emit(Event{
		Phase: PhaseProviderSwitch, Tool: "model",
		Detail: "provider " + providerID + ": modelo lembrado restaurado: " + saved,
	})
}

// ProviderNames returns the switchable built-in provider names.
func (l *Loop) ProviderNames() []string {
	return l.cfg.ProviderNames()
}

// CurrentProvider returns the provider ACTIVE right now. It tracks /provider
// switches; reading cfg.Provider here made /connect target the wrong provider
// after the first switch (M14).
func (l *Loop) CurrentProvider() string {
	if l.curProvider != "" {
		return l.curProvider
	}
	return strings.ToLower(strings.TrimSpace(l.cfg.Provider))
}

// CurrentAPIKeyEnv returns the environment variable name that api_key_env
// resolves to for the active provider. The name is not a secret.
//
// It must follow the active provider, not the YAML: after /provider nvidia the
// env name is NVIDIA_API_KEY, and reporting the YAML's would send the operator
// to export the wrong variable.
func (l *Loop) CurrentAPIKeyEnv() string {
	if _, env, ok := l.cfg.ProviderDefault(l.CurrentProvider()); ok && env != "" {
		return env
	}
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
