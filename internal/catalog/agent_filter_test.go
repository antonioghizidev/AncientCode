package catalog

import (
	"fmt"
	"strings"
	"testing"

	"github.com/antonioghizidev/ancientcode/internal/provider"
)

// entry builds a catalog entry with the metadata OpenRouter actually publishes.
func entry(id string, ctx int, params ...string) Entry {
	p := ""
	if len(params) == 0 {
		p = "0.000001" // priced, not free
	}
	return Entry{
		Info: provider.ModelInfo{
			ID:                  id,
			ContextLength:       ctx,
			SupportedParameters: params,
			PricingPrompt:       p,
			PricingCompletion:   p,
		},
		Tools: ToolsUnknown,
	}
}

// --- o filtro usa metadata, nao palpite -------------------------------------

func TestAgentViableDropsModelsWithoutDisclosedTools(t *testing.T) {
	all := []Entry{
		entry("com/tools", 128000, "tools", "temperature"),
		entry("sem/tools", 128000, "temperature", "top_p"), // provider SAID what it supports
	}
	got := Filter(all, Options{AgentViable: true})
	if len(got) != 1 || got[0].Info.ID != "com/tools" {
		t.Fatalf("filtro de tools falhou: %v", ids(got))
	}
}

// Ausencia de metadata NAO e evidencia contra o modelo: a NIM nao divulga nada,
// e descartar todos os seus modelos deixaria o operador sem provedor gratuito.
func TestAgentViableKeepsModelsWithNoMetadataAtAll(t *testing.T) {
	all := []Entry{
		entry("nim/sem-metadata", 128000), // SupportedParameters vazio
	}
	got := Filter(all, Options{AgentViable: true})
	if len(got) != 1 {
		t.Fatalf("um provider sem metadata foi descartado: %v", ids(got))
	}
	if got[0].Tools != ToolsUnknown {
		t.Fatal("o estado de tools foi alterado; tem de continuar desconhecido")
	}
}

// ToolsNo explicito (verificado por --check) e desqualificador.
func TestAgentViableDropsVerifiedNoTools(t *testing.T) {
	e := entry("m/1", 128000, "tools")
	e.Tools = ToolsNo
	got := Filter([]Entry{e}, Options{AgentViable: true})
	if len(got) != 0 {
		t.Fatalf("modelo verificado SEM tools sobreviveu: %v", ids(got))
	}
}

// Contexto publicado e pequeno demais: desqualifica. Contexto ausente, nao.
func TestAgentViableContextFloor(t *testing.T) {
	all := []Entry{
		entry("grande", 200000, "tools"),
		entry("pequeno", 4095, "tools"),
		entry("nao-declarado", 0, "tools"),
	}
	got := ids(Filter(all, Options{AgentViable: true}))
	for _, want := range []string{"grande", "nao-declarado"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s deveria ter sobrevivido: %v", want, got)
		}
	}
	for _, g := range got {
		if g == "pequeno" {
			t.Fatalf("contexto de 4095 tokens sobreviveu: %v", got)
		}
	}
}

// O piso de contexto e ajustevel, e o padrao tem um motivo declarado.
func TestAgentContextFloorIsAdjustable(t *testing.T) {
	e := entry("m", 16384, "tools")
	if len(Filter([]Entry{e}, Options{AgentViable: true})) != 0 {
		t.Fatal("16k deveria ficar abaixo do piso padrao")
	}
	if len(Filter([]Entry{e}, Options{AgentViable: true, MinContext: 8192})) != 1 {
		t.Fatal("com o piso em 8k, 16k deveria passar")
	}
	if MinAgentContext < 32000 {
		t.Fatalf("o piso padrao caiu para %d; o loop manda ~31KB por turno", MinAgentContext)
	}
}

// --- a ordenacao -------------------------------------------------------------

// O sinal mais forte, entre os que o provider publica, e gratuito: medido,
// apenas 16 dos 469 modelos da OpenRouter tem preco zero.
//
// Os IDs sao escolhidos para que o ALFABETICO favoreca o PAGO. Numa versao
// anterior desta tabela o gratuito vencia pelo nome ("livre" < "pago"), e o
// teste continuava verde depois de remover a regra de preco — ou seja, ele
// passava pelo motivo errado e nao cobria nada.
func TestAgentRankPutsFreeFirst(t *testing.T) {
	pago := entry("aaa/pago", 128000, "tools")
	pago.Info.PricingPrompt, pago.Info.PricingCompletion = "0.00001", "0.00002"
	livre := entry("zzz/livre", 128000, "tools")
	livre.Info.PricingPrompt, livre.Info.PricingCompletion = "0", "0"

	if !strings.HasPrefix(pago.Info.ID, "a") || !strings.HasPrefix(livre.Info.ID, "z") {
		t.Fatal("a tabela perdeu a premissa: o pago precisa ordenar antes por nome")
	}
	got := ids(AgentRank([]Entry{pago, livre}, ""))
	if got[0] != "zzz/livre" {
		t.Fatalf("o gratuito deveria ir primeiro: %v", got)
	}
}

// O mesmo teste visto pelo outro lado: se a regra de preco sumir, o resultado
// tem que mudar. Sem esta assimetria o teste acima passa por acidente.
func TestFreeOrderingIsActuallyLoadBearing(t *testing.T) {
	// Mesmos dois modelos, porem AMBOS pagos: a regra de preco nao decide nada,
	// e o alfabetico manda.
	a := entry("aaa/x", 128000, "tools")
	b := entry("zzz/y", 128000, "tools")
	a.Info.PricingPrompt, a.Info.PricingCompletion = "0.001", "0.002"
	b.Info.PricingPrompt, b.Info.PricingCompletion = "0.003", "0.004"
	got := ids(AgentRank([]Entry{a, b}, ""))
	if got[0] != "aaa/x" {
		t.Fatalf("sem diferenca de preco o alfabetico deveria vencer: %v", got)
	}
}

