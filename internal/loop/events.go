package loop

import (
	"time"

	"ancientai/internal/redact"
)

// Phase identifies one observable stage of an agent turn (M8, SPEC §3 contract
// shared by the REPL, the M9 TUI and any future transport — web UI). The enum
// is stable: new phases must be APPENDED, never renumbered, so persisted and
// JSON round-tripped events stay unambiguous.
type Phase int

const (
	PhaseModelWaiting Phase = iota
	PhaseModelStreaming
	PhaseGateWaiting
	PhaseToolRunning
	PhaseTurnOK
	PhaseTurnError
	PhaseProviderSwitch
)

// phaseLabel maps a phase to the human-readable status text the TTY renderer
// shows (D-2026-09-16-35 vocabulary).
func (p Phase) Label() string {
	switch p {
	case PhaseModelWaiting:
		return "modelo: aguardando o primeiro token..."
	case PhaseModelStreaming:
		return "modelo: recebendo tokens..."
	case PhaseGateWaiting:
		return "gate humano: aguardando sua aprovacao..."
	case PhaseToolRunning:
		return "tool: executando..."
	case PhaseTurnOK:
		return ""
	case PhaseProviderSwitch:
		return "provider trocado em tempo de execucao"
	case PhaseTurnError:
		return ""
	default:
		return ""
	}
}

// Event is the structured, JSON-serializable observer payload. Detail must be
// redacted by the loop BEFORE emission (redact.Redact); Dur is set for
// ToolRunning (execution elapsed) and At is the emission timestamp.
type Event struct {
	Phase  Phase         `json:"phase"`
	Tool   string        `json:"tool,omitempty"`
	Detail string        `json:"detail,omitempty"`
	Dur    time.Duration `json:"dur,omitempty"`
	At     time.Time     `json:"at"`
}

// Usage is the context-meter readout (M8, D-37): how many bytes of history the
// provider will actually receive this turn vs the configured budget.
type Usage struct {
	SentBytes int `json:"sent_bytes"`
	Budget    int `json:"budget,omitempty"` // 0 = trimming off (full history sent)
	Pct       int `json:"pct"`              // -1 = off
}

// emit delivers an event to the observer (nil-safe, fail-closed default). The
// Detail is re-redacted as defense in depth before touching a consumer.
func (l *Loop) emit(ev Event) {
	if l.obs == nil {
		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	ev.Detail = redact.Redact(ev.Detail)
	l.obs(ev)
}
