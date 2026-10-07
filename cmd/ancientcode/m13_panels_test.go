package main

import (
	"strings"
	"testing"
)

// contWidth is the width of the continuation indent in cells.
const contWidth = len(contMark)

// M13 — painel de sessão e de atividade legíveis por inteiro. A falha medida no
// Inspiron era dupla: `runeCap` no paint e `truncatedLine`/`lineCap` no append.
// Estes testes cobrem a projeção compartilhada (`panelLines`), a geometria 70/30
// e a rolagem horizontal.

// unwrapPanelLines inverte a projeção: remove o marcador da primeira linha física
// e a indentação das continuações. Se `panelLines` perder um único caractere, o
// resultado difere do corpo original.
func unwrapPanelLines(physical string) string {
	lines := strings.Split(physical, "\n")
	var b strings.Builder
	for i, l := range lines {
		if i == 0 {
			l = strings.TrimPrefix(strings.TrimPrefix(l, "> "), "< ")
		} else {
			l = strings.TrimPrefix(l, contMark)
		}
		b.WriteString(l)
	}
	return b.String()
}

// Uma linha lógica longa precisa quebrar em N linhas físicas sem perder texto.
func TestPanelLinesWrapWithoutLosingContent(t *testing.T) {
	const width = 20
	logical := strings.Repeat("a", 400)
	got := panelLines([]string{logical}, width)
	// 1 linha cheia (width) + o resto em continuação de (width - indent) células.
	avail := width - contWidth
	want := 1 + (400-width+avail-1)/avail
	if len(got) != want {
		t.Fatalf("400 cells at width %d: got %d physical lines, want %d", width, len(got), want)
	}
	for i, l := range got {
		if n := len([]rune(l)); n > width {
			t.Errorf("line %d exceeds width: %d > %d", i, n, width)
		}
	}
	// Nenhum caractere perdido: tirando a indentação das continuações, a
	// concatenação devolve exatamente a linha lógica (a indentação é o preço da
	// legibilidade, não um ganho de conteúdo).
	if back := unwrapPanelLines(strings.Join(got, "\n")); back != logical {
		t.Errorf("content lost: %d runes in, %d out", len([]rune(logical)), len([]rune(back)))
	}
}

// O prefixo semântico ("> " do prompt, "< " da resposta) sobrevive à quebra e a
// continuação fica indentada sob o texto, não sob o marcador.
func TestPanelLinesKeepsPrefixAndIndentsContinuation(t *testing.T) {
	body := strings.Repeat("x", 30)
	logical := "> " + body
	got := panelLines([]string{logical}, 10)
	if !strings.HasPrefix(got[0], "> ") {
		t.Errorf("first line must keep the > marker, got %q", got[0])
	}
	if len(got) < 2 {
		t.Fatalf("expected a wrapped line, got %d", len(got))
	}
	if !strings.HasPrefix(got[1], contMark) {
		t.Errorf("continuation must be indented, got %q", got[1])
	}
	if strings.HasPrefix(got[1], "> ") || strings.HasPrefix(got[1], "< ") {
		t.Errorf("continuation must not repeat the marker, got %q", got[1])
	}
	// E o texto continua inteiro: tirando o marcador e as indentações.
	if back := unwrapPanelLines(strings.Join(got, "\n")); back != body {
		t.Errorf("prefixed content lost: in %q, out %q", body, back)
	}
}

// Uma resposta multilinha: cada \n é uma linha lógica nova, e a quebra de cada
// uma é independente.
func TestPanelLinesPreservesEmbeddedNewlines(t *testing.T) {
	got := panelLines([]string{"a\nb\nc"}, 10)
	if len(got) != 3 {
		t.Fatalf("expected one physical line per logical line, got %d: %q", len(got), got)
	}
}

// Sem marcador, a continuação também é indentada — é o que distingue "continuação
// da linha anterior" de "linha nova".
func TestPanelLinesIndentsPlainContinuation(t *testing.T) {
	got := panelLines([]string{strings.Repeat("y", 25)}, 10)
	if len(got) < 2 {
		t.Fatalf("expected a wrap, got %v", got)
	}
	if !strings.HasPrefix(got[1], contMark) {
		t.Errorf("plain continuation must be indented, got %q", got[1])
	}
}

// Larguras degeneradas não podem gerar panic nem laço infinito.
func TestPanelLinesHandlesDegenerateWidths(t *testing.T) {
	for _, w := range []int{0, -1, 1, 2} {
		got := panelLines([]string{"abc\ndef"}, w)
		if len(got) == 0 {
			t.Errorf("width %d produced no lines", w)
		}
	}
}

