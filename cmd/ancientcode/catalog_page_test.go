package main

import (
	"strings"
	"testing"

	"github.com/antonioghizidev/ancientcode/internal/catalog"
	"github.com/antonioghizidev/ancientcode/internal/provider"
)

// --- o filtro de adequacao fica LIGADO por padrao ---------------------------

// Sem isto o catalogo e um despejo: `var o catalogOpts` deixava agentViable
// falso e os 469 modelos apareciam, 395 dos quais servem.
func TestModelsFilterIsOnByDefault(t *testing.T) {
	for _, in := range []string{"/models", "/models --free", "/models llama", "/models --next"} {
		c, ok := parseCommand(in)
		if !ok || c.kind != cmdModels {
			t.Fatalf("%s nao foi reconhecido como /models", in)
		}
		if !c.opts.agentViable {
			t.Fatalf("%s: o filtro de adequacao deveria estar ligado por padrao", in)
		}
	}
}

// --viable e o caminho de volta: quem quer ver tudo, digita.
func TestModelsViableFlagTurnsFilterOff(t *testing.T) {
	c, ok := parseCommand("/models --viable")
	if !ok || c.kind != cmdModels {
		t.Fatal("/models --viable nao reconhecido")
	}
	if c.opts.agentViable {
		t.Fatal("--viable deveria DESLIGAR o filtro, nao liga-lo")
	}
}

// --- paginacao: os tokens sao lidos -----------------------------------------

func TestModelsPageTokensParsed(t *testing.T) {
	c, _ := parseCommand("/models --next")
	if c.opts.page != -1 || !c.opts.pageSet {
		t.Fatalf("--next: page=%d pageSet=%v", c.opts.page, c.opts.pageSet)
	}
	c, _ = parseCommand("/models --prev")
	if c.opts.page != -2 || !c.opts.pageSet {
		t.Fatalf("--prev: page=%d pageSet=%v", c.opts.page, c.opts.pageSet)
	}
	c, _ = parseCommand("/models")
	if c.opts.pageSet {
		t.Fatal("/models sem --next nao deveria marcar paginacao")
	}
	// Paginar e filtrar sao coisas distintas: --next nao pode virar query.
	c, _ = parseCommand("/models --next")
	if c.opts.query != "" {
		t.Fatalf("--next virou query %q", c.opts.query)
	}
}

// Um token desconhecido continua sendo busca: uma opacao que nunca existiu nao
// pode falhar em silencio numa lista vazia.
func TestModelsUnknownTokenStillSearches(t *testing.T) {
	c, _ := parseCommand("/models --inexistente claude")
	if c.opts.query != "--inexistente claude" {
		t.Fatalf("query = %q; um token desconhecido deveria ser buscado", c.opts.query)
	}
}

// --- o estado da pagina sobrevive entre comandos ----------------------------

// catalogPage precisa ser de escopo de PACOTE. Declarada dentro do laco de
// comandos ela e reinicializada a cada iteracao e --next nao faz NADA — foi o
// que o smoke do Inspiron mostrou.
func TestCatalogPageStateSurvivesCommands(t *testing.T) {
	entries := manyEntries(120)

	// "renderiza" a pagina 1, como o REPL faz.
	_, _, pg := catalogLines(entries, catalogOpts{agentViable: true}, "", 1)
	if pg.Number != 1 {
		t.Fatalf("primeira renderizacao = pagina %d", pg.Number)
	}
	catalogPage.page, catalogPage.cur = 1, pg.Number

	// --next precisa resolver pela FUNCAO DE PRODUCAO. Um teste anterior
	// recalculava cur+1 dentro do proprio teste e por isso nao cobria a
	// resolucao: uma mutacao que fixava --next na pagina 1 passava verde.
	next := resolveCatalogPage(catalogOpts{page: -1, pageSet: true}, catalogPage.cur)
	if next != 2 {
		t.Fatalf("--next resolveu para %d", next)
	}
	_, _, pg2 := catalogLines(entries, catalogOpts{agentViable: true}, "", next)
	if pg2.Number != 2 {
		t.Fatalf("--next ficou na pagina %d", pg2.Number)
	}
	catalogPage.page, catalogPage.cur = next, pg2.Number

	prev := resolveCatalogPage(catalogOpts{page: -2, pageSet: true}, catalogPage.cur)
	if prev != 1 {
		t.Fatalf("--prev resolveu para %d", prev)
	}
}

