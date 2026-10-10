package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antonioghizidev/ancientcode/internal/workspace"
)

func tuiWs(t *testing.T) (*tuiModel, *workspace.Workspace) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "agente")
	proj := filepath.Join(base, "workspace", "projeto")
	for _, d := range []string{home, proj} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	w, err := workspace.New(proj, home)
	if err != nil {
		t.Fatal(err)
	}
	m := mkModel(t, nil)
	m.ws = w
	return m, w
}

// A TUI precisa ter o workspace. Sem injeção, /cd responderia "indisponivel"
// e o operador teria um comando que existe no REPL e não existe na TUI —
// exatamente o tipo de paridade que o M12 jáPagou uma vez.
func TestTUIHasTheWorkspace(t *testing.T) {
	m, w := tuiWs(t)
	if m.ws == nil {
		t.Fatal("a TUI precisa do workspace")
	}
	m.handleCd("")
	if len(m.session) == 0 {
		t.Fatal("/cd sem argumento nao respondeu nada na TUI")
	}
	if !strings.Contains(m.session[0], w.ProjectRoot()) {
		t.Errorf("a TUI nao mostrou a raiz: %q", m.session[0])
	}
}

// Estreitar nao pede nada na TUI.
func TestTUICdNarrowingAppliesImmediately(t *testing.T) {
	m, w := tuiWs(t)
	dentro := filepath.Join(w.ProjectRoot(), "internal")
	if err := os.MkdirAll(dentro, 0o700); err != nil {
		t.Fatal(err)
	}
	m.handleCd(dentro)
	if w.ProjectRoot() != dentro {
		t.Fatalf("raiz = %q, esperava %q", w.ProjectRoot(), dentro)
	}
}

// Ampliar na TUI pede confirmacao por repeticao do comando. O ponto de
// seguranca: na primeira vez a raiz NAO pode ter mudado.
func TestTUICdWideningNeedsRetypeBeforeItMoves(t *testing.T) {
	m, w := tuiWs(t)
	proj := w.ProjectRoot()
	pai := filepath.Dir(proj)

	m.handleCd(pai)
	if w.ProjectRoot() != proj {
		t.Fatalf("a primeira vez ja trocou a raiz: %q", w.ProjectRoot())
	}
	if m.pendingCd != pai {
		t.Fatalf("a confirmacao ficou pendente? %q", m.pendingCd)
	}
	// O operador precisa ver que aquilo amplia.
	joined := strings.Join(m.session, "\n")
	if !strings.Contains(joined, "AMPLIA") {
		t.Errorf("a TUI nao avisou que amplia: %q", joined)
	}

	// Repetir o comando confirma.
	m.handleCd(pai)
	if w.ProjectRoot() != pai {
		t.Fatalf("apos repetir, a raiz nao mudou: %q", w.ProjectRoot())
	}
	if m.pendingCd != "" {
		t.Error("a confirmacao deveria estar limpa depois de aplicada")
	}
}

// Um comando diferente descarta o pendente: assim um /cd antigo nunca dispara
// por acidente depois, no tipo de keystroke que o operador nao associaria a ele.
func TestTUICdPendingIsDiscardedByAnotherPath(t *testing.T) {
	m, w := tuiWs(t)
	proj := w.ProjectRoot()
	pai := filepath.Dir(proj)
	irmao := filepath.Join(filepath.Dir(proj), "outro")
	if err := os.MkdirAll(irmao, 0o700); err != nil {
		t.Fatal(err)
	}

	m.handleCd(pai) // pendente
	m.handleCd(irmao)

	if m.pendingCd != "" {
		t.Errorf("o pendente deveria ter sido descartado: %q", m.pendingCd)
	}
	if w.ProjectRoot() == pai {
		t.Fatal("o pendente antigo disparou quando nao devia")
	}
	if w.ProjectRoot() != irmao {
		t.Errorf("raiz = %q, esperava %q", w.ProjectRoot(), irmao)
	}
	_ = proj
}

// A TUI recusa a casa do agente e diz por que.
func TestTUICdRefusesAgentHome(t *testing.T) {
	m, w := tuiWs(t)
	proj := w.ProjectRoot()
	m.handleCd(w.AgentHome())
	if w.ProjectRoot() != proj {
		t.Errorf("raiz mudou numa troca recusada: %q", w.ProjectRoot())
	}
	if !strings.Contains(strings.Join(m.session, "\n"), "erro") {
		t.Error("a recusa nao foi mostrada")
	}
}

// A linha de onde estamos working é sempre visível: o operador pediu isso, e
// sem ela `pwd` e o confine das tools podem discordar em silêncio.
func TestProjectLineShowsTheCurrentFolder(t *testing.T) {
	_, w := tuiWs(t)
	got := projectLine(w)
	if got == "" {
		t.Fatal("a linha de projeto não pode ser vazia")
	}
	if got != filepath.Base(w.ProjectRoot()) {
		t.Errorf("projectLine = %q, esperava %q", got, filepath.Base(w.ProjectRoot()))
	}
	if projectLine(nil) != "" {
		t.Error("sem workspace, a linha deveria ser vazia em vez de inventar pasta")
	}
}
