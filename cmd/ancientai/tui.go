// M9 TUI (specs/milestones/09-m9-tui.md): a Bubble Tea frontend that consumes
// the exact contract M8 stabilized — the pure command parser (commands.go), the
// loop.Approver gate and the structured Event stream (loop.SetObserver). The
// render boundary is here (View); every projection lives in tui_panels.go and
// is stdlib-pure so it is unit-tested without a terminal.
//
// Security invariants (SPEC M9 §4):
//   - The gate (tuiGate) implements loop.Approver and is the ONLY approval
//     path: no tool executes unless the operator answered y/n/d in the dialog.
//   - Everything painted re-redacts defensively via internal/redact before
//     leaving the model (a raw secret that somehow reached a session line can
//     never appear on screen).
//   - The model never calls agent.Turn/Continue on a keypress: it returns a
//     tea.Cmd which the Bubble Tea runtime runs, and results come back as
//     messages, so the input loop stays reactive and Ctrl+C works mid-turn.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"ancientai/internal/config"
	"ancientai/internal/loop"
	"ancientai/internal/redact"
)

// turnRunner is the tiny port the TUI drives turns through. Internal/loop is
// injected behind it (dependency inversion, same as every other frontend); a
// fakeRunner in tests replaces it for deterministic, network-free coverage.
type turnRunner interface {
	run(ctx context.Context, user string) (string, error)
	cont(ctx context.Context) (string, error)
}

// loopRunner adapts *loop.Loop to turnRunner.
type loopRunner struct {
	agent *loop.Loop
}

func (r loopRunner) run(ctx context.Context, user string) (string, error) {
	return r.agent.Turn(ctx, user)
}

func (r loopRunner) cont(ctx context.Context) (string, error) {
	return r.agent.Continue(ctx, "")
}

const (
	maxSessionLines  = 200 // session panel scrollback bound
	maxActivityLines = 30  // activity panel scrollback bound
	maxCancelled     = 20  // M13: how many cancelled turns /continue can resume

	// M13 animation + navigation.
	spinInterval = 100 * time.Millisecond // spinner tick while a turn runs
	hscrollStep  = 8                      // cells per Shift+Left/Right
)

// turnResultMsg carries one finished turn back to Update (from the tea.Cmd).
type turnResultMsg struct {
	out   string
	err   error
	retry bool
}

// spinTickMsg advances the activity spinner. M13: without a temporal signal the
// operator cannot tell a working model from a frozen one.
type spinTickMsg struct{}

// spinTick schedules the next spinner frame. It is ONLY scheduled while a turn
// is in flight, so an idle TUI does no repaint work at all.
func spinTick() tea.Cmd {
	return tea.Tick(spinInterval, func(time.Time) tea.Msg { return spinTickMsg{} })
}

// gateOpenMsg tells the model a gate dialog must be shown (bridged from
// tuiGate.opened by the wiring in runTUI).
type gateOpenMsg struct {
	dialog *gateOpen
}

// gateClosedMsg clears the dialog (bridged from tuiGate.closed).
type gateClosedMsg struct{}

// agentEventMsg carries a redacted loop event onto the activity panel.
type agentEventMsg struct {
	ev loop.Event
}

// tuiModel is the TUI state machine. It is a pure message handler: every
// side effect (turn, gate answer, model switch) is a returned tea.Cmd or a
// response sent through the injected tuiGate.
type tuiModel struct {
	agent   *loop.Loop
	gate    *tuiGate
	usageFn func() loop.Usage // nil in tests; real wiring binds agent.ContextUsage

	runner      turnRunner
	version     string
	provider    string
	model       string
	toolbox     string
	sessionName string
	width       int
	height      int

	input      string
	cursor     int
	promptHist []string
	histIdx    int // -1 = editing a fresh line
	draft      string

	session  []string // session panel lines (redacted before paint)
	activity []string // activity panel lines (from the observer)
	banner   string   // one error/info line
	usage    loop.Usage

	// M13: physical (wrapped) projections. The logical slices above keep the
	// text WHOLE; these are what the viewport scrolls over, so a long answer is
	// readable row by row instead of jumping over in one keystroke.
	sessionView  []string
	activityView []string

	focus       panelID         // viewport navigation target (M10)
	sessionOff  int             // viewport top offset (PHYSICAL rows) of the session panel
	activityOff int             // viewport top offset (PHYSICAL rows) of the activity panel
	hoff        map[panelID]int // M13: horizontal scroll per panel (defensive)
	picker      *picker         // M12: open model-selection panel (nil = closed)

	inTurn     bool
	frame      int    // M13: spinner tick counter (advances only while inTurn)
	asciiSpin  bool   // M13: no Braille in this terminal (ASCII degradation)
	turnCancel func() // canceled on Ctrl+C or Esc mid-turn
	dialog     *gateOpen

	// M13: the prompt of the turn in flight, so cancelling can hand it back to
	// the input instead of making the operator retype it.
	flyingPrompt string
	turnSeq      int      // identifies the turn in flight
	cancelledSeq int      // turnSeq of the turn the operator interrupted
	turnTools    []string // tools that already ran in the turn in flight (M13)
	cancelled    []cancelInfo
	lastCancel   string
}

