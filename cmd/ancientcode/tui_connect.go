// M14: the /connect panel for the TUI (D-2026-10-02-66).
//
// Why a panel and not a raw readSecret(): the TUI already owns the terminal in
// Bubble Tea's alt-screen, and readSecret (the REPL/CLI path) drives termios
// directly. Two raw-input drivers on one terminal fight over ECHO and the
// cursor, and the paste of an ESC sequence would go to whichever won the race.
// So the panel keeps the key entirely inside the Bubble Tea key stream: it is
// captured as runes, masked for the paint, and never echoed to the terminal.
//
// The panel has two stages, exactly like the M12 picker it reuses:
//  1. choose a provider (type to filter, ↑↓ to move, Enter to select);
//  2. type the key masked (Enter verifies + stores, Esc goes back).
//
// Both are fail-closed: Esc cancels without writing, and a rejected key never
// reaches the store (the verify-then-store order lives in connect.Service).
package main

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/antonioghizidev/ancientcode/internal/config"
	"github.com/antonioghizidev/ancientcode/internal/connect"
	"github.com/antonioghizidev/ancientcode/internal/creds"
	"github.com/antonioghizidev/ancientcode/internal/loop"
)

// connectStage is where the panel is in the choose→type→verify flow.
type connectStage int

const (
	connectStageProvider connectStage = iota // picking a provider
	connectStageKey                          // typing the masked key
	connectStageBusy                         // verify request in flight
	connectStageResult                       // finished; showing the outcome
)

// connectPanel is the open /connect panel. A nil *connectPanel means closed.
type connectPanel struct {
	svc   *connect.Service
	agent *loop.Loop

	stage connectStage

	// Stage 1: provider selection.
	names  []string // every built-in provider, sorted
	vis    []string // what the current query filters to
	query  string
	cur    int
	active string // provider currently selected in the session

	// Stage 2: key entry. key holds the real characters; it is masked on paint
	// and zeroed as soon as the request is dispatched.
	key    string
	target string // provider the key is for

	// Stage 2/3: the async verify+store outcome.
	busy   bool
	result string
	errMsg string
}

// connectDoneMsg carries the verify+store result back into Update.
type connectDoneMsg struct {
	res connect.Result
	err error
}

// newConnectPanel builds the panel with the provider list pre-filled. The
// active provider is pre-highlighted so the common case (reconnecting the
// provider in use) is one Enter away.
func newConnectPanel(svc *connect.Service, agent *loop.Loop) *connectPanel {
	p := &connectPanel{svc: svc, agent: agent, names: config.ProviderNames()}
	if agent != nil {
		p.active = strings.ToLower(strings.TrimSpace(agent.CurrentProvider()))
	}
	p.visible()
	// Start on the active provider when there is one; otherwise on the first.
	for i, n := range p.vis {
		if n == p.active {
			p.cur = i
			break
		}
	}
	return p
}

// visible recomputes the filtered provider list and keeps the cursor in range.
func (p *connectPanel) visible() {
	q := strings.ToLower(strings.TrimSpace(p.query))
	p.vis = p.vis[:0]
	for _, n := range p.names {
		if q == "" || strings.Contains(n, q) {
			p.vis = append(p.vis, n)
		}
	}
	if p.cur >= len(p.vis) {
		p.cur = len(p.vis) - 1
	}
	if p.cur < 0 {
		p.cur = 0
	}
}

// move moves the provider highlight by delta.
func (p *connectPanel) move(delta int) {
	if len(p.vis) == 0 {
		p.cur = 0
		return
	}
	p.cur += delta
	if p.cur < 0 {
		p.cur = 0
	}
	if p.cur >= len(p.vis) {
		p.cur = len(p.vis) - 1
	}
}

// selectedProvider is the provider under the cursor, or "" when the filter
// matched nothing.
func (p *connectPanel) selectedProvider() string {
	if p.cur < 0 || p.cur >= len(p.vis) {
		return ""
	}
	return p.vis[p.cur]
}

// selectProvider moves from stage 1 to stage 2 for the highlighted provider.
func (p *connectPanel) selectProvider(name string) {
	if err := creds.ValidateProvider(name); err != nil {
		p.errMsg = err.Error()
		return
	}
	p.target = name
	p.stage = connectStageKey
	p.key = ""
	p.errMsg = ""
}

