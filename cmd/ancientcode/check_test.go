package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/antonioghizidev/ancientcode/internal/catalog"
	"github.com/antonioghizidev/ancientcode/internal/loop"
)

// The row must SHOW the verdict, not just rank by it. A verified entry the
// operator cannot see is a claim the UI never made visible.
func TestCatalogRowShowsFailed(t *testing.T) {
	rows := []struct {
		check catalog.Check
		want  string
	}{
		{catalog.CheckOK, "verificado"},
		{catalog.CheckFailed, "sem tool"},
	}
	base := manyEntries(1)[0].Info
	for _, tc := range rows {
		e := catalog.Entry{Info: base, Check: tc.check}
		got := catalogRow(0, e, "")
		if !strings.Contains(got, tc.want) {
			t.Errorf("Check=%v nao renderizou %q: %s", tc.check, tc.want, got)
		}
	}
	// Unverified must stay silent: printing "sem tool" for a model nobody
	// probed is exactly the lie this whole feature exists to avoid.
	e := catalog.Entry{Info: base}
	if strings.Contains(catalogRow(0, e, ""), "sem tool") {
		t.Errorf("nao verificado nao pode dizer 'sem tool': %s", catalogRow(0, e, ""))
	}
}

// The verdicts must survive the re-render. The REPL re-filters from the SOURCE
// slice after checking, and catalog.Filter returns copies — so writing the
// verdicts into the filtered slice loses them, and the rows come back without
// the "verificado" tag the operator just paid for. The Inspiron smoke showed
// exactly this: the outcome lines said "verificado", the table below did not.
func TestCheckVerdictsSurviveTheRerender(t *testing.T) {
	ag := &fakeChecker{probed: map[string]bool{}, fail: map[string]error{}}
	entries := manyEntries(120)
	opt := catalogOpts{agentViable: true}

	// Build the page EXACTLY as the REPL does — filter then rank, not filter
	// alone. A page built by a different path probes a different set of models,
	// which is how a test can pass while measuring the wrong thing.
	_, _, pg := catalogLines(entries, opt, "", 1)

	if _, err := runCheckPage(context.Background(), ag, entries, pg); err != nil {
		t.Fatal(err)
	}

	// The probed models must carry the verdict in the SOURCE slice. Look them up
	// by ID rather than reading pg.Entries: the page holds copies taken BEFORE
	// the probe, so it is expected — and correct — to still read CheckUnverified
	// there.
	for _, e := range pg.Entries {
		idx := indexOfEntry(entries, e.Info.ID)
		if idx < 0 {
			t.Fatalf("%s sumiu do slice de origem", e.Info.ID)
		}
		if entries[idx].Check != catalog.CheckOK {
			t.Fatalf("a entry %s nao foi marcada no slice de origem: Check=%v", e.Info.ID, entries[idx].Check)
		}
	}

	// ...and survive the re-render, with the tag visible on the row.
	_, visible, _ := catalogLines(entries, opt, "", 1)
	seen := map[string]bool{}
	for _, e := range visible {
		if !ag.probed[e.Info.ID] {
			continue
		}
		if e.Check != catalog.CheckOK {
			t.Fatalf("a entry %s perdeu o veredito no re-render: Check=%v", e.Info.ID, e.Check)
		}
		if !strings.Contains(catalogRow(0, e, ""), "verificado") {
			t.Fatalf("a row de %s nao mostra 'verificado': %s", e.Info.ID, catalogRow(0, e, ""))
		}
		seen[e.Info.ID] = true
	}
	if len(seen) == 0 {
		t.Fatal("nenhum modelo sondado apareceu no re-render")
	}
}

// --check must actually probe the VISIBLE page, not the whole catalog, and not
// silently skip. Before this, --check was a no-op that only asked for consent.
func TestCheckPageProbesVisibleEntriesOnly(t *testing.T) {
	ag := &fakeChecker{probed: map[string]bool{}}
	visible := manyEntries(120)
	pg := catalog.Paginate(visible, 3, catalog.DefaultPageSize)

	out, err := runCheckPage(context.Background(), ag, visible, pg)
	if err != nil {
		t.Fatal(err)
	}

	if len(out) != len(pg.Entries) {
		t.Fatalf("verificou %d, esperava %d (a pagina)", len(out), len(pg.Entries))
	}
	if len(ag.probed) != len(pg.Entries) {
		t.Fatalf("sondou %d modelos, esperava %d", len(ag.probed), len(pg.Entries))
	}
}

// The confirmation must name the real cost. "1 request per model" without a
// number is how 40 surprise requests get approved.
func TestCheckPageConsentNamesTheCount(t *testing.T) {
	visible := manyEntries(120)
	pg := catalog.Paginate(visible, 1, catalog.DefaultPageSize)

	msg := checkConsentMessage(pg)
	if !strings.Contains(msg, "40") {
		t.Errorf("o consentimento nao diz quantas requisicoes: %q", msg)
	}
	if !strings.Contains(strings.ToLower(msg), "token") {
		t.Errorf("o consentimento nao menciona custo: %q", msg)
	}
}

// A declined consent must probe NOTHING.
func TestCheckPageDeclinedProbesNothing(t *testing.T) {
	ag := &fakeChecker{probed: map[string]bool{}}
	visible := manyEntries(120)
	pg := catalog.Paginate(visible, 1, catalog.DefaultPageSize)

	if _, ok := runCheckPageIfConfirmed(context.Background(), ag, visible, pg, false); ok {
		t.Fatal("recusar o consentimento nao pode sondar")
	}
	if len(ag.probed) != 0 {
		t.Fatalf("sondou %v sem consentimento", ag.probed)
	}
}