// cancelInfo records a cancelled turn: the prompt to re-send and whether any
// tool already ran. Re-sending after a tool executed REPEATS its effect, so the
// operator is told which tools ran instead of being silently exposed to it
// (spec M13 §4.5).
type cancelInfo struct {
	prompt string
	tools  []string
}

func (m *tuiModel) resetPrompt() {
	m.input = ""
	m.cursor = 0
	m.histIdx = -1
	m.draft = ""
}

func (m *tuiModel) refreshUsage() {
	if m.usageFn != nil {
		m.usage = m.usageFn()
	}
}

func (m *tuiModel) statusTone() statusTone {
	switch {
	case m.banner != "":
		return statusErr
	case m.inTurn || m.dialog != nil:
		return statusWait
	default:
		return statusOK
	}
}

// Init satisfies tea.Model.
func (m *tuiModel) Init() tea.Cmd { return nil }

// Update satisfies tea.Model: it maps messages to model state and returns the
// side effects (tea.Cmd) the runtime should execute.
func (m *tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case gateOpenMsg:
		m.dialog = msg.dialog
		return m, nil
	case gateClosedMsg:
		m.dialog = nil
		return m, nil
	case agentEventMsg:
		if line := activityFromEvent(msg.ev); line != "" {
			m.appendActivity(line)
		}
		// M13: remember which tools ran in the turn in flight, so a cancel can
		// tell the operator what re-sending would repeat.
		if msg.ev.Phase == loop.PhaseToolRunning && msg.ev.Tool != "" {
			m.turnTools = append(m.turnTools, msg.ev.Tool)
		}
		m.refreshUsage()
		return m, nil
	case spinTickMsg:
		// M13: the tick only exists while a turn is running; the tick that ends
		// the turn is never scheduled, so nothing ticks while idle.
		if m.inTurn {
			m.frame++
			return m, spinTick()
		}
		return m, nil
	case pickerMsg:
		return m, m.pickerFetched(msg)
	case modelSwitchedMsg:
		m.applyModelSwitch(msg)
		return m, nil
	case providerSwitchedMsg:
		m.applyProviderSwitch(msg)
		return m, nil
	case turnResultMsg:
		m.inTurn = false
		m.turnCancel = nil
		// M13: a turn the operator interrupted comes back as an error
		// (context canceled) — that is the EXPECTED outcome, not a failure.
		// Painting it in red would tell the operator their deliberate action
		// broke something.
		wasCancelled := m.cancelledSeq == m.turnSeq && m.turnSeq != 0
		m.turnSeq = 0
		if wasCancelled {
			if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
				m.appendSession("! erro: " + msg.err.Error())
			}
			return m, nil
		}
		if msg.err != nil {
			// M11: an error that only flips the footer to "erro" is unreadable.
			// It gets the banner AND a history line, so it survives scrolling.
			m.banner = "erro: " + msg.err.Error()
			// M13: no truncation here. The error is kept whole and wrapped by the
			// projection — the tail of an error IS the useful part.
			m.appendSession("! erro: " + msg.err.Error())
		} else {
			m.banner = ""
			m.appendSession("< " + redact.Redact(msg.out))
			m.refreshUsage()
		}
		return m, nil
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}