// backspaceKey removes the last character from the typed key.
func (p *connectPanel) backspaceKey() {
	r := []rune(p.key)
	if len(r) == 0 {
		return
	}
	p.key = string(r[:len(r)-1])
}

// masked renders the typed key as asterisks. It never returns the real value,
// so this is the ONLY way the paint can see the key — by construction the key
// cannot reach scrollback even if a future refactor forgets a redaction.
func (p *connectPanel) masked() string {
	n := len([]rune(p.key))
	if n == 0 {
		return ""
	}
	return strings.Repeat("*", n)
}

// dispatch hands the key to connect.Service for verify+store. It returns a
// tea.Cmd so the HTTP request runs OFF the Bubble Tea event loop (the same
// reasoning as switchModelCmd: anything that could block must not run inside
// Update). The key is zeroed on the panel the moment it is handed over, so a
// later repaint cannot render it.
// modelHint is the model the session would actually use. Empty is fine: the
// probe then falls back to the provider catalog.
func modelHint(a *loop.Loop) string {
	if a == nil {
		return ""
	}
	return a.CurrentModel()
}

func (m *tuiModel) connectDispatch(p *connectPanel) tea.Cmd {
	svc, target, key := p.svc, p.target, p.key
	p.key = ""
	p.busy = true
	p.stage = connectStageBusy
	return func() tea.Msg {
		// Order matters: LookupProviderDefault returns (baseURL, apiKeyEnv, ok).
		// Reading them the other way round passes the env var NAME where a URL
		// is expected, and the smoke on the Inspiron caught exactly that as
		// "base_url scheme \"\" must be http(s)".
		base, _, ok := config.LookupProviderDefault(target)
		if !ok || base == "" {
			return connectDoneMsg{err: errUnknownProvider(target)}
		}
		res, err := svc.Connect(context.Background(), target, base, key, modelHint(m.agent))
		return connectDoneMsg{res: res, err: err}
	}
}

// applyConnectDone renders the outcome and always names the real source
// (D-2026-10-02-66): a generic "saved" would let the operator believe an
// environment key is in play when the store pin is what actually won.
func (m *tuiModel) applyConnectDone(p *connectPanel, msg connectDoneMsg) {
	p.busy = false
	p.stage = connectStageResult
	if msg.err != nil {
		p.errMsg = msg.err.Error()
		p.result = ""
		return
	}
	verb := "chave guardada e verificada"
	if msg.res.Replaced {
		verb = "chave substituida e verificada"
	}
	p.errMsg = ""
	p.result = verb + verifiedCatalogNote(msg.res) + "; origem em uso: " + string(msg.res.Source)
	// Mirror the outcome onto the session panel so it survives scrolling away.
	m.appendSession("connect: provider " + msg.res.Provider + ": " + p.result)
	if m.agent != nil {
		// The session may still be on another provider; tell the operator how
		// to make the freshly connected provider the active one.
		if msg.res.Provider != strings.ToLower(strings.TrimSpace(m.agent.CurrentProvider())) {
			m.appendSession("  /provider " + msg.res.Provider + " para usar este provider")
		}
	}
}

// closeConnect drops the panel and returns the activity viewport to the top.
func (m *tuiModel) closeConnect() {
	m.connect = nil
	m.activityOff = 0
}

// handleConnectKey owns the keyboard while the panel is open.
func (m *tuiModel) handleConnectKey(k tea.KeyMsg) tea.Cmd {
	p := m.connect
	switch p.stage {
	case connectStageProvider:
		return m.handleConnectProviderKey(p, k)
	case connectStageKey:
		return m.handleConnectKeyStage(p, k)
	case connectStageBusy:
		// While the verify request is in flight the panel is modal: only Ctrl+C
		// is honoured, so a stray Enter cannot double-submit the same key.
		if k.Type == tea.KeyCtrlC {
			m.closeConnect()
			m.banner = "connect cancelado durante a verificacao"
			return nil
		}
		return nil
	case connectStageResult:
		return m.handleConnectResultKey(p, k)
	}
	return nil
}