// A resolucao de pagina, em si: --next avanca, --prev volta, e tanto um
// /models solto quanto uma busca nova voltam ao topo.
func TestResolveCatalogPage(t *testing.T) {
	next := catalogOpts{page: -1, pageSet: true}
	prev := catalogOpts{page: -2, pageSet: true}
	bare := catalogOpts{agentViable: true}

	if got := resolveCatalogPage(next, 3); got != 4 {
		t.Errorf("--next da pagina 3 = %d, esperava 4", got)
	}
	if got := resolveCatalogPage(prev, 3); got != 2 {
		t.Errorf("--prev da pagina 3 = %d, esperava 2", got)
	}
	// --prev na pagina 1 nao foge para zero ou negativo.
	if got := resolveCatalogPage(prev, 1); got != 1 {
		t.Errorf("--prev da pagina 1 = %d, esperava 1", got)
	}
	if got := resolveCatalogPage(prev, 0); got != 1 {
		t.Errorf("--prev sem pagina anterior = %d, esperava 1", got)
	}
	// Um /models solto ou uma busca nova comeca do topo, mesmo estando na 7.
	if got := resolveCatalogPage(bare, 7); got != 1 {
		t.Errorf("/models solto = pagina %d, esperava 1", got)
	}
	// Busca nova: o operador estava na pagina 7 e digita um termo.
	//
	// O caso TEM pageSet=true de proposito. `/models claude` chega com
	// pageSet=false, e ai o ramo `!pageSet` ja resolve para 1 — o que fazia o
	// teste passar pelo motivo errado e deixava a clausula `query != ""` sem
	// cobertura real. Com pageSet=true so a clausula da busca pode resolver.
	if got := resolveCatalogPage(catalogOpts{query: "claude", pageSet: true}, 7); got != 1 {
		t.Errorf("busca nova com pageSet = pagina %d, esperava 1", got)
	}
	// E paginar sem buscar continua avancando, mesmo com pageSet=true.
	if got := resolveCatalogPage(catalogOpts{pageSet: true}, 7); got != 7 {
		t.Errorf("pageSet sem --next e sem busca = pagina %d, esperava 7 (preservar)", got)
	}
}

// --- a numeracao mostrada e a que /model aceita -----------------------------

// O numero impresso e o indice na lista filtrada COMPLETA, nao na pagina: e por
// ele que /model <numero> seleciona.
func TestCatalogRowNumbersAreGlobalNotPageLocal(t *testing.T) {
	entries := manyEntries(120)
	lines, visible, pg := catalogLines(entries, catalogOpts{agentViable: true}, "", 2)
	if len(visible) != 120 {
		t.Fatalf("visivel = %d", len(visible))
	}
	if pg.Number != 2 {
		t.Fatalf("pagina %d", pg.Number)
	}
	var nums []int
	for _, l := range lines {
		if !isCatalogRow(l) {
			continue // linhas de rodape nao tem numero de modelo
		}
		nums = append(nums, rowNumber(t, l))
	}
	if len(nums) != catalog.DefaultPageSize {
		t.Fatalf("pagina 2 com %d linhas", len(nums))
	}
	// A pagina 2 comeca em 41 e termina em 80, com numeracao global.
	if nums[0] != 41 || nums[len(nums)-1] != 80 {
		t.Fatalf("numeracao da pagina 2 = %d..%d, esperava 41..80", nums[0], nums[len(nums)-1])
	}
}

// A linha do modelo ativo leva "*" no inicio; o numero continua legivel. Sem
// isto o operador nao consegue ler o numero da linha 1.
func TestActiveRowNumberIsStillReadable(t *testing.T) {
	all := manyEntries(3)
	lines, _, _ := catalogLines(all, catalogOpts{agentViable: true}, all[0].Info.ID, 1)
	found := false
	for _, l := range lines {
		if rowNumberOpt(l, all[0].Info.ID) {
			found = true
		}
	}
	if !found {
		t.Fatal("a linha do modelo ativo nao expoe o numero")
	}
}

// isCatalogRow reports whether a rendered line is a numbered model row rather
// than a footer line ("pagina 2 de 3 ...", "selecione com /model <numero>").
func isCatalogRow(line string) bool {
	i := strings.Index(line, ".")
	if i <= 0 {
		return false
	}
	for _, r := range strings.TrimLeft(line[:i], " *") {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(strings.TrimLeft(line[:i], " *")) > 0
}

// rowNumber extracts N from a catalog row, tolerating the active marker.
func rowNumber(t *testing.T, line string) int {
	t.Helper()
	i := strings.Index(line, ".")
	if i <= 0 {
		t.Fatalf("linha sem numero: %q", line)
	}
	rest := strings.TrimLeft(line[:i], " *")
	n := 0
	for _, r := range rest {
		if r < '0' || r > '9' {
			t.Fatalf("numero unexpectedo em %q", line)
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// rowNumberOpt reports whether line is the row for the given active id.
func rowNumberOpt(line, id string) bool {
	return strings.Contains(line, id)
}

func manyEntries(n int) []catalog.Entry {
	out := make([]catalog.Entry, n)
	for i := range out {
		out[i] = catalog.Entry{
			Info: provider.ModelInfo{
				ID:                  "org/modelo-" + strings.Repeat("0", 3-len(itoaTest(i))) + itoaTest(i),
				ContextLength:       128000,
				SupportedParameters: []string{"tools"},
				PricingPrompt:       "0.000001",
				PricingCompletion:   "0.000001",
			},
		}
	}
	return out
}

func itoaTest(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
