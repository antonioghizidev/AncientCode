package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A mensagem de fallback precisa dizer as três coisas: o que estava salvo, por
// que não serviu e onde o harness foi parar. Sem o destino, o operador fica
// sem saber em qual pasta o agente vai escrever.
func TestSavedRootMessageSaysFallbackAndReason(t *testing.T) {
	msg := savedRootNotice("/caminho/que/nao/existe", "a pasta nao existe", "/home/USUARIO/workspace/PROJETO")
	if !strings.Contains(msg, "/caminho/que/nao/existe") {
		t.Errorf("a mensagem nao diz o que estava salvo: %q", msg)
	}
	if !strings.Contains(msg, "a pasta nao existe") {
		t.Errorf("a mensagem nao diz o motivo: %q", msg)
	}
	if !strings.Contains(msg, "/home/USUARIO/workspace/PROJETO") {
		t.Errorf("a mensagem nao diz onde o harness vai trabalhar: %q", msg)
	}
	if !strings.Contains(msg, "state.json") {
		t.Errorf("a mensagem nao diz onde o valor estava gravado: %q", msg)
	}
}

// staleReader simula um state.json apontando para uma pasta que nao existe mais
// — exatamente o caso do smoke: o projeto foi apagado na limpeza.
type staleReader struct {
	root  string
	ok    bool
	err   error
	calls int
}

func (s *staleReader) ProjectRoot() (string, bool, error) {
	s.calls++
	return s.root, s.ok, s.err
}

// O STARTUP precisa avisar. O helper avisar nao basta: e o call site que decide
// se a mensagem chega na tela. D-75, pela terceira vez — e a mutacao [A] abaixo
// existia justamente porque so havia teste do helper.
func TestStartupWarnsWhenSavedRootIsGone(t *testing.T) {
	w, _ := wsFixture(t)
	configRoot := w.ProjectRoot()
	sumido := filepath.Join(filepath.Dir(configRoot), "projeto-apagado")

	aviso := restoreSavedProjectRoot(w, &staleReader{root: sumido, ok: true}, configRoot)
	if aviso == "" {
		t.Fatal("raiz guardada e inexistente: o startup nao avisou nada")
	}
	if !strings.Contains(aviso, sumido) {
		t.Errorf("o aviso nao diz qual pasta falhou: %q", aviso)
	}
	if !strings.Contains(aviso, configRoot) {
		t.Errorf("o aviso nao diz onde o harness vai trabalhar: %q", aviso)
	}
	// E a raiz real NAO pode ter virado a pasta apagada.
	if w.ProjectRoot() == sumido {
		t.Fatal("a raiz mudou para a pasta que nao existe")
	}
}

// E quando a raiz guardada é válida, nenhum aviso — para não virar ruído.
func TestStartupSilentWhenSavedRootIsUsable(t *testing.T) {
	w, base := wsFixture(t)
	alvo := filepath.Join(base, "projeto-valido")
	if err := os.MkdirAll(alvo, 0o700); err != nil {
		t.Fatal(err)
	}
	aviso := restoreSavedProjectRoot(w, &staleReader{root: alvo, ok: true}, w.ProjectRoot())
	if aviso != "" {
		t.Errorf("raiz valida gerou aviso: %q", aviso)
	}
	if w.ProjectRoot() != alvo {
		t.Errorf("ProjectRoot = %q, esperava %q", w.ProjectRoot(), alvo)
	}
}

// Sem project_root gravado, nada acontece e nada se diz.
func TestStartupSilentWhenNothingSaved(t *testing.T) {
	w, _ := wsFixture(t)
	antes := w.ProjectRoot()
	for _, s := range []*staleReader{
		{root: "", ok: false},
		{root: "/nao/faz/nada"},
	} {
		if aviso := restoreSavedProjectRoot(w, s, antes); aviso != "" {
			t.Errorf("sem raiz gravada gerou aviso: %q", aviso)
		}
	}
	if w.ProjectRoot() != antes {
		t.Error("a raiz mudou sem nada gravado")
	}
}