// handleKey routes one key event. When a gate dialog is open only y/n/d (or
// Ctrl+C = n) are meaningful; when the model picker is open it owns the
// keyboard; everything else is routed to editing/navigation as the M10 keymap
// requires.
func (m *tuiModel) handleKey(k tea.KeyMsg) tea.Cmd {
	if m.dialog != nil {
		return m.handleGateKey(k)
	}
	if m.picker != nil {
		return m.handlePickerKey(k)
	}
	switch k.Type {
	case tea.KeyEnter:
		return m.submit()
	case tea.KeyEsc:
		// M13: Esc interrupts the turn in flight. It does NOT quit — Ctrl+C is
		// the quit gesture, and turning Esc into "exit" would be a regression of
		// a key the operator already uses to back out of the picker.
		if m.inTurn {
			m.cancelTurn("Esc")
			return nil
		}
		return nil
	case tea.KeyCtrlC:
		if m.inTurn {
			m.cancelTurn("Ctrl+C")
			return nil
		}
		return tea.Quit
	case tea.KeyShiftLeft:
		// M13: Shift+Left/Right scroll the focused panel horizontally. Wrapping
		// handles long prose, but a path or a one-line JSON has no break
		// opportunity — without this its tail is unreachable.
		m.hscroll(-1)
		return nil
	case tea.KeyShiftRight:
		m.hscroll(+1)
		return nil
	case tea.KeyTab:
		m.cycleFocus()
		return nil
	case tea.KeyShiftTab: // Shift+Tab arrives as its own key type
		m.cycleFocus()
		return nil
	case tea.KeyDown:
		if m.navigatePanel(+1) {
			return nil
		}
		m.histStep(+1)
	case tea.KeyUp:
		if m.navigatePanel(-1) {
			return nil
		}
		m.histStep(-1)
	case tea.KeyPgDown:
		m.scrollPanel(m.panelContentRows())
		return nil
	case tea.KeyPgUp:
		m.scrollPanel(-m.panelContentRows())
		return nil
	case tea.KeyHome:
		if m.focus != panelPrompt {
			m.panelToTop()
			return nil
		}
		m.cursor = 0
	case tea.KeyEnd:
		if m.focus != panelPrompt {
			m.panelToBottom()
			return nil
		}
		m.cursor = len([]rune(m.input))
	case tea.KeyLeft:
		if m.focus != panelPrompt {
			m.cycleFocus()
			return nil
		}
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyRight:
		if m.focus != panelPrompt {
			m.cycleFocus()
			return nil
		}
		if m.cursor < len([]rune(m.input)) {
			m.cursor++
		}
	case tea.KeyBackspace:
		m.backspace()
	case tea.KeyDelete:
		m.deleteAt()
	case tea.KeySpace:
		m.insertRunes([]rune{' '})
	case tea.KeyRunes:
		return m.handleRunes(k.Runes)
	}
	return nil
}

// followBottom snaps a viewport to the newest content when the operator was
// already at the bottom — chat behaviour: a new line shows up immediately.
// `prevTotal` is the content length BEFORE the append; comparing against the
// post-append length would never detect "was at the bottom" (the append moves
// the bottom away by one). A viewport the operator scrolled up is left alone.
//
// `off`/`total` are passed by pointer-style through the returned value.
func (m *tuiModel) followBottom(off *int, total, prevTotal int) {
	vis := m.panelContentRows()
	if total <= vis {
		*off = 0
		return
	}
	wasAtBottom := prevTotal <= vis || *off >= prevTotal-vis
	if wasAtBottom {
		*off = total - vis
	}
}

// sessBodyWidth / actBodyWidth are the usable cell counts inside each panel
// frame: the inner width minus the scrollbar column. They are the wrap width
// for panelLines.
func (m *tuiModel) sessBodyWidth() int {
	w := m.width
	if w <= 0 {
		w = 120
	}
	sessInner, _ := panelInnerWidths(w)
	if n := sessInner - 1; n > 1 {
		return n
	}
	return 1
}

func (m *tuiModel) actBodyWidth() int {
	w := m.width
	if w <= 0 {
		w = 120
	}
	_, actInner := panelInnerWidths(w)
	if n := actInner - 1; n > 1 {
		return n
	}
	return 1
}

// cancelTurn interrupts the turn in flight (M13).
//
// It hands the prompt back to the input: before M13 the text was dropped on
// submit, so the operator had to retype the whole thing to fix a route. It also
// records which tools ALREADY ran, because re-sending the prompt repeats their
// effects (`write_file`, `docker rm`) — the operator is told, not exposed to it.
func (m *tuiModel) cancelTurn(by string) {
	if m.turnCancel != nil {
		m.turnCancel()
	}
	tools := append([]string(nil), m.turnTools...)
	prompt := m.flyingPrompt
	wasCancelled := m.turnSeq
	m.inTurn = false
	m.turnCancel = nil
	m.flyingPrompt = ""
	m.turnTools = nil
	// Remember WHICH turn was interrupted: its result message arrives later and
	// must not be reported as a failure.
	m.cancelledSeq = wasCancelled

	line := "! turno cancelado (" + by + ") — edite e reenvie"
	if len(tools) > 0 {
		line += " | tools que ja executaram: " + strings.Join(tools, ", ")
	}
	m.appendSession(line)
	m.banner = "turno cancelado (" + by + ")"
	if prompt != "" {
		m.cancelled = append(m.cancelled, cancelInfo{prompt: prompt, tools: tools})
		if len(m.cancelled) > maxCancelled {
			m.cancelled = m.cancelled[len(m.cancelled)-maxCancelled:]
		}
		// Hand the prompt back for editing, caret at the end.
		m.input = prompt
		m.cursor = len([]rune(prompt))
		m.histIdx = -1
	}
	m.syncViews()
}