// A provider with no probe capability fails closed with a clear message rather
// than reporting every model as verified or failing.
func TestCheckPageUnsupportedProvider(t *testing.T) {
	ag := &fakeChecker{probed: map[string]bool{}, noProbe: true}
	visible := manyEntries(120)
	pg := catalog.Paginate(visible, 1, catalog.DefaultPageSize)

	_, err := runCheckPage(context.Background(), ag, visible, pg)
	if !errors.Is(err, loop.ErrCheckUnsupported) {
		t.Fatalf("err = %v, esperava ErrCheckUnsupported", err)
	}
}

// The outcome line must carry the SAME number the operator saw in the table.
// o.Index is an index into the whole catalog; the number shown is the position
// in the filtered, ranked page. Using the raw index would point the operator at
// a row that does not exist on their screen — while the check itself was fine.
func TestCheckOutcomeLinesUseTheDisplayedNumber(t *testing.T) {
	ag := &fakeChecker{probed: map[string]bool{}, fail: map[string]error{}}
	entries := manyEntries(120)

	// A QUERY is essential here, not decoration. With no filter, the index in
	// the catalog and the number on screen are the same integer, so a mutation
	// that swapped one for the other would be invisible - and it stayed green
	// for exactly that reason on the first attempt.
	opt := catalogOpts{agentViable: true, query: "modelo-1"}

	_, _, pg := catalogLines(entries, opt, "", 1)
	if len(pg.Entries) == 0 {
		t.Fatal("a consulta nao devolveu nada")
	}
	out, err := runCheckPage(context.Background(), ag, entries, pg)
	if err != nil {
		t.Fatal(err)
	}
	lines := checkOutcomeLines(entries, pg, out)
	if len(lines) == 0 {
		t.Fatal("nenhuma linha de resultado")
	}
	// The first visible row is number 1 even though it sits deep in the
	// catalog: the query dropped everything before it.
	catalogIdx := indexOfEntry(entries, pg.Entries[0].Info.ID)
	if catalogIdx <= 0 {
		t.Fatalf("a consulta nao deslocou as posicoes (indice %d); o teste nao prova nada", catalogIdx)
	}
	first := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(first, "1.") {
		t.Fatalf("primeira linha = %q, esperava o numero 1 (indice no catalogo: %d)", first, catalogIdx)
	}
	if !strings.Contains(first, pg.Entries[0].Info.ID) {
		t.Fatalf("linha %q nao nomeia %s", first, pg.Entries[0].Info.ID)
	}
}

// The call site must pass the SOURCE slice, and this asserts it end to end.
//
// This is the exact defect the Inspiron smoke showed: main.go passed `visible`
// (the filtered copies) to runCheckPage while the REPL re-rendered from
// `entries`, so every verdict was written to a throwaway slice. The check ran,
// the request was spent, and the operator saw the model named wrong in the
// outcome line and no "verificado" tag on the row.
//
// A helper-level test cannot catch this — the helper was correct; the caller
// was wrong. So the assertion is on the effect through the same two calls main
// makes, in the same order.
func TestCheckPageFromTheCallSiteKeepsVerdictsAndNames(t *testing.T) {
	ag := &fakeChecker{probed: map[string]bool{}, fail: map[string]error{}}
	entries := manyEntries(120)
	opt := catalogOpts{agentViable: true, query: "modelo-1"}

	lines, visible, pg := catalogLines(entries, opt, "", 1)
	if len(visible) == 0 {
		t.Fatal("nada visivel")
	}
	wantID := pg.Entries[0].Info.ID

	// What main.go does: check the SOURCE, then re-render from the SOURCE.
	checked, err := runCheckPage(context.Background(), ag, entries, pg)
	if err != nil {
		t.Fatal(err)
	}
	lines, visible, pg = catalogLines(entries, opt, "", 1)
	outLines := checkOutcomeLines(entries, pg, checked)

	if len(outLines) == 0 {
		t.Fatal("nenhuma linha de resultado")
	}
	if !strings.Contains(outLines[0], wantID) {
		t.Fatalf("linha de resultado nomeia %s, mas a pagina mostra %s como 1",
			strings.TrimSpace(outLines[0]), wantID)
	}
	// And the row itself carries the verdict.
	if !strings.Contains(catalogRow(0, visible[0], ""), "verificado") {
		t.Fatalf("a row de %s nao mostra 'verificado' depois do re-render: %s",
			visible[0].Info.ID, catalogRow(0, visible[0], ""))
	}
	if len(lines) == 0 {
		t.Fatal("nenhuma linha de tabela")
	}
}

// Per-model lines, so 40 probes do not look like 40 silent no-ops.
func TestCheckPageLinesReportEveryOutcome(t *testing.T) {
	ag := &fakeChecker{
		probed: map[string]bool{},
		fail:   map[string]error{},
	}
	visible := manyEntries(3)
	pg := catalog.Paginate(visible, 1, catalog.DefaultPageSize)
	ag.fail[visible[0].Info.ID] = errors.New("HTTP 404")

	out, err := runCheckPage(context.Background(), ag, visible, pg)
	if err != nil {
		t.Fatal(err)
	}
	lines := checkOutcomeLines(visible, pg, out)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "404") {
		t.Errorf("a linha do modelo que falhou nao mostra o motivo: %s", joined)
	}
	if !strings.Contains(joined, "verificado") {
		t.Errorf("nenhuma linha diz o que deu certo: %s", joined)
	}
}
