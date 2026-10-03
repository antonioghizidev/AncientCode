package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/antonioghizidev/ancientcode/internal/loop"
)

// M13 — animação de atividade e controle do turno. O pedido do operador: com a
// tela estática ele não distingue "processando" de "abortado" de "congelado", e
// precisa poder interromper, corrigir o prompt e continuar sem redigitar.

// blockingRunner blocks until the context it was given is cancelled, so a turn
// can be observed while in flight. The harness never executes the returned Cmd,
// so tests drive the in-flight state through flyTurn and assert on the model.
type blockingRunner struct {
	ctx context.Context // set by flyTurn: the cancellable context of the turn
}

func (b *blockingRunner) run(ctx context.Context, user string) (string, error) {
	wait := b.ctx
	if wait == nil {
		wait = ctx
	}
	<-wait.Done()
	return "", wait.Err()
}

func (b *blockingRunner) cont(ctx context.Context) (string, error) { return b.run(ctx, "") }

// drain executes the commands a Cmd would have run (including the ones nested in
// a tea.Batch) so the fake runner actually observes the submission. The harness
// deliberately does not run Cmds; this stands in for Bubble Tea's event loop.
func drain(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	switch b := msg.(type) {
	case tea.BatchMsg:
		for _, c := range b {
			drain(c)
		}
	}
}

// flyTurn puts the model in the state of a turn that is genuinely in flight:
// context cancellable, prompt recorded, input already cleared by the submit.
func (m *tuiModel) flyTurn(prompt string, r turnRunner) (*tuiModel, chan struct{}) {
	cancelled := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	if br, ok := r.(*blockingRunner); ok {
		br.ctx = ctx
	}
	go func() {
		<-ctx.Done()
		close(cancelled)
	}()
	m.runner = r
	m.inTurn = true
	m.frame = 0
	m.turnSeq++
	m.flyingPrompt = prompt
	m.turnCancel = cancel
	m.input = ""
	m.cursor = 0
	return m, cancelled
}

// O spinner só existe enquanto há turno em andamento: parado, ele seria pior que
// nenhum — confirmaria justamente o congelamento que o operador quer excluir.
func TestSpinnerOnlyTicksDuringATurn(t *testing.T) {
	m := mkModel(t, nil)
	if got := m.spinnerLine(); strings.TrimSpace(got) != "" {
		t.Errorf("idle model must not show a spinner, got %q", got)
	}

	m.inTurn = true
	first := m.spinnerFrame()
	// O tick avança o frame.
	m, _ = upd(m, spinTickMsg{})
	if m.spinnerFrame() == first {
		t.Errorf("tick must advance the spinner frame (stuck at %q)", first)
	}
	// Ao fim do turno, some de vez (sem frame residual).
	m.inTurn = false
	if got := strings.TrimSpace(m.spinnerLine()); got != "" {
		t.Errorf("spinner must disappear when the turn ends, got %q", got)
	}
}

// O frame é um único glyph visível e nunca payload de resposta.
func TestSpinnerLineIsSingleGlyph(t *testing.T) {
	m := mkModel(t, nil)
	m.inTurn = true
	line := m.spinnerLine()
	f := strings.TrimSpace(line)
	if len([]rune(f)) != 1 {
		t.Errorf("spinner line must be a single glyph, got %q", f)
	}
}

// Esc cancela o turno em andamento. Sem turno, Esc NÃO sai — trocar Esc por
// "sair" seria uma regressão de gesto.
func TestEscCancelsTheTurnButNeverQuits(t *testing.T) {
	r := &blockingRunner{}
	m := mkModel(t, nil)
	m, cancelled := m.flyTurn("verifique o hardware", r)

	_, cmd := upd(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.inTurn {
		t.Error("Esc must cancel the turn in flight")
	}
	if findQuit(cmd) {
		t.Error("Esc must NOT quit the application")
	}
	// O cancelamento realmente aborta o contexto do turno.
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Error("Esc must cancel the turn's context, not just the UI flag")
	}
	if !sessionContains(m, "turno cancelado") {
		t.Errorf("session must record the cancellation, got %v", m.session)
	}

	// Sem turno em andamento, Esc é inofensivo.
	m2 := mkModel(t, nil)
	_, cmd = upd(m2, tea.KeyMsg{Type: tea.KeyEsc})
	if findQuit(cmd) {
		t.Error("Esc without a turn must not quit")
	}
}

