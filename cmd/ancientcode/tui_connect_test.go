package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/antonioghizidev/ancientcode/internal/connect"
	"github.com/antonioghizidev/ancientcode/internal/creds"
)

func newConnPanelForTest(t *testing.T) (*tuiModel, *connectPanel) {
	t.Helper()
	store, err := creds.Open(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc := connect.New(store, fakeOKVerifier{})
	m := &tuiModel{credSvc: svc, hoff: map[panelID]int{}}
	return m, newConnectPanel(svc, nil)
}

// fakeOKVerifier accepts any key, so the panel flow can be tested without a
// network. The store is real, so the "nothing written on cancel" assertions are
// about actual files.
type fakeOKVerifier struct{}

func (fakeOKVerifier) Verify(_ context.Context, _, _ string) (int, error) { return 7, nil }

// fakeRejectVerifier mimics a provider that says 401.
type fakeRejectVerifier struct{}

func (fakeRejectVerifier) Verify(_ context.Context, _, _ string) (int, error) {
	return 0, connect.ErrRejected
}

// --- S4a: a chave nao aparece na projecao, jamais ---------------------------

// A propriedade: por QUALQUER caminho de render do painel, a chave nao pode
// estar no texto. Nao basta testar "o caminho feliz" — o caminho de Erro e o de
// resultado tambem são projections.
func TestConnectPanelNeverRendersTheKey(t *testing.T) {
	const key = "nvapi-secreta-nao-pode-vazar"
	m, p := newConnPanelForTest(t)

	p.selectProvider("nvidia")
	p.key = key
	p.stage = connectStageKey

	for _, lines := range [][]string{connectLines(p)} {
		for _, l := range lines {
			if strings.Contains(l, key) {
				t.Fatalf("a chave vazou na projecao: %q", l)
			}
			if strings.Contains(l, "secreta") {
				t.Fatalf("fragmento da chave vazou: %q", l)
			}
		}
	}

	// Tambem pela View completa: o painel substitui o painel de atividade.
	m.connect = p
	m.width, m.height = 100, 24
	view := m.View()
	if strings.Contains(view, key) {
		t.Fatal("a chave vazou na View completa")
	}
	// E o eco e asterisco, como S4 exige.
	if !strings.Contains(strings.Join(connectLines(p), "\n"), strings.Repeat("*", len(key))) {
		t.Fatalf("o eco nao e uma mascara de asteriscos:\n%s", strings.Join(connectLines(p), "\n"))
	}
}

// O mesmo, no caminho de ERRO: um erro de verificacao nao pode arrastar a chave.
func TestConnectPanelErrorStageNeverRendersTheKey(t *testing.T) {
	const key = "sk-or-v1-vazaria-aqui"
	m, p := newConnPanelForTest(t)
	p.target = "openrouter"
	p.stage = connectStageBusy
	p.key = key // ainda em memoria enquanto a request corre
	m.connect = p
	m.width, m.height = 100, 24
	if view := m.View(); strings.Contains(view, key) {
		t.Fatal("a chave vazou na View durante a verificacao")
	}
	m.applyConnectDone(p, connectDoneMsg{err: errUnknownProvider("openrouter")})
	for _, l := range connectLines(p) {
		if strings.Contains(l, key) {
			t.Fatalf("a chave vazou no resultado com erro: %q", l)
		}
	}
}

// --- S9: cancelar / Esc nao escreve nada -----------------------------------

func TestConnectPanelCancelWritesNothing(t *testing.T) {
	// Stage 1: Esc fecha.
	m, p := newConnPanelForTest(t)
	m.connect = p
	m.handleConnectKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.connect != nil {
		t.Fatal("Esc no stage 1 nao fechou o painel")
	}
	if names, _ := p.svc.List(); len(names) != 0 {
		t.Fatalf("Esc no stage 1 gravou %v", names)
	}

	// Stage 2: o primeiro Esc VOLTA ao stage 1 (para o operador corrigir o
	// provider alvo) e so um segundo Esc fecha — mas nenhum dos dois grava.
	m2, p2 := newConnPanelForTest(t)
	p2.selectProvider("nvidia")
	p2.key = "chave-digitada"
	m2.connect = p2
	m2.handleConnectKey(tea.KeyMsg{Type: tea.KeyEsc})
	if p2.stage != connectStageProvider {
		t.Fatalf("o primeiro Esc deveria voltar ao stage 1, ficou em %v", p2.stage)
	}
	if names, _ := p2.svc.List(); len(names) != 0 {
		t.Fatalf("voltar ao stage 1 gravou %v", names)
	}
	m2.handleConnectKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m2.connect != nil {
		t.Fatal("o segundo Esc deveria fechar o painel")
	}
	if names, _ := p2.svc.List(); len(names) != 0 {
		t.Fatalf("fechar do stage 1 gravou %v", names)
	}
}

// Voltar do stage 2 para o stage 1 tambem nao pode gravar, e nao pode carregar
// a chave digitada entre os stages.
func TestConnectPanelBackspaceDropsTypedKey(t *testing.T) {
	m, p := newConnPanelForTest(t)
	p.selectProvider("nvidia")
	p.key = "chave-que-nao-deve-sobreviver"
	m.connect = p
	m.handleConnectKey(tea.KeyMsg{Type: tea.KeyEsc})
	if p.stage != connectStageProvider {
		t.Fatalf("Esc deveria voltar ao stage 1, ficou em %v", p.stage)
	}
	if p.key != "" {
		t.Fatalf("a chave sobreviveu a saida do stage 2: %q", p.key)
	}
	if names, _ := p.svc.List(); len(names) != 0 {
		t.Fatal("voltar do stage 2 gravou alguma coisa")
	}
}

// --- fluxo feliz: verificar e guardar ---------------------------------------

func TestConnectPanelStoresVerifiedKey(t *testing.T) {
	m, p := newConnPanelForTest(t)
	m.connect = p
	// Stage 1: filtrar por nvidia para o destaque cair nele (com agent=nil o
	// destaque comeca no primeiro provider da lista, litellm).
	for _, r := range "nvidia" {
		m.handleConnectKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.handleConnectKey(tea.KeyMsg{Type: tea.KeyEnter})
	if p.stage != connectStageKey {
		t.Fatalf("Enter no stage 1 deveria abrir o stage 2, ficou em %v", p.stage)
	}
	if p.target != "nvidia" {
		t.Fatalf("o provider alvo errado: %q", p.target)
	}
	// Stage 2: digita e submete.
	for _, r := range "nvapi-abc" {
		m.handleConnectKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if p.key != "nvapi-abc" {
		t.Fatalf("a digitacao nao acumulou: %q", p.key)
	}
	cmd := m.handleConnectKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter com chave deveria despachar a verificacao")
	}
	if p.key != "" {
		t.Fatal("a chave deveria ter sido zerada ao despachar")
	}
	// A request corre fora do event loop (mesma razao do switchModelCmd).
	msg := cmd().(connectDoneMsg)
	if msg.err != nil {
		t.Fatalf("verificacao falhou: %v", msg.err)
	}
	m.applyConnectDone(p, msg)
	if p.stage != connectStageResult {
		t.Fatalf("deveria estar no stage de resultado, ficou em %v", p.stage)
	}
	// O resultado NOMEIA a origem (D-66 / S6) — nunca um "salvo" generico.
	if !strings.Contains(p.result, "store fixada") {
		t.Fatalf("o resultado nao nomeia a origem: %q", p.result)
	}
	names, err := p.svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "nvidia" {
		t.Fatalf("store inesperado: %v", names)
	}
	// E a linha de sessao tambem nomeia a origem, para sobreviver ao scroll.
	if !strings.Contains(strings.Join(m.session, "\n"), "origem em uso: store fixada") {
		t.Fatalf("a sessao nao registrou a origem: %v", m.session)
	}
}

// --- fail-closed: chave recusada nao e guardada -----------------------------

func TestConnectPanelRejectedKeyIsNotStored(t *testing.T) {
	store, err := creds.Open(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc := connect.New(store, fakeRejectVerifier{})
	m := &tuiModel{credSvc: svc, hoff: map[panelID]int{}}
	p := newConnectPanel(svc, nil)
	m.connect = p

	p.selectProvider("nvidia")
	p.key = "chave-que-o-provedor-recusa"
	cmd := m.handleConnectKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyConnectDone(p, cmd().(connectDoneMsg))

	if names, _ := svc.List(); len(names) != 0 {
		t.Fatalf("uma chave recusada foi guardada: %v", names)
	}
	if p.errMsg == "" {
		t.Fatal("o erro do provedor deveria aparecer no painel")
	}
	if !strings.Contains(strings.Join(connectLines(p), "\n"), "nada foi guardado") {
		t.Fatalf("o painel nao diz que nada foi guardado:\n%s", strings.Join(connectLines(p), "\n"))
	}
}

// Chave vazia: nada e guardado e o motivo e mostrado.
func TestConnectPanelEmptyKeyIsRefused(t *testing.T) {
	m, p := newConnPanelForTest(t)
	m.connect = p
	p.selectProvider("nvidia")
	if cmd := m.handleConnectKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("Enter com chave vazia nao deveria despachar nada")
	}
	if names, _ := p.svc.List(); len(names) != 0 {
		t.Fatal("chave vazia foi guardada")
	}
	if p.errMsg == "" {
		t.Fatal("deveria haver um motivo")
	}
}

// --- o painel mostra a origem ANTES de conectar (stage 1) -------------------

func TestConnectPanelStage1ShowsCredentialSource(t *testing.T) {
	_, p := newConnPanelForTest(t)
	// Um provider ja configurado no ambiente.
	t.Setenv("NVIDIA_API_KEY", "x")
	p.query = "nvid"
	p.visible()
	body := strings.Join(connectProviderLines(p), "\n")
	if !strings.Contains(body, "origem: env") {
		t.Fatalf("o stage 1 nao mostra a origem do provedor ja configurado:\n%s", body)
	}
	// E um provider sem nada mostra "sem credencial", para o operador saber o
	// que /connect vai mudar.
	p.query = ""
	p.visible()
	body = strings.Join(connectProviderLines(p), "\n")
	if !strings.Contains(body, "sem credencial") {
		t.Fatalf("o stage 1 nao mostra quem esta sem credencial:\n%s", body)
	}
}

// --- filtros e navegacao -----------------------------------------------------

func TestConnectPanelFilterAndMove(t *testing.T) {
	m, p := newConnPanelForTest(t)
	m.connect = p
	for _, r := range "nvidi" {
		m.handleConnectKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(p.vis) != 1 || p.vis[0] != "nvidia" {
		t.Fatalf("filtro nao isolou o nvidia: %v", p.vis)
	}
	// Backspace limpa o filtro inteiro e restaura a lista completa.
	for i := 0; i < len("nvidia"); i++ {
		m.handleConnectKey(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	if p.query != "" {
		t.Fatalf("o filtro nao voltou a ser vazio: %q", p.query)
	}
	if len(p.vis) < 2 {
		t.Fatalf("a lista nao foi restaurada: %v", p.vis)
	}
	// move satura nos extremos em vez de dar panic.
	for i := 0; i < 20; i++ {
		m.handleConnectKey(tea.KeyMsg{Type: tea.KeyUp})
	}
	if p.cur != 0 {
		t.Fatalf("move para cima passou do inicio: cur=%d", p.cur)
	}
	for i := 0; i < 40; i++ {
		m.handleConnectKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if p.cur != len(p.vis)-1 {
		t.Fatalf("move para baixo passou do fim: cur=%d len=%d", p.cur, len(p.vis))
	}
	// Filtro sem resultado: Enter nao avanca de stage.
	m.handleConnectKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m.handleConnectKey(tea.KeyMsg{Type: tea.KeyEnter})
	if p.stage != connectStageProvider {
		t.Fatalf("Enter sem resultado abriu o stage 2: %v", p.stage)
	}
	if p.errMsg == "" {
		t.Fatal("deveria avisar que nada corresponde")
	}
}

// Uma chave colada nao pode carregar sequencia de escape (o TUI nao ecoa, mas o
// operador veria menos asteriscos do que colou e a chave falharia na verificacao
// sem explicacao) nem byte de controle solto.
func TestConnectPanelDropsControlCharsFromKey(t *testing.T) {
	m, p := newConnPanelForTest(t)
	m.connect = p
	p.selectProvider("nvidia")

	// (a) sequencia CSI colada inteira: ESC [ A -> nada entra na chave.
	m.handleConnectKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0x1b, '[', 'A', 'k'}})
	if p.key != "k" {
		t.Fatalf("uma sequencia ESC entrou na chave: %q", p.key)
	}

	// (b) byte de controle solto (0x01) e DEL: descartados, sem virar parte
	// da chave. Este e o caminho que o teste (a) NAO cobria.
	p.key = ""
	m.handleConnectKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0x01, 'a', 0x7f, 'b', 0x07}})
	if p.key != "ab" {
		t.Fatalf("bytes de controle entraram na chave: %q", p.key)
	}

	// (c) e nenhum deles sobrevive a uma View completa.
	p.key = "ab"
	m.width, m.height = 100, 24
	if v := m.View(); strings.ContainsAny(v, "\x01\x07\x7f") {
		t.Fatal("byte de controle chegou a View pelo painel")
	}
}

