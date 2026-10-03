package main

import (
	"strings"
	"testing"
)

// panelText strips the TUI frame so a wrapped line can be reassembled: the box
// borders sit between the two panel columns, and a line that wraps lands right
// against the session panel's right border. Without removing them, the last
// fragment of a wrapped line is always "cut" — which is a test artefact, not a
// projection bug. (This cost a debugging round on the Inspiron smoke.)
func panelText(view string) string {
	r := strings.NewReplacer(
		" ", "", "\n", "", "\t", "",
		"│", "", "┌", "", "┐", "", "└", "", "┘", "", "├", "", "┤", "", "┬", "", "┴", "",
		"─", "", "░", "", "█", "", "▁", "",
	)
	return r.Replace(view)
}

// Reproduz o achado do smoke: no pty do Inspiron o fim do prompt ("> ...
// incluindo modelo e tamanho") nao aparecia, embora a projecao pura
// (TestPanelLinesWrapWithoutLosingContent) estivesse correta.
func TestViewShowsTheWholeLongPrompt(t *testing.T) {
	m := mkModel(t, nil)
	m.height = 30
	long := "verifique o hardware deste computador e me informe, com detalhe: " +
		"chipset, processador, memoria ram, swap, e armazenamento, incluindo modelo e tamanho"
	m.appendSession("> " + long)

	m.syncViews()
	if len(m.sessionView) < 2 {
		t.Fatalf("expected the prompt to wrap into several rows, got %v", m.sessionView)
	}
	flat := panelText(m.View())
	for _, want := range []string{"chipset", "armazenamento", "incluindomodeloe", "tamanho"} {
		if !strings.Contains(flat, want) {
			t.Errorf("the view is missing %q — the prompt tail was cut", want)
		}
	}
}

// O mesmo para a RESPOSTA longa, que era o caso reportado pelo operador.
func TestViewShowsTheWholeLongAnswer(t *testing.T) {
	m := mkModel(t, nil)
	m.height = 30
	answer := "Processador: Intel Core 2 Duo. " +
		"Memoria: 4 GB DDR2. Swap: 1 GB em disco. " +
		"Armazenamento: 250 GB SATA com 180 GB livres, montado em /dev/sda1."
	m.appendSession("< " + answer)
	m.syncViews()
	flat := panelText(m.View())
	for _, want := range []string{"Processador:", "Memoria:", "Swap:", "Armazenamento:", "montadoem/dev/sda1."} {
		if !strings.Contains(flat, want) {
			t.Errorf("the view is missing %q — the answer tail was cut", want)
		}
	}
}

// Regressão: View() é chamado pelo renderizador ~10x/s. Ele.prefixava a linha do
// spinner em m.activity e devolvia o slice ao modelo — a cada frame entrava uma
// linha nova e a mais antiga (o evento real) era descartada. Durante um turno o
// log de atividade ficava só com spinner.
func TestViewDoesNotMutateTheActivityLog(t *testing.T) {
	m := mkModel(t, nil)
	m.inTurn = true
	m.activity = []string{"* tool: shell ok 0.10s", "* modelo: recebendo tokens..."}
	before := append([]string(nil), m.activity...)

	for i := 0; i < 40; i++ { // ~4 segundos a 10 fps
		m.View()
	}

	if len(m.activity) != len(before) {
		t.Fatalf("View must not grow the activity log: %d -> %d (%v)",
			len(before), len(m.activity), m.activity)
	}
	for i := range before {
		if m.activity[i] != before[i] {
			t.Fatalf("View must not rewrite the activity log: line %d = %q, want %q",
				i, m.activity[i], before[i])
		}
	}
	// E o painel ainda mostra o spinner: a animação é de exibição, não de estado.
	if !strings.Contains(panelText(m.View()), "processando") {
		t.Error("the spinner must still be visible while the turn runs")
	}
}

// Regressão: sanitize comia o `\n` (é um controle C0), colapsando saída
// multilinha de tool — e qualquer resposta com parágrafos — numa linha gigante.
// A estrutura de linhas é dado, não sequência de escape.
func TestSanitizePreservesNewlinesButStillStripsEscapes(t *testing.T) {
	in := "linha um\nlinha dois\nlinha tres"
	out := sanitize(in)
	if n := strings.Count(out, "\n"); n != 2 {
		t.Errorf("sanitize must keep the line structure, got %d newlines in %q", n, out)
	}
	// E continua removendo o que é perigoso.
	evil := sanitize("a\n\x1b[2J\x1b[31mb\x07c")
	if strings.ContainsRune(evil, 0x1b) || strings.ContainsRune(evil, 0x07) {
		t.Errorf("sanitize must still strip escapes and BEL: %q", evil)
	}
	if strings.Count(evil, "\n") != 1 {
		t.Errorf("the safe newline must survive: %q", evil)
	}
}

// A saída multilinha de uma tool precisa aparecer linha a linha, não como uma
// linha só (que aí viraria scroll lateral inútil).
func TestMultiLineToolOutputKeepsOneRowPerLine(t *testing.T) {
	m := mkModel(t, nil)
	m.height = 24
	m.appendSession("< " + "erro: erro1\naviso: aviso2\nok: linha3\nfim: linha4")
	m.syncViews()
	if len(m.sessionView) < 4 {
		t.Fatalf("expected one physical row per output line, got %v", m.sessionView)
	}
	flat := panelText(m.View())
	for _, want := range []string{"erro:erro1", "aviso:aviso2", "ok:linha3", "fim:linha4"} {
		if !strings.Contains(flat, want) {
			t.Errorf("missing output line %q — the output was collapsed", want)
		}
	}
}

// Uma linha lógica longa o suficiente para ocupar a tela inteira tem de ser rolável
// até o FIM: o operador precisa ler a última linha, não só o começo.
func TestLongAnswerTailIsReachableByScrolling(t *testing.T) {
	m := mkModel(t, nil)
	m.height = 12
	m.focus = panelSession
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("linha ")
		b.WriteString(strings.Repeat("x", 5))
		b.WriteString(itoa(i))
		b.WriteString(" do relatorio\n")
	}
	m.appendSession("< " + b.String())
	m.syncViews()

	// Desce até o fim.
	m.panelToBottom()
	m.syncViews()
	flat := panelText(m.View())
	tail := flat
	if len(tail) > 160 {
		tail = tail[len(tail)-160:]
	}
	if !strings.Contains(flat, "linhaxxxxx39dorelatorio") {
		t.Errorf("scrolling to the bottom must expose the last row, got tail %q", tail)
	}
}
