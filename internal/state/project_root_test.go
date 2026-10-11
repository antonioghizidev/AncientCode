package state

import (
	"os"
	"path/filepath"
	"testing"
)

// O ultimo project_root volta na proxima execucao.
//
// O path vem do state.json, que e DISCO — e um state.json editado a mao escolhe
// a pasta que o agente vai passar a escrever. Por isso a mesma gramaria estrita
// do model id se aplica: fail-closed na leitura, nunca aceitar o que vier.
func TestProjectRootRoundTrips(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	raiz := filepath.Join(dir, "workspace", "projeto")
	if err := s.SetProjectRoot(raiz); err != nil {
		t.Fatalf("SetProjectRoot: %v", err)
	}
	got, ok, err := s.ProjectRoot()
	if err != nil || !ok {
		t.Fatalf("leitura: ok=%v err=%v", ok, err)
	}
	if got != raiz {
		t.Errorf("ProjectRoot = %q, esperava %q", got, raiz)
	}

	// Numa Store nova, o mesmo arquivo.
	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	got2, ok2, err := s2.ProjectRoot()
	if err != nil || !ok2 || got2 != raiz {
		t.Errorf("releitura: %q ok=%v err=%v", got2, ok2, err)
	}
}

// Um path que nao cabe (ou contem NUL) e recusado na HORA DE GRAVAR, nao na
// leitura: um state.json invalido nao pode ficar esperando alguem abrir.
func TestSetProjectRootRejectsInvalidPaths(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "com\x00nul", string(make([]byte, 4096))} {
		if err := s.SetProjectRoot(bad); err == nil {
			t.Errorf("caminho invalido aceito: %q", bad)
		}
	}
}

// Ler um path invalido do disco nao pode devolver caminho nenhum: devolver
// seria confiar num arquivo que alguem escreveu.
func TestProjectRootOnDiskIsValidated(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	if err := os.WriteFile(p, []byte(`{"version":2,"models":{},"project_root":"com\u0000nul"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.ProjectRoot()
	if err == nil && ok {
		t.Fatalf("state.json com NUL foi aceito: %q", got)
	}
}

// Um state.json com versao antiga (sem o campo) continua legivel: quem
// Atualizou o binario nao pode perder a memoria de modelo.
func TestProjectRootAbsentIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetModel("openrouter", "org/modelo"); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := s2.ProjectRoot()
	if err != nil || ok || got != "" {
		t.Errorf("sem project_root: %q ok=%v err=%v", got, ok, err)
	}
	if m, ok, err := s2.GetModel("openrouter"); err != nil || !ok || m != "org/modelo" {
		t.Errorf("a memoria de modelo se perdeu: %q ok=%v err=%v", m, ok, err)
	}
}