// reSendCancelled is what /continue does: re-send the last cancelled prompt.
// Fail-closed with nothing cancelled, and it consumes the entry so a second
// /continue cannot repeat a turn (and its tool effects) twice.
func (m *tuiModel) reSendCancelled() tea.Cmd {
	if len(m.cancelled) == 0 {
		m.banner = "erro: nada a continuar (nenhum turno foi cancelado)"
		return nil
	}
	c := m.cancelled[len(m.cancelled)-1]
	m.cancelled = m.cancelled[:len(m.cancelled)-1]
	m.resetPrompt()
	if len(c.tools) > 0 {
		// Option chosen by the operator: warn, do not refuse. Re-sending is
		// legitimate; doing it blind is not.
		m.banner = "atencao: reenviando; ja executaram: " + strings.Join(c.tools, ", ")
	} else {
		m.banner = ""
	}
	m.input = c.prompt
	return m.submit()
}

// hscroll moves the focused panel horizontally, clamped by hscrollRange.
func (m *tuiModel) hscroll(dir int) {
	p := m.focus
	if p != panelSession && p != panelActivity {
		return // the prompt owns the arrow keys: no horizontal scroll there
	}
	if m.hoff == nil {
		m.hoff = map[panelID]int{}
	}
	step := dir * hscrollStep
	m.hoff[p] += step
	if m.hoff[p] < 0 {
		m.hoff[p] = 0
	}
}

// spinnerFrame returns the current spinner glyph, or 0 when no turn is running.
func (m *tuiModel) spinnerFrame() rune {
	if !m.inTurn {
		return 0
	}
	return spinnerFrame(m.frame, m.asciiSpin)
}

// spinnerLine returns the spinner prefix for the activity panel status line, or
// "" when idle. Empty when idle is deliberate: a stopped spinner would confirm
// the freeze the operator is trying to rule out.
func (m *tuiModel) spinnerLine() string {
	f := m.spinnerFrame()
	if f == 0 {
		return ""
	}
	return string(f) + " "
}

// syncViews recomputes the PHYSICAL projections of both panels (M13).
//
// The logical lines are kept intact in m.session / m.activity — that is the
// point: `truncatedLine` used to cut them at 90 runes ON APPEND, so the text was
// gone before any projection ran and no fix in the paint could recover it.
//
// The viewport offsets count physical (wrapped) rows on purpose. With logical
// offsets, a 40-row answer counts as ONE row: pressing ↓ would jump over it and
// the operator could never read the middle — the very complaint the M13
// milestone was opened for.
func (m *tuiModel) syncViews() {
	m.sessionView = panelLines(m.session, m.sessBodyWidth())
	m.activityView = panelLines(m.activity, m.actBodyWidth())
}

// appendSession appends one logical line to the session and keeps the viewport
// pinned to the bottom. The line is stored WHOLE (no cap): truncation is the
// projection's job now.
func (m *tuiModel) appendSession(line string) {
	m.syncViews()
	prev := len(m.sessionView)
	m.session = append(m.session, line)
	m.session = capLines(m.session, maxSessionLines)
	m.syncViews()
	m.followBottom(&m.sessionOff, len(m.sessionView), prev)
}

// appendActivity appends one logical line to the activity log, same contract.
func (m *tuiModel) appendActivity(line string) {
	m.syncViews()
	prev := len(m.activityView)
	m.activity = append(m.activity, line)
	m.activity = capLines(m.activity, maxActivityLines)
	m.syncViews()
	m.followBottom(&m.activityOff, len(m.activityView), prev)
}

// followSessionBottom keeps the session viewport pinned to the newest line.
func (m *tuiModel) followSessionBottom(prevTotal int) {
	m.syncViews()
	m.followBottom(&m.sessionOff, len(m.sessionView), prevTotal)
}

// followActivityBottom keeps the activity viewport pinned to the newest event.
func (m *tuiModel) followActivityBottom(prevTotal int) {
	m.syncViews()
	m.followBottom(&m.activityOff, len(m.activityView), prevTotal)
}

