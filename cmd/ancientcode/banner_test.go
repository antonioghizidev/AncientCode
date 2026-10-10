package main

import (
	"strings"
	"testing"

	"github.com/antonioghizidev/ancientcode/internal/config"
	"github.com/antonioghizidev/ancientcode/internal/loop"
)

// REGRESSAO do smoke do Inspiron: o banner anunciava
// `model=nvidia/nemotron-nano-3-30b-a3b` numa sessao que rodava
// `google/diffusiongemma-26b-a4b-it`, porque lia cfg.Model em vez do loop.
// A primeira linha de orientacao do operador contradizia a sessao.
//
// O bug nao tinha TESTE — o banner era um Fprintf inline em main(). Extrair
// para uma funcao pura e o que torna a propriedade verificavel.
func TestBannerReportsActiveModelNotConfigModel(t *testing.T) {
	mem := bannerMem{}
	mem["nvidia"] = "nvidia/modelo-restaurado"

	agent := newLoopForBanner(t, mem, "nvidia")
	// O loop precisa ter o modelo restaurado de fato, como na sessao real.
	agent.SetModelMemory(mem)
	if agent.CurrentModel() != "nvidia/modelo-restaurado" {
		t.Fatalf("pre-condicao: o loop nao restaurou o modelo (%q)", agent.CurrentModel())
	}

	out := banner("9.9.9-test", agent, "default.jsonl", "run", "")

	if strings.Contains(out, "nvidia/modelo-restaurado") == false {
		t.Fatalf("o banner nao mostra o modelo ATIVO:\n%s", out)
	}
	if strings.Contains(out, "nemotron-nano-3-30b-a3b") {
		t.Fatalf("o banner ainda mostra o modelo do YAML:\n%s", out)
	}
	// E anuncia que o modelo veio da memoria, para o operador nao se perguntar
	// por que difere do YAML.
	if !strings.Contains(out, "restaurado de state.json") {
		t.Fatalf("o banner nao avisou que o modelo veio da memoria:\n%s", out)
	}
}

// O mesmo para o provider: cfg.Provider nunca muda no /provider.
func TestBannerReportsActiveProvider(t *testing.T) {
	mem := bannerMem{}
	agent := newLoopForBanner(t, mem, "openrouter")
	if err := agent.SwitchProvider("nvidia"); err != nil {
		t.Fatal(err)
	}
	out := banner("9.9.9-test", agent, "default.jsonl", "plan", "")
	if !strings.Contains(out, "provider=nvidia") {
		t.Fatalf("o banner nao mostra o provider ATIVO apos /provider:\n%s", out)
	}
	if strings.Contains(out, "provider=openrouter") {
		t.Fatalf("o banner ainda mostra o provider do YAML:\n%s", out)
	}
}

// A linha extra de "restaurado" so aparece quando veio da memoria — nunca para
// um modelo que veio do config, senao vira ruido em toda sessao.
func TestBannerOnlySaysRestoredWhenItWasRestored(t *testing.T) {
	// Sem memoria instalada: nada de linha extra.
	agent := newLoopForBanner(t, nil, "nvidia")
	out := banner("9.9.9-test", agent, "default.jsonl", "run", "")
	if strings.Contains(out, "restaurado de state.json") {
		t.Fatalf("anunciou restauracao sem ter restaurado:\n%s", out)
	}
	// A memoria existe mas nao tem este provider.
	mem := bannerMem{}
	mem["openrouter"] = "outro/modelo"
	agent2 := newLoopForBanner(t, nil, "nvidia")
	agent2.SetModelMemory(mem)
	out2 := banner("9.9.9-test", agent2, "default.jsonl", "run", "")
	if strings.Contains(out2, "restaurado de state.json") {
		t.Fatalf("anunciou restauracao de outro provider:\n%s", out2)
	}
}

// O banner nao pode derrubar a sessao: um agente nil ainda produz texto.
func TestBannerIsTotal(t *testing.T) {
	out := banner("9.9.9-test", nil, "default.jsonl", "run", "")
	if !strings.Contains(out, "ancientcode 9.9.9-test") {
		t.Fatalf("banner com agente nil saiu vazio:\n%s", out)
	}
	if !strings.Contains(out, helpText) {
		t.Fatal("o banner precisa trazer a ajuda dos comandos")
	}
}

// --- helpers ---------------------------------------------------------------

// newLoopForBanner builds a loop whose provider is name, over the stub provider
// the package already uses for model switching (tui_picker_live_test.go), so
// CurrentModel/CurrentProvider/SwitchProvider behave like a real session.
func newLoopForBanner(t *testing.T, mem loop.ModelMemory, provider string) *loop.Loop {
	t.Helper()
	cfg := &config.Config{
		Provider:  provider,
		Model:     "nvidia/nemotron-nano-3-30b-a3b", // o que o YAML diz
		BaseURL:   "https://exemplo.invalido/v1",
		APIKeyEnv: "CHAVE",
	}
	return loop.New(&switchableCatalogProvider{model: cfg.Model}, nil, cfg, nil, nil)
}

// newLoopModelMemory is a ModelMemory that always answers from its map.
type bannerMem map[string]string

func (m bannerMem) GetModel(p string) (string, bool, error) {
	v, ok := m[p]
	return v, ok, nil
}

func (m bannerMem) SetModel(p, model string) error {
	m[p] = model
	return nil
}

// O banner precisa dizer ONDE o agente trabalha.
//
// Sem essa linha, `pwd` no shell e a pasta que as file tools confinam podem
// discordar em silêncio, e o operador edita um projeto acreditando estar em
// outro. E o banner é a única coisa vista antes do primeiro comando.
func TestBannerShowsTheProjectRoot(t *testing.T) {
	agent := newLoopForBanner(t, bannerMem{}, "openrouter")
	out := banner("9.9.9-test", agent, "default.jsonl", "run", "/home/USUARIO/workspace/PROJETO")
	if !strings.Contains(out, "/home/USUARIO/workspace/PROJETO") {
		t.Fatalf("o banner nao mostrou a raiz do projeto:\n%s", out)
	}
	// E sem raiz conhecida, não inventa uma: uma pasta inventada é pior que
	// nenhuma, porque o operador passaria a conferir a errada.
	out2 := banner("9.9.9-test", agent, "default.jsonl", "run", "")
	if strings.Contains(out2, "projeto:") {
		t.Fatalf("sem raiz conhecida, o banner nao deveria inventar uma:\n%s", out2)
	}
}
