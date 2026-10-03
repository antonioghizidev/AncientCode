// Wiring between the model picker panel and the Bubble Tea model (M12).
package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/antonioghizidev/ancientcode/internal/catalog"
	"github.com/antonioghizidev/ancientcode/internal/loop"
)

// loopModel is the core the picker drives (the same *loop.Loop the REPL uses).
type loopModel = loop.Loop

// closePicker closes the panel and gives the activity panel its viewport back
// from the top (a stale offset would show the log scrolled).
func (m *tuiModel) closePicker() {
	m.picker = nil
	m.activityOff = 0
}

// handlePickerKey owns the keyboard while the picker is open.
//
// Mapping (M12): printable characters and Backspace narrow the list, ↑↓ move
// the highlight, Enter selects, Esc / Ctrl+C cancel, Ctrl+F toggles the
// free-only filter (and says so when the provider has no pricing).
func (m *tuiModel) handlePickerKey(k tea.KeyMsg) tea.Cmd {
	p := m.picker
	switch k.Type {
	case tea.KeyEsc:
		m.closePicker()
		return nil
	case tea.KeyCtrlC:
		m.closePicker()
		m.banner = "selecao de modelo cancelada"
		return nil
	case tea.KeyEnter:
		entry, ok := p.selected()
		m.closePicker()
		if !ok {
			m.banner = "nenhum modelo selecionado; nada foi trocado"
			return nil
		}
		if m.agent == nil {
			m.banner = "erro: agente nao configurado; nenhum modelo trocado"
			return nil
		}
		// The switch MUST run outside the Bubble Tea event loop: SwitchModel
		// emits an event, the observer calls p.Send, and p.Send blocks when the
		// message channel is full — which can never drain from inside the loop
		// that would drain it. Doing it synchronously here deadlocks the UI
		// (found by the Inspiron smoke, M12).
		return switchModelCmd(m.agent, entry.Info.ID)
	case tea.KeyUp:
		p.move(-1)
		return nil
	case tea.KeyDown:
		p.move(+1)
		return nil
	case tea.KeyPgUp:
		p.move(-pickerRows)
		return nil
	case tea.KeyPgDown:
		p.move(+pickerRows)
		return nil
	case tea.KeyHome:
		p.cur = 0
		p.move(0)
		return nil
	case tea.KeyEnd:
		p.cur = len(p.vis) - 1
		if p.cur < 0 {
			p.cur = 0
		}
		p.move(0)
		return nil
	case tea.KeyCtrlF:
		if !p.freeAvailable {
			m.banner = "este provedor nao informa preco: filtro de gratuitos indisponivel"
			return nil
		}
		p.free = !p.free
		p.cur = 0
		p.off = 0
		p.visible()
		return nil
	case tea.KeyBackspace:
		p.backspace()
		return nil
	case tea.KeySpace:
		p.appendRune(' ')
		return nil
	case tea.KeyRunes:
		for _, r := range k.Runes {
			if r >= ' ' {
				p.appendRune(r)
			}
		}
		return nil
	}
	return nil
}

// switchModelCmd performs the model switch asynchronously and reports the
// outcome as a message.
//
// Why async (M12, found by the Inspiron smoke): Loop.SwitchModel emits an
// event, the observer forwards it with tea.Program.Send, and Send *blocks* when
// the message channel is full. Called from inside Update — i.e. inside the very
// loop that would drain that channel — it deadlocks the whole UI. Returning a
// tea.Cmd runs the switch on the Bubble Tea runtime, where draining continues.
func switchModelCmd(agent *loopModel, id string) tea.Cmd {
	return func() tea.Msg {
		if err := agent.SwitchModel(id); err != nil {
			return modelSwitchedMsg{id: id, err: err}
		}
		return modelSwitchedMsg{id: id}
	}
}

// modelSwitchedMsg reports the outcome of a model switch back to Update.
type modelSwitchedMsg struct {
	id  string
	err error
}

// applyModelSwitch renders the result of an asynchronous switch.
func (m *tuiModel) applyModelSwitch(msg modelSwitchedMsg) {
	if msg.err != nil {
		m.banner = "erro: " + msg.err.Error()
		return
	}
	m.model = msg.id
	m.banner = ""
	m.appendSession("model: " + redactModelLine(m.model) + " (vale nesta sessao — nao persiste no yaml)")
}

// switchProviderCmd switches provider asynchronously, for the same reason as
// switchModelCmd (SwitchProvider also emits an event).
func switchProviderCmd(agent *loopModel, name string) tea.Cmd {
	return func() tea.Msg {
		if err := agent.SwitchProvider(name); err != nil {
			return providerSwitchedMsg{name: name, err: err}
		}
		return providerSwitchedMsg{name: name}
	}
}

// providerSwitchedMsg reports the outcome of a provider switch.
type providerSwitchedMsg struct {
	name string
	err  error
}

// applyProviderSwitch renders the result of an asynchronous provider switch.
func (m *tuiModel) applyProviderSwitch(msg providerSwitchedMsg) {
	if msg.err != nil {
		m.banner = "erro: " + msg.err.Error()
		return
	}
	if m.agent != nil {
		m.provider = m.agent.CurrentProviderBaseURL()
	}
	m.banner = ""
	m.appendSession("provider: " + strings.ToLower(strings.TrimSpace(msg.name)) + " ativo (vale nesta sessao)")
}

// openPicker starts the catalog fetch and shows the panel.
func (m *tuiModel) openPicker(opts catalogOpts) tea.Cmd {
	if m.agent == nil {
		m.banner = "erro: agente nao configurado; catalogo indisponivel"
		return nil
	}
	m.picker = &picker{opts: opts, active: m.agent.CurrentModel(), loading: true}
	m.appendSession("  abrindo catalogo de modelos do provedor…")
	return openPickerCmd(m)
}

// pickerFetched handles the async catalog result: a failure must leave the model
// untouched and say why (fail-closed), never show a partial/empty list as if it
// were the truth.
func (m *tuiModel) pickerFetched(msg pickerMsg) tea.Cmd {
	if msg.err != nil {
		m.closePicker()
		m.banner = "erro: " + msg.err.Error()
		return nil
	}
	p := m.picker
	if p == nil {
		return nil // closed while fetching
	}
	p.all = msg.entries
	p.loading = false
	p.freeAvailable = catalog.FreeFilterAvailable(msg.entries)
	p.visible()
	c := catalog.Summarize(p.all, p.vis)
	m.appendSession(
		"  catalogo: " + itoa(c.Total) + " modelos no provedor, " +
			itoa(c.Visible) + " uteis para este agente")
	return nil
}

// redactModelLine is the second-pass guard used when echoing provider strings
// into the session panel (the provider layer already redacted them).
func redactModelLine(s string) string { return sanitizeLine(s) }

// trimQueryPrefix is used by tests and by the picker to normalize a query typed
// with stray separators.
func trimQueryPrefix(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
