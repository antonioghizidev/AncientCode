package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// Restaurar do state.json NÃO pode recriar a pasta.
//
// /cd cria o diretório porque o operador pode estar digitando o projeto que
// ainda vai clonar. Um startup é diferente: se a pasta sumiu, foi porque alguém
// a removeu — de propósito. Recriá-la seria o harness desobedecendo uma
// remoção, e com um nome de diretório que o operador jáRKirk rejeitou.
func TestRestoreDoesNotRecreateAMissingDirectory(t *testing.T) {
	_, home := siblingHome(t)
	w, err := New(filepath.Join(mustMkdir(t, "projeto")), home)
	if err != nil {
		t.Fatal(err)
	}
	base, _ := filepath.EvalSymlinks(t.TempDir())
	sumido := filepath.Join(base, "projeto-apagado")

	err = w.RestoreProjectRoot(sumido)
	if err == nil {
		t.Fatal("restaurar uma pasta inexistente deveria falhar")
	}
	if _, statErr := os.Stat(sumido); statErr == nil {
		t.Fatal("a restauracao CRIOU a pasta apagada")
	}
	if w.ProjectRoot() == sumido {
		t.Fatal("a raiz mudou apesar da falha")
	}
}

// Restaurar uma pasta existente funciona — é o caminho normal.
func TestRestoreUsesAnExistingDirectory(t *testing.T) {
	_, home := siblingHome(t)
	inicial := mustMkdir(t, "projeto-inicial")
	w, err := New(inicial, home)
	if err != nil {
		t.Fatal(err)
	}
	alvo := mustMkdir(t, "projeto-real")

	if err := w.RestoreProjectRoot(alvo); err != nil {
		t.Fatalf("restaurar uma pasta existente: %v", err)
	}
	if w.ProjectRoot() != alvo {
		t.Errorf("ProjectRoot = %q, esperava %q", w.ProjectRoot(), alvo)
	}
}

// E as recusas de sempre continuam valendo na restauração.
func TestRestoreKeepsTheAgentHomeRefusal(t *testing.T) {
	base, home := siblingHome(t)
	inicial := mustMkdir(t, "p1")
	if err := os.MkdirAll(filepath.Join(base, "p2"), 0o700); err != nil {
		t.Fatal(err)
	}
	w, err := New(inicial, home)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.RestoreProjectRoot(home); err == nil {
		t.Fatal("restaurar para a casa do agente deveria ser recusado")
	}
	if err := w.RestoreProjectRoot(filepath.Join(home, "sub")); err == nil {
		t.Fatal("restaurar para dentro da casa do agente deveria ser recusado")
	}
}

func mustMkdir(t *testing.T, name string) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(base, name)
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}
