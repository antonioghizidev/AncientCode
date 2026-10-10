package files

import (
	"os"
	"path/filepath"
	"testing"
)

// A raiz tem de ser lida no momento do uso, e nao capturada na construcao.
// Sem isso o /cd trocaria o Workspace mas as file tools continuariam presas ao
// path antigo — o agente leria no projeto novo e escreveria no velho.
func TestFilesFollowsTheRootAtCallTime(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "projeto-a")
	b := filepath.Join(base, "projeto-b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	current := a
	fs := NewFilesLive(func() string { return current }, 0)

	if err := os.WriteFile(filepath.Join(a, "nota.txt"), []byte("de-a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "nota.txt"), []byte("de-b"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := readRoot(t, fs, "nota.txt")
	if string(got) != "de-a" {
		t.Fatalf("leu %q na raiz A", got)
	}

	// A troca da raiz (o /cd).
	current = b

	got = readRoot(t, fs, "nota.txt")
	if string(got) != "de-b" {
		t.Fatalf("depois da troca, leu %q; a tool ficou presa a raiz antiga", got)
	}
}

// O confine precisa acompanhar a raiz: depois do /cd, escrever num path da raiz
// ANTIGA tem de ser recusado. Sem isso, o /cdvaleria apenas para leitura.
func TestFilesRefusesWritesOutsideTheCurrentRoot(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "projeto-a")
	b := filepath.Join(base, "projeto-b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	current := a
	fs := NewFilesLive(func() string { return current }, 0)

	// Antes: escrever em A funciona.
	if _, err := fs.ResolveWrite("novo.txt"); err != nil {
		t.Fatalf("escrita na raiz ativa deveria passar: %v", err)
	}

	current = b

	if _, err := fs.ResolveWrite("novo.txt"); err != nil {
		t.Fatalf("deveria escrever na raiz nova, que tambem tem 'novo.txt': %v", err)
	}
	// E um path que existe so em A agora esta fora.
	escapado := filepath.Join(a, "nota-so-em-a.txt")
	if err := os.WriteFile(escapado, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(b, escapado)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fs.ResolveWrite(rel)
	if err == nil {
		t.Fatal("escrita na raiz antiga deveria ser recusada depois do /cd")
	}
}

// readRoot resolve rel contra a raiz que a tool usa AGORA e devolve o conteudo.
// Passa por resolve (o mesmo caminho do read_file) em vez de montar o path na
// mao: um helper que monta o path ele mesmo testaria o helper, nao a tool.
func readRoot(t *testing.T, fs *Files, rel string) []byte {
	t.Helper()
	p, err := fs.Resolve(rel)
	if err != nil {
		t.Fatalf("resolve(%q): %v", rel, err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %q: %v", p, err)
	}
	return b
}

// NewFiles (raiz fixa) continua funcionando: quem chama assim quer uma tool
// imutavel, e o teste antigo da suite depende disso.
func TestNewFilesStillTakesAFixedRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("fixo"), 0o600); err != nil {
		t.Fatal(err)
	}
	fs := NewFiles(dir, 0)
	if fs.Root() != filepath.Clean(dir) {
		t.Errorf("Root = %q", fs.Root())
	}
	if got := readRoot(t, fs, "x.txt"); string(got) != "fixo" {
		t.Errorf("leu %q", got)
	}
}