// handleConnectProviderKey is stage 1: filter, move, select, cancel.
func (m *tuiModel) handleConnectProviderKey(p *connectPanel, k tea.KeyMsg) tea.Cmd {
	switch k.Type {
	case tea.KeyEsc:
		m.closeConnect()
		m.banner = "connect cancelado"
		return nil
	case tea.KeyCtrlC:
		m.closeConnect()
		return tea.Quit
	case tea.KeyEnter:
		name := p.selectedProvider()
		if name == "" {
			p.errMsg = "nenhum provedor corresponde; ajuste o filtro"
			return nil
		}
		p.selectProvider(name)
		return nil
	case tea.KeyUp:
		p.move(-1)
		return nil
	case tea.KeyDown:
		p.move(+1)
		return nil
	case tea.KeyHome:
		p.cur = 0
		return nil
	case tea.KeyEnd:
		p.cur = len(p.vis) - 1
		if p.cur < 0 {
			p.cur = 0
		}
		return nil
	case tea.KeyBackspace:
		r := []rune(p.query)
		if len(r) > 0 {
			p.query = string(r[:len(r)-1])
			p.cur = 0
			p.visible()
		}
		return nil
	case tea.KeySpace:
		p.query += " "
		p.cur = 0
		p.visible()
		return nil
	case tea.KeyRunes:
		for _, r := range k.Runes {
			if r >= ' ' {
				p.query += strings.ToLower(string(r))
			}
		}
		p.cur = 0
		p.visible()
		return nil
	}
	return nil
}

// handleConnectKeyStage is stage 2: masked entry, submit, back, cancel.
func (m *tuiModel) handleConnectKeyStage(p *connectPanel, k tea.KeyMsg) tea.Cmd {
	switch k.Type {
	case tea.KeyEsc:
		// Esc goes BACK to provider selection (so a mistyped target is fixable)
		// without discarding the panel; a second Esc cancels. The typed key is
		// dropped on the way out — it is not carried between stages.
		p.key = ""
		p.stage = connectStageProvider
		p.cur = 0
		for i, n := range p.vis {
			if n == p.target {
				p.cur = i
			}
		}
		p.visible()
		return nil
	case tea.KeyCtrlC:
		p.key = ""
		m.closeConnect()
		m.banner = "connect cancelado"
		return tea.Quit
	case tea.KeyEnter:
		if strings.TrimSpace(p.key) == "" {
			p.errMsg = "chave vazia; nada foi guardado"
			return nil
		}
		if p.svc == nil {
			p.errMsg = "store de credenciais indisponivel"
			return nil
		}
		p.errMsg = ""
		return m.connectDispatch(p)
	case tea.KeyBackspace:
		p.backspaceKey()
		return nil
	case tea.KeySpace:
		p.key += " "
		return nil
	case tea.KeyRunes:
		p.key = appendKeyRunes(p.key, k.Runes)
		return nil
	}
	return nil
}

// appendKeyRunes filters pasted runes into what may become part of a key.
//
// Two classes are dropped, and the second one only matters on paste:
//
//   - control characters (< 0x20 and DEL). An interactive ESC arrives as its
//     own tea.KeyEsc, but a PASTE containing an escape sequence arrives as raw
//     runes; without this, a control byte could reach the key AND the paint.
//   - a whole CSI sequence (ESC [ ... final). Dropping only the ESC would leave
//     the printable tail ("[A", "[2J") in the key, so the operator would see
//     fewer asterisks than characters pasted and the key would just fail
//     verification later with no clue why.
func appendKeyRunes(dst string, runes []rune) string {
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == 0x1b {
			i += csiTailLen(runes[i+1:])
			continue
		}
		if r < ' ' || r == 0x7f {
			continue
		}
		dst += string(r)
	}
	return dst
}

// csiTailLen reports how many runes belong to a CSI sequence that starts with
// "[": the parameters, then the final byte. It returns 0 when "[" is not
// followed by a plausible final byte.
func csiTailLen(rest []rune) int {
	for i, r := range rest {
		if r == '[' {
			// Params are 0x30-0x3F, intermediates 0x20-0x2F, final 0x40-0x7E.
			j := i + 1
			for j < len(rest) && rest[j] >= 0x20 && rest[j] <= 0x3f {
				j++
			}
			if j < len(rest) && rest[j] >= 0x40 && rest[j] <= 0x7e {
				return j + 1
			}
			return i + 1
		}
		// Not a CSI: ESC followed by anything else (e.g. ESC O for SS3).
		if i == 0 {
			return 0
		}
	}
	return 0
}