// A largura efetiva é a largura do CORPO do painel (largura interna menos a
// coluna do scrollbar). Integrar isso errado reintroduz o corte.
func TestPanelLinesFitsThePanelBody(t *testing.T) {
	_, actInner := panelInnerWidths(100)
	body := actInner - 1 // menos a coluna do scrollbar
	logical := strings.Repeat("z", body*3)
	got := panelLines([]string{logical}, body)
	for i, l := range got {
		if n := len([]rune(l)); n > body {
			t.Errorf("line %d is %d cells, body is %d", i, n, body)
		}
	}
	// 3 corpos com indentação de continuação: uma linha cheia + as seguintes
	// mais estreitas, então 3 corpos rendem 4 linhas físicas. O que importa é
	// que nada se perca e nenhuma linha estoure o corpo.
	if len(got) != 4 {
		t.Errorf("expected 4 physical lines, got %d: %v", len(got), got)
	}
	if back := unwrapPanelLines(strings.Join(got, "\n")); back != logical {
		t.Errorf("content lost: %d runes in, %d out", len([]rune(logical)), len([]rune(back)))
	}
}

// Geometria 70/30 com soma exata e piso de ~28 células para a atividade (que
// precisa de largura para o catálogo do M12 e o detalhe do gate não virarem pilha).
func TestPanelWidths70_30WithFloor(t *testing.T) {
	const minActivity = 28
	for _, w := range []int{40, 60, 80, 100, 120, 200, 240} {
		sess, act := panelWidths(w)
		if sess+act != w {
			t.Errorf("width %d: allocations must sum exactly, got %d+%d", w, sess, act)
		}
		if sess < 1 || act < 1 {
			t.Errorf("width %d: degenerate allocation %d+%d", w, sess, act)
		}
		if w >= 2*minActivity && act < minActivity {
			t.Errorf("width %d: activity %d is below the %d-cell floor", w, act, minActivity)
		}
	}
}

// A atividade é a coluna estreita: 30% em um terminal largo, não 40%.
func TestPanelWidthsGiveActivityThirtyPercent(t *testing.T) {
	sess, act := panelWidths(100)
	if act != 30 || sess != 70 {
		t.Errorf("width 100 should split 70/30, got %d/%d", sess, act)
	}
	// E a sessão precisa ser visivelmente mais larga que a atividade.
	s2, a2 := panelWidths(160)
	if s2 <= a2 {
		t.Errorf("session %d must be wider than activity %d", s2, a2)
	}
}

// Rolagem horizontal: a janela é clampada e alcança o fim da linha.
func TestHscrollRangeClampsAndReachesTheEnd(t *testing.T) {
	const total, width = 200, 20
	lo, hi := hscrollRange(total, width, 0)
	if lo != 0 || hi != width {
		t.Errorf("offset 0: got [%d,%d], want [0,%d]", lo, hi, width)
	}
	// Muito além do fim: clampado no máximo possível.
	_, hi = hscrollRange(total, width, 1<<30)
	if hi != total {
		t.Errorf("max scroll must expose the last cell, got hi=%d want %d", hi, total)
	}
	// Negativo: clampado em 0.
	lo, _ = hscrollRange(total, width, -50)
	if lo != 0 {
		t.Errorf("negative offset must clamp to 0, got %d", lo)
	}
	// Conteúdo que cabe: a janela cobre tudo, sem deslocamento.
	if lo, hi = hscrollRange(5, 20, 0); lo != 0 || hi != 5 {
		t.Errorf("short content: got [%d,%d], want [0,5]", lo, hi)
	}
}

// O spinner avança e nunca fica parado: um frame parado seria pior que nenhum.
func TestSpinnerFramesAdvance(t *testing.T) {
	a, b := spinnerFrame(0, false), spinnerFrame(1, false)
	if a == b {
		t.Errorf("consecutive Braille frames must differ, both %q", a)
	}
	if a == ' ' || b == ' ' {
		t.Error("spinner frame must never be blank")
	}
	// Degradação ASCII é um subconjunto de 4 glyphs distintos e sempre visível.
	seen := map[rune]bool{}
	for i := 0; i < 4; i++ {
		f := spinnerFrame(i, true)
		if f == ' ' || f == 0 {
			t.Fatalf("ASCII frame %d is not visible: %q", i, f)
		}
		seen[f] = true
	}
	if len(seen) != 4 {
		t.Errorf("ASCII spinner must cycle 4 distinct glyphs, got %d", len(seen))
	}
	// O índice é cíclico: nunca sai da tabela.
	if spinnerFrame(99, false) == 0 {
		t.Error("frame index must wrap, not vanish")
	}
}

// O frame é literal do código: nenhum payload pode entrar nele.
func TestSpinnerNeverContainsPayload(t *testing.T) {
	for i := 0; i < 12; i++ {
		for _, ascii := range []bool{false, true} {
			if n := len([]rune(string(spinnerFrame(i, ascii)))); n != 1 {
				t.Fatalf("frame %d is %d runes: must be a single glyph", i, n)
			}
		}
	}
}