// Cancelar DEVOLVE o prompt ao input: hoje o texto é descartado e o operador
// precisa redigitar tudo para corrigir uma rota.
func TestCancelHandsThePromptBack(t *testing.T) {
	m := mkModel(t, nil)
	m, _ = m.flyTurn("verifique o hardware deste computador", &blockingRunner{})

	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.input != "verifique o hardware deste computador" {
		t.Errorf("the prompt must come back for editing, got %q", m.input)
	}
	if m.cursor != len([]rune(m.input)) {
		t.Errorf("cursor must land at the end, got %d of %d", m.cursor, len([]rune(m.input)))
	}
	// E dá para editar e reenviar sem redigitar.
	m.input = "verifique o hardware, mas só a memória"
	f := &fakeRunner{out: "ok"}
	m.runner = f
	_, cmd := upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	drain(cmd)
	if len(f.turns) != 1 || f.turns[0] != "verifique o hardware, mas só a memória" {
		t.Errorf("the corrected prompt must be sent as typed, got %v", f.turns)
	}
}

// Cancelar registra quais tools JÁ executaram: reenviar o prompt repete o efeito
// de `write_file`/`docker rm`. O operador precisa saber, não ser exposto a isso
// em silêncio.
func TestCancelRecordsToolsThatAlreadyRan(t *testing.T) {
	m := mkModel(t, nil)
	m, _ = m.flyTurn("instale o pacote X", &blockingRunner{})
	m.turnTools = []string{"write_file", "shell"}
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyEsc})

	if len(m.cancelled) != 1 {
		t.Fatalf("expected one cancelled turn, got %d", len(m.cancelled))
	}
	c := m.cancelled[0]
	if c.prompt != "instale o pacote X" {
		t.Errorf("cancelled prompt recorded as %q", c.prompt)
	}
	if len(c.tools) != 2 || c.tools[0] != "write_file" || c.tools[1] != "shell" {
		t.Errorf("tools that ran must be recorded, got %v", c.tools)
	}
	// E a linha de aviso precisa dizer quais foram, senão o aviso não informa.
	found := false
	for _, l := range m.session {
		if strings.Contains(l, "write_file") && strings.Contains(l, "shell") {
			found = true
		}
	}
	if !found {
		t.Errorf("the warning line must name the tools that already ran: %v", m.session)
	}
}

// /continue reenvia o cancelado UMA vez; a segunda recusa (fail-closed) — reenviar
// duas vezes repetiria efeitos de tools.
func TestContinueReSendsTheCancelledTurnExactlyOnce(t *testing.T) {
	f := &fakeRunner{out: "ok"}
	m := mkModel(t, nil)
	m, _ = m.flyTurn("primeira tentativa", &blockingRunner{})
	m.runner = f
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyEsc})

	m.input = "/continue"
	_, cmd := upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	drain(cmd)
	if len(f.turns) != 1 || f.turns[0] != "primeira tentativa" {
		t.Fatalf("/continue must re-send the cancelled prompt exactly once, got %v", f.turns)
	}
	if len(m.cancelled) != 0 {
		t.Errorf("a consumed cancellation must be cleared, got %d left", len(m.cancelled))
	}

	// Segunda vez: a entrada consumida NÃO pode ser reenviada de novo — reenviar
	// duas vezes repetiria os efeitos das tools.
	f2 := &fakeRunner{out: "ok"}
	m.runner = f2
	m.input = "/continue"
	_, cmd = upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	drain(cmd)
	for _, sent := range f2.turns {
		if sent == "primeira tentativa" {
			t.Errorf("a consumed cancellation must not be re-sent: %v", f2.turns)
		}
	}
	if m.inTurn {
		// O /continue legado (retomar o último turno) continua valendo, mas não
		// pode ressuscitar o prompt cancelado.
		t.Logf("/continue fell back to the legacy continue path: turns=%v", f2.turns)
	}
}

