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