// handleConnectResultKey closes the panel from the result stage.
func (m *tuiModel) handleConnectResultKey(p *connectPanel, k tea.KeyMsg) tea.Cmd {
	switch k.Type {
	case tea.KeyEnter, tea.KeyEsc:
		m.closeConnect()
		return nil
	case tea.KeyCtrlC:
		m.closeConnect()
		return tea.Quit
	}
	return nil
}

// connectLines renders the panel body for the paint. It NEVER returns the real
// key: stage 2 renders p.masked(), and every other stage has no key at all.
func connectLines(p *connectPanel) []string {
	switch p.stage {
	case connectStageProvider:
		return connectProviderLines(p)
	case connectStageKey:
		return connectKeyLines(p)
	case connectStageBusy:
		return connectBusyLines(p)
	case connectStageResult:
		return connectResultLines(p)
	}
	return nil
}

// connectProviderLines renders stage 1 (provider choice).
func connectProviderLines(p *connectPanel) []string {
	out := make([]string, 0, len(p.vis)+6)
	if p.query == "" {
		out = append(out, "  /connect — escolha o provedor   (digite filtra, ↑↓ move)")
	} else {
		out = append(out, "  /connect — filtrando: "+p.query+"   Enter seleciona")
	}
	for i, n := range p.vis {
		marker := "  "
		if i == p.cur {
			marker = "> "
		}
		line := marker + n
		if st := p.status(n); st != "" {
			line += "   " + st
		}
		out = append(out, line)
	}
	if len(p.vis) == 0 {
		out = append(out, "  (nenhum provedor corresponde)")
	}
	if p.errMsg != "" {
		out = append(out, "  ! "+p.errMsg)
	}
	out = append(out, "  Enter conecta   Esc cancela   Ctrl+C sai")
	return out
}

// connectKeyLines renders stage 2 (masked key entry). The label and the mask
// are the only things about the key on screen.
func connectKeyLines(p *connectPanel) []string {
	out := make([]string, 0, 6)
	out = append(out, "  chave de "+p.target+" (não será exibida): "+p.masked())
	if p.key == "" {
		out = append(out, "  cole a chave e pressione Enter   Esc volta   Ctrl+C sai")
	} else {
		out = append(out, "  Enter verifica e guarda   Esc volta   Ctrl+C sai")
	}
	out = append(out, "  a chave é verificada no provedor antes de ser guardada; nunca vai no argv.")
	if p.errMsg != "" {
		out = append(out, "  ! "+p.errMsg)
	}
	return out
}

// connectBusyLines renders the in-flight verify.
func connectBusyLines(p *connectPanel) []string {
	return []string{
		"  verificando a chave de " + p.target + " no provedor…",
		"  (nada é guardado se o provedor recusar)   Ctrl+C cancela",
	}
}

// connectResultLines renders the outcome, naming the real source.
func connectResultLines(p *connectPanel) []string {
	out := make([]string, 0, 4)
	if p.errMsg != "" {
		out = append(out, "  ! "+p.errMsg)
		out = append(out, "  (nada foi guardado)   Enter volta")
		return out
	}
	out = append(out, "  ✓ "+p.result)
	out = append(out, "  Enter volta")
	return out
}

// status renders the current credential source for a provider, so stage 1 shows
// which providers are already configured (and from where) before the operator
// reconnects. It never shows a key.
func (p *connectPanel) status(name string) string {
	if p.svc == nil {
		return ""
	}
	_, env, ok := config.LookupProviderDefault(name)
	if !ok {
		return ""
	}
	st := p.svc.StatusFor(name, env)
	if !st.Configured {
		return "sem credencial"
	}
	return "origem: " + string(st.Source)
}

// errUnknownProvider is the fail-closed guard for a provider id that vanished
// between panel construction and dispatch (defensive; the built-in set is
// static, but the lookup is the authority on what exists).
type errUnknownProvider string

func (e errUnknownProvider) Error() string {
	return "connect: provedor desconhecido: " + string(e)
}