// cycleFocus moves the viewport navigation focus between the two panels (M10).
// The prompt is never part of the cycle: typing stays on the prompt, only the
// scroll target moves.
func (m *tuiModel) cycleFocus() {
	switch m.focus {
	case panelSession:
		m.focus = panelActivity
	case panelActivity:
		m.focus = panelSession
	default:
		m.focus = panelSession // first Tab from the prompt focuses the session
	}
}

// navigatePanel scrolls the focused panel one row in `delta` direction
// (+1 down, -1 up); reports true when the key was consumed by navigation
// (never by the prompt editor).
func (m *tuiModel) navigatePanel(delta int) bool {
	m.syncViews()
	switch m.focus {
	case panelSession:
		m.sessionOff = viewportFor(m.sessionOff, len(m.sessionView), m.panelContentRows(), delta)
		return true
	case panelActivity:
		m.activityOff = viewportFor(m.activityOff, len(m.activityView), m.panelContentRows(), delta)
		return true
	}
	return false
}

// scrollPanel scrolls the focused panel by `delta` rows (page keys). The
// prompt never consumes these keys.
func (m *tuiModel) scrollPanel(delta int) {
	m.navigatePanel(delta)
}

// panelToTop / panelToBottom implement Home/End on the focused panel.
func (m *tuiModel) panelToTop()    { m.navigatePanel(-(1 << 30)) }
func (m *tuiModel) panelToBottom() { m.navigatePanel(+(1 << 30)) }

// gateDetailMaxRows bounds how tall the approval dialog may grow. M13: the
// detail used to be capped at TWO lines, which silently truncated the payload of
// any long command — approving what you cannot read is not informed consent
// (M11). The cap now exists only to keep the dialog on screen.
const gateDetailMaxRows = 8

// gateRowsFor returns how many detail rows the gate can spend: bounded by
// gateDetailMaxRows AND by the height left over once the prompt, footer and a
// usable panel strip are reserved. The dialog never crowds the input line out.
func (m *tuiModel) gateRowsFor() int {
	h := m.height
	if h <= 0 {
		h = 30
	}
	// fixedRows with no gate: divider + prompt + footer, plus the banner.
	reserved := fixedRows(0, m.banner != "")
	spare := h - reserved - minPanelRowsOnGate // minPanelRowsOnGate = 5
	if spare < 1 {
		return 1
	}
	if spare > gateDetailMaxRows {
		return gateDetailMaxRows
	}
	return spare
}

// minPanelRowsOnGate is how many panel rows must survive when the gate is open:
// enough to see the turn state and the pending event while deciding.
const minPanelRowsOnGate = 5

// gateLines renders the pending approval so the operator can read WHAT will run
// before answering (M11). Approving a tool whose payload is hidden is not
// informed consent — the command is the whole point of the gate.
func (m *tuiModel) gateLines() []string {
	if m.dialog == nil {
		return nil
	}
	width := m.width
	if width <= 0 {
		width = 120
	}
	head := "[aprovar] " + m.dialog.Tool + " | y aprova, n nega, d nega sempre"
	lines := []string{gateOn + head + gateOff}
	for _, d := range gateDetail(m.dialog.Summary, width-2, m.gateRowsFor()) {
		lines = append(lines, "  "+d)
	}
	return lines
}

// panelContentRows mirrors the row budget buildView gives the panels, so the
// viewport navigation and the projection agree on how many rows are visible.
func (m *tuiModel) panelContentRows() int {
	h := m.height
	if h <= 0 {
		h = 30
	}
	rows := h - fixedRows(len(m.gateLines()), m.banner != "")
	if rows < 3 {
		rows = 3
	}
	return rows - 2 // top + bottom border
}

// handleGateKey resolves the open dialog. Only y/n/d/Ctrl+C do anything; d
// registers a permanent veto for the tool (operator last word, D-37).
func (m *tuiModel) handleGateKey(k tea.KeyMsg) tea.Cmd {
	if k.Type == tea.KeyCtrlC {
		m.dialog.Respond(loop.DecisionDeny)
		m.dialog = nil
		return nil
	}
	if k.Type != tea.KeyRunes || len(k.Runes) != 1 {
		return nil
	}
	switch k.Runes[0] {
	case 'y', 'Y':
		m.dialog.Respond(loop.DecisionApprove)
	case 'n', 'N':
		m.dialog.Respond(loop.DecisionDeny)
	case 'd', 'D':
		m.dialog.Respond(loop.DecisionDeny)
		m.gate.registerVeto(m.dialog.Tool)
	default:
		return nil // any other key keeps the dialog open
	}
	m.dialog = nil
	return nil
}