// Tool calling revelado pelo provider pesa mais que inferido pelo nome.
func TestAgentRankPrefersDisclosedToolSupport(t *testing.T) {
	disclosed := entry("d/tem", 128000, "tools")
	inferred := entry("i/sem", 128000) // sem metadata: ToolsUnknown
	got := ids(AgentRank([]Entry{inferred, disclosed}, ""))
	if got[0] != "d/tem" {
		t.Fatalf("toolsupport desconhecido nao deveria ir antes do revelado: %v", got)
	}
}

// O modelo ativo fica no topo, sempre.
func TestAgentRankKeepsActiveOnTop(t *testing.T) {
	all := []Entry{entry("a", 128000, "tools"), entry("b", 128000, "tools"), entry("c", 128000, "tools")}
	got := ids(AgentRank(all, "c"))
	if got[0] != "c" {
		t.Fatalf("o modelo ativo nao ficou no topo: %v", got)
	}
}

// A ordem e estavel entre execucoes: uma lista que embaralha a cada /models
// impede o operador de aprender onde as coisas estão.
func TestAgentRankIsStable(t *testing.T) {
	all := []Entry{
		entry("z", 128000, "tools"),
		entry("a", 128000, "tools"),
		entry("m", 128000, "tools"),
	}
	first := strings.Join(ids(AgentRank(all, "")), ",")
	for i := 0; i < 5; i++ {
		if got := strings.Join(ids(AgentRank(all, "")), ","); got != first {
			t.Fatalf("a ordem variou entre execucoes:\n%s\n%s", first, got)
		}
	}
}

// --- paginacao ---------------------------------------------------------------

func TestPaginateDefaultsToForty(t *testing.T) {
	all := make([]Entry, 395)
	for i := range all {
		all[i] = entry(string(rune('a'+i%26))+"/m", 128000, "tools")
	}
	pg := Paginate(all, 1, 0)
	if len(pg.Entries) != DefaultPageSize {
		t.Fatalf("pagina 1 com %d entradas, esperava %d", len(pg.Entries), DefaultPageSize)
	}
	if DefaultPageSize != 40 {
		t.Fatalf("o tamanho de pagina mudou para %d", DefaultPageSize)
	}
	if pg.TotalPages != 10 {
		t.Fatalf("395/40 = 10 paginas, veio %d", pg.TotalPages)
	}
	if pg.Total != 395 {
		t.Fatalf("Total = %d", pg.Total)
	}
}

// Toda pagina tem o tamanho certo e as paginas nao se sobrepoem nem perdem
// modelos: e isso que torna a paginacao uma forma de acesso, e nao um truque.
func TestPaginateCoversEverythingExactlyOnce(t *testing.T) {
	// IDs inequivocos: minha primeira versao derivava o id de i/26 e i%26 e
	// colidia, o que acusava o Paginate de duplicar entradas.
	all := make([]Entry, 95)
	for i := range all {
		all[i] = Entry{Info: provider.ModelInfo{ID: fmt.Sprintf("org/modelo-%03d", i)}}
	}
	seen := map[string]int{}
	// Iterate over the REAL page count. 95 items at 40/page is 3 pages, and
	// asking for a 4th clamps back onto page 3 — so looping to 4 counted 15
	// models twice and looked like Paginate was duplicating entries.
	total := Paginate(all, 1, 40).TotalPages
	for p := 1; p <= total; p++ {
		pg := Paginate(all, p, 40)
		for _, e := range pg.Entries {
			seen[e.Info.ID]++
		}
	}
	if len(seen) != 95 {
		t.Fatalf("a paginacao mostrou %d de 95 modelos", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("modelo %s apareceu %d vezes", id, n)
		}
	}
}

// Pagina fora da faixa prende na ultima, e nao devolve tela vazia: tela vazia
// le como "nada corresponde", que e uma afirmacao diferente e errada.
func TestPaginateClampsOutOfRange(t *testing.T) {
	all := []Entry{entry("a", 128000, "tools"), entry("b", 128000, "tools")}
	if pg := Paginate(all, 99, 40); pg.Number != 1 || len(pg.Entries) != 2 {
		t.Fatalf("pagina 99 deveria prender na 1: %+v", pg.Number)
	}
	if pg := Paginate(all, -5, 40); pg.Number != 1 {
		t.Fatalf("pagina negativa deveria virar 1: %d", pg.Number)
	}
	if pg := Paginate(nil, 1, 40); pg.Number != 0 || pg.Total != 0 {
		t.Fatalf("catalogo vazio deveria dar pagina 0: %+v", pg)
	}
}

func TestPageNavigationClamps(t *testing.T) {
	p := Page{Number: 1, TotalPages: 3}
	if p.Next() != 2 || p.Prev() != 1 {
		t.Fatal("navegacao basica errada")
	}
	last := Page{Number: 3, TotalPages: 3}
	if last.Next() != 3 {
		t.Fatal("Next na ultima deveria prender")
	}
	if !p.HasMore() || last.HasMore() {
		t.Fatal("HasMore errado")
	}
}

func ids(es []Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Info.ID
	}
	return out
}