// O painel bloqueia submissao duplicada enquanto a request corre.
func TestConnectPanelBusyStageIsModal(t *testing.T) {
	m, p := newConnPanelForTest(t)
	m.connect = p
	p.target = "nvidia"
	p.stage = connectStageBusy
	p.key = "meio-typed"
	// Enter e qualquer tecla sao ignorados enquanto verifica.
	if cmd := m.handleConnectKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("o stage busy nao deveria despachar nada")
	}
	if p.stage != connectStageBusy {
		t.Fatalf("o stage mudou sozinho: %v", p.stage)
	}
	// Ctrl+C fecha.
	m.handleConnectKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if m.connect != nil {
		t.Fatal("Ctrl+C deveria fechar o painel")
	}
}

// Sem servico, /connect recusa em vez de fingir funcionar.
func TestConnectPanelRefusesWithoutService(t *testing.T) {
	m := &tuiModel{hoff: map[panelID]int{}}
	if cmd := m.handleCommand("/connect", command{kind: cmdConnect}); cmd != nil {
		t.Fatal("nao deveria despachar nada")
	}
	if m.connect != nil {
		t.Fatal("o painel nao deveria ter aberto sem store")
	}
	if !strings.Contains(m.banner, "indisponivel") {
		t.Fatalf("o banner deveria explicar: %q", m.banner)
	}
}