// handleRunes processes printable key runs, with the special ^R/retry and
// ^C-ish clear affordances the spec maps (a banner error makes 'r' retry and
// 'c' clear). Typing always returns the viewport focus to the prompt (the
// scroll target and the keyboard are decoupled, M10).
// handleRunes inserts typed text, with two single-letter shortcuts that only
// apply when the input is EMPTY.
//
// M13: they used to fire whenever a banner was visible, which silently ate the
// operator's typing. "verifique" became "veifique" because the 'r' started a
// retry. The banner is exactly what is on screen after an error or a cancel —
// so the bug hit the most common recovery path: fix the prompt and resend.
// A shortcut that eats letters is worse than no shortcut.
func (m *tuiModel) handleRunes(rs []rune) tea.Cmd {
	if len(rs) == 1 && m.input == "" {
		switch rs[0] {
		case 'r', 'R':
			if m.banner != "" {
				m.banner = ""
				m.inTurn = true
				m.frame = 0
				m.turnSeq++
				m.flyingPrompt = ""
				ctx, cancel := context.WithCancel(context.Background())
				m.turnCancel = cancel
				m.turnTools = nil
				cont := m.runner.cont
				return tea.Batch(func() tea.Msg {
					out, err := cont(ctx)
					return turnResultMsg{out: out, err: err, retry: true}
				}, spinTick())
			}
		case 'c', 'C':
			if m.banner != "" {
				m.banner = ""
				return nil
			}
		}
	}
	m.focus = panelPrompt
	m.insertRunes(rs)
	return nil
}

func (m *tuiModel) insertRunes(rs []rune) {
	for _, r := range rs {
		if r < 0x20 {
			continue
		}
		buf := []rune(m.input)
		if m.cursor < 0 {
			m.cursor = 0
		}
		if m.cursor > len(buf) {
			m.cursor = len(buf)
		}
		buf = append(buf, 0)
		copy(buf[m.cursor+1:], buf[m.cursor:])
		buf[m.cursor] = r
		m.input = string(buf)
		m.cursor++
	}
}

func (m *tuiModel) backspace() {
	if m.cursor <= 0 {
		return
	}
	buf := []rune(m.input)
	buf = append(buf[:m.cursor-1], buf[m.cursor:]...)
	m.input = string(buf)
	m.cursor--
}

func (m *tuiModel) deleteAt() {
	buf := []rune(m.input)
	if m.cursor >= len(buf) {
		return
	}
	buf = append(buf[:m.cursor], buf[m.cursor+1:]...)
	m.input = string(buf)
}

// histStep mirrors editor.historyStep (input.go): ↓ past the newest restores
// the fresh draft, ↑ climbs from the newest history entry.
func (m *tuiModel) histStep(delta int) {
	if len(m.promptHist) == 0 {
		return
	}
	h := len(m.promptHist)
	if m.histIdx < -1 || m.histIdx >= h {
		m.histIdx = -1
	}
	switch {
	case m.histIdx == -1 && delta > 0:
		return
	case m.histIdx == -1 && delta < 0:
		m.draft = m.input
		m.histIdx = h - 1
	case m.histIdx == 0 && delta < 0:
		// already at the oldest entry
	case m.histIdx == h-1 && delta > 0:
		m.histIdx = -1
		m.input = m.draft
		m.cursor = len([]rune(m.input))
		return
	default:
		m.histIdx += delta
	}
	if m.histIdx >= 0 && m.histIdx < h {
		m.input = m.promptHist[m.histIdx]
		m.cursor = len([]rune(m.input))
	}
}

// submit runs one turn (or a command). It never blocks this method: the turn
// is a tea.Cmd the runtime executes, and the result comes back as
// turnResultMsg.
func (m *tuiModel) submit() tea.Cmd {
	if m.inTurn || m.dialog != nil {
		return nil
	}
	if m.runner == nil {
		m.banner = "erro: runner nao configurado"
		return nil
	}
	line := strings.TrimSpace(m.input)
	if line == "" {
		return nil
	}
	if cmd, isCmd := parseCommand(m.input); isCmd {
		// The command consumed the line: clear the input. Without this the
		// command text stayed in the prompt and the next thing typed was
		// appended to it ("/modelsoi"), because commands returned before the
		// reset below — invisible in the REPL, obvious in the TUI.
		raw := m.input
		m.resetPrompt()
		return m.handleCommand(raw, cmd)
	}
	// M13: the prompt is stored WHOLE (no 90-rune cap). The panel wraps it.
	m.appendSession("> " + redact.Redact(strings.TrimSpace(line)))
	m.promptHist = append(m.promptHist, redact.Redact(line))
	prompt := line
	m.resetPrompt()
	m.inTurn = true
	m.frame = 0
	m.turnSeq++
	m.flyingPrompt = prompt
	ctx, cancel := context.WithCancel(context.Background())
	m.turnCancel = cancel
	m.turnTools = nil
	run := func() tea.Msg {
		out, err := m.runner.run(ctx, prompt)
		return turnResultMsg{out: out, err: err}
	}
	// M13: the spinner starts with the turn and dies with it.
	return tea.Batch(run, spinTick())
}

