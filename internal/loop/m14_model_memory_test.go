package loop

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/antonioghizidev/ancientcode/internal/config"
	"github.com/antonioghizidev/ancientcode/internal/provider"
)

// memModelMemory is an in-memory ModelMemory for these tests.
type memModelMemory struct {
	mu      sync.Mutex
	models  map[string]string
	getErr  error
	setErr  error
	setCall int
}

func newMemModelMemory() *memModelMemory { return &memModelMemory{models: map[string]string{}} }

func (m *memModelMemory) GetModel(p string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return "", false, m.getErr
	}
	v, ok := m.models[p]
	return v, ok, nil
}

func (m *memModelMemory) SetModel(p, model string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setCall++
	if m.setErr != nil {
		return m.setErr
	}
	m.models[p] = model
	return nil
}

// fakeCtrl records every model and endpoint the session installed.
type fakeCtrl struct {
	mu         sync.Mutex
	model      string
	baseURL    string
	env        string
	modelCalls []string
	rejectSet  map[string]bool // models SetModel refuses
}

func (f *fakeCtrl) Chat(ctx context.Context, msgs []provider.ChatMessage, tools []provider.ToolDef, onEvent func(provider.StreamEvent)) (provider.ChatResult, error) {
	return provider.ChatResult{}, nil
}
func (f *fakeCtrl) Model() string { f.mu.Lock(); defer f.mu.Unlock(); return f.model }
func (f *fakeCtrl) BaseURL() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.baseURL
}
func (f *fakeCtrl) SetModel(m string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rejectSet[m] {
		return errors.New("provider: model nao encontrado")
	}
	f.modelCalls = append(f.modelCalls, m)
	f.model = m
	return nil
}
func (f *fakeCtrl) SetBaseURL(base, env string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.baseURL, f.env = base, env
	return nil
}

func newModelLoop(t *testing.T, mem ModelMemory, providerName string) (*Loop, *fakeCtrl, *[]Event) {
	t.Helper()
	fc := &fakeCtrl{model: "modelo-inicial", baseURL: "https://exemplo/v1", rejectSet: map[string]bool{}}
	evs := &[]Event{}
	cfg := &config.Config{
		Provider:  providerName,
		BaseURL:   "https://exemplo/v1",
		APIKeyEnv: "CHAVE_PADRAO",
	}
	l := New(fc, nil, cfg, nil, nil)
	if mem != nil {
		l.SetModelMemory(mem)
	}
	l.SetObserver(func(e Event) { *evs = append(*evs, e) })
	return l, fc, evs
}

// --- /provider precisa trocar o provider ATIVO -------------------------------

// O bug que motivou curProvider: cfg.Provider nunca muda, entao CurrentProvider
// mentia depois do primeiro /provider — e e o que /connect usa para escolher o
// alvo padrao e para dizer "use /provider X".
func TestCurrentProviderFollowsSwitch(t *testing.T) {
	l, _, _ := newModelLoop(t, nil, "openrouter")
	if got := l.CurrentProvider(); got != "openrouter" {
		t.Fatalf("provider inicial = %q", got)
	}
	if err := l.SwitchProvider("nvidia"); err != nil {
		t.Fatal(err)
	}
	if got := l.CurrentProvider(); got != "nvidia" {
		t.Fatalf("apos /provider nvidia, CurrentProvider = %q (o YAML nao muda)", got)
	}
	// E o nome da env segue o provider ativo, senao mandariamos o operador
	// exportar a variavel errada.
	if got := l.CurrentAPIKeyEnv(); got != "NVIDIA_API_KEY" {
		t.Fatalf("env do provider ativo = %q", got)
	}
}

// Maiusculas normalizam, como em /provider.
func TestCurrentProviderNormalizes(t *testing.T) {
	l, _, _ := newModelLoop(t, nil, "OpenRouter")
	if got := l.CurrentProvider(); got != "openrouter" {
		t.Fatalf("CurrentProvider nao normalizou: %q", got)
	}
}

// --- §3.7: /models persiste por provedor --------------------------------------

func TestSwitchModelPersistsPerProvider(t *testing.T) {
	mem := newMemModelMemory()
	l, fc, _ := newModelLoop(t, mem, "openrouter")
	if err := l.SwitchModel("openai/gpt-x"); err != nil {
		t.Fatal(err)
	}
	mem.mu.Lock()
	got := mem.models["openrouter"]
	mem.mu.Unlock()
	if got != "openai/gpt-x" {
		t.Fatalf("o modelo nao foi lembrado para openrouter: %q", got)
	}
	// Dois provedores, dois modelos, ambos sobrevivem.
	if err := l.SwitchProvider("nvidia"); err != nil {
		t.Fatal(err)
	}
	if err := l.SwitchModel("nvidia/nemotron"); err != nil {
		t.Fatal(err)
	}
	mem.mu.Lock()
	defer mem.mu.Unlock()
	if mem.models["openrouter"] != "openai/gpt-x" || mem.models["nvidia"] != "nvidia/nemotron" {
		t.Fatalf("estado por provedor incorreto: %v", mem.models)
	}
	_ = fc
}

// Trocar de provedor NAO pode sobrescrever o modelo salvo do outro.
func TestSwitchProviderDoesNotOverwriteOtherProvidersModel(t *testing.T) {
	mem := newMemModelMemory()
	l, _, _ := newModelLoop(t, mem, "openrouter")
	_ = l.SwitchModel("openai/gpt-x")
	mem.mu.Lock()
	before := mem.setCall
	mem.mu.Unlock()
	if err := l.SwitchProvider("nvidia"); err != nil {
		t.Fatal(err)
	}
	mem.mu.Lock()
	defer mem.mu.Unlock()
	if mem.setCall != before {
		t.Fatal("/provider gravou um modelo no lugar de outro")
	}
	if mem.models["openrouter"] != "openai/gpt-x" {
		t.Fatal("o modelo do openrouter foi sobrescrito")
	}
}