// Com NENHUMA entrada de cancelamento e sem histórico, /continue não pode enviar
// nada (fail-closed).
func TestContinueIsFailClosedWithNothingToResume(t *testing.T) {
	m := mkModel(t, nil)
	if len(m.cancelled) != 0 {
		t.Fatalf("precondition: no cancellations, got %d", len(m.cancelled))
	}
	// Com a lista vazia, o caminho do M13 recusa explicitamente.
	if cmd := m.reSendCancelled(); cmd != nil {
		t.Error("reSendCancelled must refuse with no cancelled turn")
	}
	if !strings.Contains(m.banner, "nada a continuar") {
		t.Errorf("banner must explain the refusal, got %q", m.banner)
	}
}

// /continue avisa quais tools já executaram antes de reenviar (opção 1 escolhida
// pelo usuário): honesto, e não limita o operador.
func TestContinueWarnsAboutToolsThatAlreadyRan(t *testing.T) {
	m := mkModel(t, nil)
	m, _ = m.flyTurn("tente de novo", &blockingRunner{})
	m.runner = &fakeRunner{out: "ok"}
	m.turnTools = []string{"write_file"}
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyEsc})

	m.input = "/continue"
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(m.banner, "write_file") {
		t.Errorf("/continue must warn about the tool that already ran, got banner %q", m.banner)
	}
	// Ainda assim reenvia (opção 1: avisar, não recusar).
	if m.inTurn != true {
		t.Error("/continue must still re-send after warning")
	}
}

// A seta para cima recupera o prompt ANTERIOR como texto editável, e reenviar não
// duplica a entrada no histórico (duplicar faria ↑ pular entradas).
func TestArrowUpEditsPreviousPromptWithoutDuplicatingHistory(t *testing.T) {
	f := &fakeRunner{out: "ok"}
	m := mkModel(t, nil)
	m.runner = f
	m.input = "primeiro"
	_, cmd := upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	drain(cmd)
	m, _ = upd(m, turnResultMsg{out: "r", err: nil})
	m.input = "segundo"
	_, cmd = upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	drain(cmd)
	m, _ = upd(m, turnResultMsg{out: "r", err: nil})

	if len(m.promptHist) != 2 {
		t.Fatalf("history should hold 2 prompts, got %v", m.promptHist)
	}
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.input != "segundo" {
		t.Errorf("up must bring back the last prompt, got %q", m.input)
	}
	// Edita e reenvia: o histórico não ganha uma terceira entrada igual.
	m.input = "segundo (editado)"
	_, cmd = upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	drain(cmd)
	want := []string{"primeiro", "segundo", "segundo (editado)"}
	if len(m.promptHist) != len(want) {
		t.Fatalf("history must hold exactly %d entries, got %v", len(want), m.promptHist)
	}
	for i, p := range want {
		if m.promptHist[i] != p {
			t.Errorf("history[%d] = %q, want %q (duplicated or reordered: %v)", i, m.promptHist[i], p, m.promptHist)
		}
	}
	// E ↑ a partir do prompt mais recente não duplica nada.
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyUp})
	if len(m.promptHist) != len(want) {
		t.Errorf("walking the history must not mutate it, got %v", m.promptHist)
	}
}

// Uma resposta longa é rolável linha a linha: com offsets lógicos, ↓ pularia a
// resposta inteira, que é exatamente a reclamação que abriu o M13.
func TestLongAnswerScrollsRowByRow(t *testing.T) {
	m := mkModel(t, nil)
	m.height = 14
	m.focus = panelSession
	m.appendSession("< " + strings.Repeat("linha de saida do comando\n", 20))

	m.syncViews()
	if len(m.sessionView) < 20 {
		t.Fatalf("a 20-line answer must project to many physical rows, got %d", len(m.sessionView))
	}
	vis := m.panelContentRows()
	m.sessionOff = 0
	// Um passo de ↓ avança UMA linha física, não o bloco inteiro.
	m.navigatePanel(+1)
	if m.sessionOff != 1 {
		t.Errorf("one ↓ must move one physical row, got offset %d", m.sessionOff)
	}
	if m.sessionOff >= len(m.sessionView)-vis {
		t.Errorf("offset %d jumped past the answer (%d rows, %d visible)", m.sessionOff, len(m.sessionView), vis)
	}
}