// handleCommand executes a slash-command using the SAME classifier the REPL
// uses (shared parser, one intent). Output goes onto the session banner/panel
// and the footer fields so prompt↔UI stay in parity.
func (m *tuiModel) handleCommand(raw string, cmd command) tea.Cmd {
	switch cmd.kind {
	case cmdExit:
		return tea.Quit
	case cmdHelp:
		m.appendSession(redact.Redact(helpText))
	case cmdReset:
		m.session = nil
		m.appendSession("session (painel) resetado")
	case cmdHistory:
		if m.agent != nil {
			for i, p := range m.agent.PromptList() {
				m.appendSession(secLine(i+1, p))
			}
		} else {
			m.appendSession("historico indisponivel")
		}
	case cmdModels:
		return m.openPicker(cmd.opts)
	case cmdContext:
		m.refreshUsage()
		if m.usage.Pct < 0 {
			m.appendSession("ctx: off (" + itoa(m.usage.SentBytes) + " bytes)")
		} else {
			m.appendSession("ctx: " + itoa(m.usage.Pct) + "% (" + itoa(m.usage.SentBytes) + "/" + itoa(m.usage.Budget) + " bytes)")
		}
	case cmdModel:
		if cmd.arg == "" {
			if m.agent != nil {
				m.model = m.agent.CurrentModel()
			}
			m.appendSession("model: " + m.model)
		} else if m.agent != nil {
			// Async for the same reason as the picker (M12): a synchronous
			// switch emits an event and deadlocks on tea.Program.Send.
			return switchModelCmd(m.agent, cmd.arg)
		}
	case cmdProvider:
		if cmd.arg == "" {
			if m.agent != nil {
				m.provider = m.agent.CurrentProviderBaseURL()
			}
			m.appendSession("provider base_url: " + m.provider)
		} else if m.agent != nil {
			return switchProviderCmd(m.agent, cmd.arg)
		}
	case cmdContinue:
		// M13: /continue re-sends the last CANCELLED turn. A plain /continue
		// after a completed turn is the existing "retry from history" behaviour,
		// so the cancelled case is only handled when there is one.
		if len(m.cancelled) > 0 {
			return m.reSendCancelled()
		}
		m.banner = ""
		m.inTurn = true
		m.frame = 0
		m.turnSeq++
		ctx, cancel := context.WithCancel(context.Background())
		m.turnCancel = cancel
		cont := m.runner.cont
		return tea.Batch(func() tea.Msg {
			out, err := cont(ctx)
			return turnResultMsg{out: out, err: err, retry: true}
		}, spinTick())
	case cmdRetry:
	case cmdResend:
		if cmd.index <= 0 || m.agent == nil {
			m.banner = "erro: sem prompt !" + itoa(cmd.index) + " (historico vazio)"
			return nil
		}
		p, ok := m.agent.PromptAt(cmd.index - 1)
		if !ok {
			m.banner = "erro: sem prompt !" + itoa(cmd.index)
			return nil
		}
		m.appendSession("> " + p)
		m.promptHist = append(m.promptHist, p)
		m.resetPrompt()
		m.inTurn = true
		m.frame = 0
		m.turnSeq++
		m.flyingPrompt = p
		ctx, cancel := context.WithCancel(context.Background())
		m.turnCancel = cancel
		m.turnTools = nil
		pline := p
		return func() tea.Msg {
			out, err := m.runner.run(ctx, pline)
			return turnResultMsg{out: out, err: err}
		}
	}
	m.syncViews()
	return nil
}

// secLine renders a numbered history/session helper line.
func secLine(i int, s string) string {
	return itoa(i) + ": " + s
}