// --- §3.7: provedor ja usado -> restaura o modelo lembrado ------------------

func TestSwitchProviderRestoresRememberedModel(t *testing.T) {
	mem := newMemModelMemory()
	mem.models["nvidia"] = "nvidia/nemotron-lembrado"
	l, fc, evs := newModelLoop(t, mem, "openrouter")
	if err := l.SwitchProvider("nvidia"); err != nil {
		t.Fatal(err)
	}
	if fc.Model() != "nvidia/nemotron-lembrado" {
		t.Fatalf("o modelo lembrado nao foi restaurado: %q", fc.Model())
	}
	// E o operador e avisado, para nao achar magico.
	if !anyEventContains(*evs, "lembrado restaurado") {
		t.Fatalf("nenhum evento restore: %v", *evs)
	}
}

// --- §3.7: provedor nunca visto -> NAO escolhe modelo, orienta /models -------

// A propriedade central do §3.7. Escolher automaticamente seria o harness
// decidindo sozinho com qual modelo falar.
func TestSwitchProviderNeverSeenChoosesNoModel(t *testing.T) {
	mem := newMemModelMemory()
	l, fc, evs := newModelLoop(t, mem, "openrouter")
	before := fc.Model()
	if err := l.SwitchProvider("nvidia"); err != nil {
		t.Fatal(err)
	}
	if fc.Model() != before {
		t.Fatalf("o harness escolheu um modelo sozinho: %q -> %q", before, fc.Model())
	}
	// Nenhum SetModel foi chamado no provider novo.
	if len(fc.modelCalls) != 0 {
		t.Fatalf("SetModel foi chamado %v vezes", fc.modelCalls)
	}
	// E o operador recebe a instrucao.
	if !anyEventContains(*evs, "/models") {
		t.Fatalf("nao orientou /models: %v", *evs)
	}
}

// --- §3.7: provider recusa o modelo lembrado -> NAO escolhe outro -----------

// A segunda recusa do §3.7: nem aqui há fallback automático.
func TestSwitchProviderRejectedModelPicksNoOther(t *testing.T) {
	mem := newMemModelMemory()
	mem.models["nvidia"] = "nvidia/modelo-removido"
	l, fc, evs := newModelLoop(t, mem, "openrouter")
	fc.rejectSet["nvidia/modelo-removido"] = true
	before := fc.Model()
	if err := l.SwitchProvider("nvidia"); err != nil {
		t.Fatal(err)
	}
	if fc.Model() != before {
		t.Fatalf("o harness escolheu outro modelo apos recusa: %q -> %q", before, fc.Model())
	}
	if len(fc.modelCalls) != 0 {
		t.Fatalf("SetModel foi chamado mesmo apos recusa: %v", fc.modelCalls)
	}
	if !anyEventContains(*evs, "/models") {
		t.Fatalf("nao orientou /models apos recusa: %v", *evs)
	}
}

// O switch de provider JA TIHO SIDO aplicado quando a restauracao falha: o
// operador fica no provider novo, so sem o modelo — e avisado.
func TestSwitchProviderSucceedsEvenWhenRestoreFails(t *testing.T) {
	mem := newMemModelMemory()
	mem.getErr = errors.New("state.json corrompido")
	l, fc, evs := newModelLoop(t, mem, "openrouter")
	if err := l.SwitchProvider("nvidia"); err != nil {
		t.Fatalf("um state.json ilegivel nao pode falhar o switch: %v", err)
	}
	if fc.BaseURL() == "https://exemplo/v1" {
		t.Fatal("o endpoint nao trocou")
	}
	if !anyEventContains(*evs, "estado ilegivel") {
		t.Fatalf("nao reportou o estado ilegivel: %v", *evs)
	}
}

// Falha ao gravar o estado nao desfaz um switch que já deu certo: o operador
// ficaria com o modelo antigo depois de ver "modelo: X" na tela.
func TestSwitchModelSucceedsWhenPersistFails(t *testing.T) {
	mem := newMemModelMemory()
	mem.setErr = errors.New("disco cheio")
	l, fc, _ := newModelLoop(t, mem, "openrouter")
	if err := l.SwitchModel("openai/gpt-x"); err != nil {
		t.Fatalf("falha ao persistir nao pode falhar o switch: %v", err)
	}
	if fc.Model() != "openai/gpt-x" {
		t.Fatalf("o modelo nao trocou: %q", fc.Model())
	}
}

// Sem memoria (estado.json ausente/ilegivel na inicializacao) o comportamento e
// o de antes do M14: o modelo segue sendo so da sessao.
func TestNoModelMemoryMeansSessionOnly(t *testing.T) {
	l, fc, _ := newModelLoop(t, nil, "openrouter")
	_ = l.SwitchModel("openai/gpt-x")
	if err := l.SwitchProvider("nvidia"); err != nil {
		t.Fatal(err)
	}
	if fc.Model() != "openai/gpt-x" {
		t.Fatalf("sem memoria o modelo nao deveria mudar: %q", fc.Model())
	}
}

func anyEventContains(evs []Event, sub string) bool {
	for _, e := range evs {
		if strings.Contains(e.Detail, sub) {
			return true
		}
	}
	return false
}

var _ provider.Provider = (*fakeCtrl)(nil)