// Nada de conteúdo remoto sobrevive como sequência de controle: o painel quebra
// texto, não injeta ANSI.
func TestPanelProjectionNeverDropsOrLeaksControlSequences(t *testing.T) {
	evil := "> " + strings.Repeat("A", 300) + "\x1b[2J\x1b[31m"
	clean := sanitize(redactForTest(evil))
	got := panelLines([]string{clean}, 40)
	for i, l := range got {
		if strings.Contains(l, "\x1b") {
			t.Errorf("line %d leaked an escape sequence: %q", i, l)
		}
		if n := len([]rune(l)); n > 40 {
			t.Errorf("line %d overflows the panel: %d cells", i, n)
		}
	}
	// E o texto útil continua inteiro depois de sanitizar+quebrar: tirando o
	// marcador e as indentações, a projeção devolve os As.
	if back := unwrapPanelLines(strings.Join(got, "\n")); !strings.Contains(back, strings.Repeat("A", 38)) {
		t.Errorf("sanitized content was lost by the projection: %q", back)
	}
}

// O gate humano continua mostrando o payload completo: a quebra de linha não
// pode "economizar" linhas no Approvar (M11 — aprovação sem ler não é
// consentimento informado).
func TestGateStillShowsFullPayloadAfterWrap(t *testing.T) {
	payload := "shell: " + strings.Repeat("df -h ", 40)
	m := mkModel(t, nil)
	m.dialog = &gateOpen{Tool: "shell", Summary: payload}
	lines := m.gateLines()
	// O comando tem que aparecer inteiro. A quebra é por caractere, então o
	// espaço exatamente no ponto de quebra é consumido — o invariante honesto é
	// que NENHUM caractere de conteúdo se perca (comparado sem espaços).
	var body strings.Builder
	for _, l := range lines[1:] {
		body.WriteString(strings.TrimLeft(l, " "))
	}
	got := strings.ReplaceAll(body.String(), " ", "")
	want := strings.ReplaceAll(strings.Repeat("df -h ", 40), " ", "")
	if got != want {
		t.Errorf("the gate payload must be rendered whole: %d content chars, want %d (lines: %q)",
			len(got), len(want), lines)
	}
}

// sessionContains is a substring check over the session log (sessHas compares
// whole lines, which is too strict for messages with a reason suffix).
func sessionContains(m *tuiModel, want string) bool {
	for _, l := range m.session {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}

// Cancelar é um estado TERMINAL distinto, nunca sucesso: o turno terminou sem
// resposta final. A spec M13 exige que "abortado" e "congelou" fiquem
// distinguíveis também fora da tela.
func TestCancelledTurnIsNotReportedAsSuccess(t *testing.T) {
	m := mkModel(t, nil)
	m, _ = m.flyTurn("algo longo", &blockingRunner{})
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyEsc})

	// A projeção da atividade nunca pinta cancelamento como "ok".
	if line := activityFromEvent(loop.Event{Phase: loop.PhaseTurnOK, Tool: "shell", Dur: 100}); line == "" {
		t.Error("a finished tool must still project normally")
	}
	cancelled := activityFromEvent(loop.Event{Phase: loop.PhaseTurnCancelled, Detail: "Esc"})
	if cancelled == "" {
		t.Fatal("a cancelled turn must project an activity line")
	}
	if strings.Contains(cancelled, " ok") {
		t.Errorf("a cancelled turn must never be rendered as ok: %q", cancelled)
	}
	// E o painel de turno não tem fase de sucesso pendente: o spinner morre.
	if m.spinnerLine() != "" {
		t.Error("the spinner must die with the cancelled turn")
	}
	// Detail remoto no evento de cancelamento é sanitizado como qualquer outro.
	evil := activityFromEvent(loop.Event{Phase: loop.PhaseTurnCancelled, Detail: "Esc\x1b[2J"})
	if strings.ContainsRune(evil, 0x1b) {
		t.Errorf("cancelled detail must be sanitized: %q", evil)
	}
}