// View renders the whole TUI screen against the model state (spec 09 layout:
// session panel, activity panel, prompt window, divider, gate, footer).
func (m *tuiModel) View() string {
	info := footerInfo{
		Version:  m.version,
		Provider: m.provider,
		Model:    m.model,
		Toolbox:  m.toolbox,
		Session:  m.sessionName,
		Status:   m.statusTone(),
		Usage:    m.usage,
	}
	sess := capLines(m.session, maxSessionLines)
	redacted := make([]string, len(sess))
	for i, l := range sess {
		// M11: redact (secrets) AND sanitize (control sequences) — both before
		// the line leaves the model.
		redacted[i] = sanitize(redact.Redact(l))
	}
	// M13: redact/sanitize happens BEFORE the wrap, so a remote escape sequence
	// can never shift the column arithmetic of the projection.
	act := capLines(m.activity, maxActivityLines)
	// M12: the picker replaces the activity panel while open — it is a distinct
	// task with its own keyboard, so mixing a choice list into the event log
	// would make both harder to read. The activity viewport must also reset:
	// a stale offset would open the picker scrolled past its first rows,
	// hiding the highlighted entry and the filter line.
	switch {
	case m.picker != nil:
		act = pickerLines(m.picker)
		m.activityOff = 0
	default:
		if spin := strings.TrimSpace(m.spinnerLine()); spin != "" {
			// M13: the animated status line. Humans read motion as "work in
			// progress"; a static panel cannot distinguish a running model from a
			// frozen one. It is a LOCAL display list — writing it back to
			// m.activity would prepend one line per rendered frame (~10/s) and
			// push the real event log out of the panel.
			act = append([]string{spin + " processando… (Esc cancela)"}, act...)
		}
	}
	m.syncViews()
	return buildView(viewData{
		Session:     panelLines(redacted, m.sessBodyWidth()),
		Activity:    panelLines(act, m.actBodyWidth()),
		Prompt:      m.input,
		Cursor:      m.cursor,
		Footer:      buildFooter(info),
		Width:       m.width,
		Height:      m.height,
		Focus:       m.focus,
		SessionOff:  m.sessionOff,
		ActivityOff: m.activityOff,
		SessHOff:    m.hoff[panelSession],
		ActHOff:     m.hoff[panelActivity],
		GateLines:   m.gateLines(),
		Banner:      sanitize(m.banner),
	})
}

// runTUI is the M9 delivery entrypoint. It builds the Bubble Tea program over
// the shared agent/audit/config, wires the Event observer and the gate bridge
// into tea messages, and blocks until the operator quits. The REPL and this
// TUI consume the exact same core (DI: the loop never knows the frontend).
//
// Wiring notes (spec 09 §4 "nenhuma execucao por atalho"):
//   - The approver is the *tuiGate built by main: only y/n/d in the dialog
//     resolve a gate.Decide; there is no other approval code path.
//   - The observer (loop.SetObserver) forwards redacted Events onto the
//     activity panel via tea.Program.Send — never by poking the loop.
func runTUI(agent *loop.Loop, gate *tuiGate, cfg *config.Config, version, sessionName string) {
	m := &tuiModel{
		agent:       agent,
		gate:        gate,
		runner:      loopRunner{agent: agent},
		usageFn:     agent.ContextUsage,
		version:     version,
		provider:    cfg.Provider,
		model:       agent.CurrentModel(),
		toolbox:     cfg.ToolboxMode,
		sessionName: sessionName,
		width:       80,
		promptHist:  agent.PromptList(),
		activity:    []string{},
		session:     []string{},
		usage:       agent.ContextUsage(),
		// M13: honest degradation. A terminal without Braille gets the ASCII
		// cycle, which still MOVES. A frozen spinner would be worse than none:
		// it would confirm the freeze the operator is trying to rule out.
		asciiSpin: !termSupportsBraille(),
		hoff:      map[panelID]int{},
	}
	m.resetPrompt()
	if m.provider == "" {
		m.provider = "default"
	}

	p := tea.NewProgram(m, tea.WithAltScreen())

	agent.SetObserver(func(ev loop.Event) { p.Send(agentEventMsg{ev: ev}) })
	go bridgeGate(gate, p)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
	}
}

// bridgeGate forwards tuiGate dialogs (opened/closed) into the bubbletea event
// loop. The gate lives on the loop's turn goroutine; the bridge is the only
// channel that touches the UI.
func bridgeGate(gate *tuiGate, p *tea.Program) {
	for {
		select {
		case open, ok := <-gate.opened():
			if !ok {
				return
			}
			p.Send(gateOpenMsg{dialog: open})
		case _, ok := <-gate.closed():
			if !ok {
				return
			}
			p.Send(gateClosedMsg{})
		}
	}
}