// O histórico de cancelamentos é limitado: uma sessão longa não pode crescer sem
// teto e vazar memória.
func TestCancelledHistoryIsBounded(t *testing.T) {
	m := mkModel(t, nil)
	for i := 0; i < maxCancelled*3; i++ {
		m, _ = m.flyTurn("p"+itoa(i), &blockingRunner{})
		m, _ = upd(m, tea.KeyMsg{Type: tea.KeyEsc})
	}
	if len(m.cancelled) != maxCancelled {
		t.Errorf("cancellation history must cap at %d, got %d", maxCancelled, len(m.cancelled))
	}
	// E o que sobra é o mais recente — o que o /continue deve reenviar.
	last := m.cancelled[len(m.cancelled)-1]
	if want := "p" + itoa(maxCancelled*3-1); last.prompt != want {
		t.Errorf("the newest cancellation must be kept, got %q want %q", last.prompt, want)
	}
}

// Cancelar não pode parecer erro. O turno interrompido volta como
// `context canceled`, que é o resultado ESPERADO — pintar isso em vermelho
// diria ao operador que a ação deliberada dele quebrou algo.
func TestCancelledTurnIsNotPaintedAsAnError(t *testing.T) {
	m := mkModel(t, nil)
	m, _ = m.flyTurn("conte devagar", &blockingRunner{})
	m, _ = upd(m, tea.KeyMsg{Type: tea.KeyEsc})

	// O turno em voo devolve o erro de cancelamento, como o runtime faria.
	m, _ = upd(m, turnResultMsg{err: context.Canceled})

	if strings.Contains(m.banner, "erro") {
		t.Errorf("a cancelled turn must not set an error banner, got %q", m.banner)
	}
	for _, l := range m.session {
		if strings.Contains(l, "! erro") {
			t.Errorf("a cancelled turn must not add an error line to the session: %q", l)
		}
	}
	// E o aviso de cancelamento continua lá.
	if !sessionContains(m, "turno cancelado") {
		t.Errorf("the cancellation notice must survive, got %v", m.session)
	}
	// Um erro REAL depois de um cancelamento continua sendo erro.
	m, _ = upd(m, turnResultMsg{err: errors.New("provider explodiu")})
	if !strings.Contains(m.banner, "erro") {
		t.Errorf("a real failure must still be reported, got banner %q", m.banner)
	}
}

// Regressão (achada no smoke do Inspiron): as teclas de atalho `r`/`c` disparavam
// sempre que havia banner na tela, e ENGIAM a letra. "verifique" virava
// "veifique". O banner é justamente o que aparece depois de um erro ou de um
// cancelamento — ou seja, o bug atingia justamente o caminho de recuperação que
// o M13 criou (corrigir o prompt e reenviar).
func TestTypingIsNeverEatenByTheBannerShortcuts(t *testing.T) {
	const prompt = "verifique o hardware: chipset, processador, memoria, swap, " +
		"armazenamento, incluindo modelo e tamanho"
	for _, withBanner := range []bool{false, true} {
		m := mkModel(t, nil)
		m.runner = &fakeRunner{out: "ok"}
		if withBanner {
			m.banner = "turno cancelado (Esc)"
		}
		for _, r := range prompt {
			m, _ = upd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		if m.input != prompt {
			t.Errorf("banner=%v: typing lost characters: got %q", withBanner, m.input)
		}
		if m.inTurn {
			t.Errorf("banner=%v: typing must not start a turn", withBanner)
		}
	}
}

// O atalho `r` continua funcionando quando o operador QUER retry — com o input
// vazio, que é a intenção original.
func TestBannerRetryShortcutStillWorksOnEmptyInput(t *testing.T) {
	f := &fakeRunner{out: "re-run"}
	m := mkModel(t, nil)
	m.runner = f
	m.banner = "erro: algo"
	_, cmd := upd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	feedTurn(t, m, cmd)
	if f.continues != 1 {
		t.Errorf("`r` on an empty input must retry the turn, got %d continues", f.continues)
	}
	if m.banner != "" {
		t.Errorf("retry must clear the banner, got %q", m.banner)
	}
}

// helpers locais

// redactForTest is a seam: the session pipeline calls redact+sanitize before the
// projection; the test supplies the sanitize half and asserts on the result.
func redactForTest(s string) string { return s }
