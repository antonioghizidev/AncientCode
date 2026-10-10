package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// grantedFixture monta projeto + granted + scratch, com um Files cujas regras
// de escrita espelham o Workspace: projeto, rascunho e concessões.
func grantedFixture(t *testing.T) (*Files, string, string, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(base, "projeto")
	scratch := filepath.Join(base, "scratch")
	granted := filepath.Join(base, "artefatos")
	for _, d := range []string{proj, scratch, granted} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	under := func(p, root string) bool {
		rel, err := filepath.Rel(root, p)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	fs := NewFilesGranted(
		func() string { return proj },
		func(p string) bool {
			return under(p, proj) || under(p, scratch) || under(p, granted)
		},
		0)
	return fs, proj, scratch, granted
}

// O ponto do milestone: /grant precisa ter EFEITO onde o confine é possível.
// Antes, a concessão era só bookkeeping — o shell escrevia lá de qualquer jeito
// (não é confinado por path), e as file tools recusavam. Ficava registro de um
// diálogo que não mudava nada.
func TestWriteOutsideRootIsAllowedWhenGranted(t *testing.T) {
	fs, _, _, granted := grantedFixture(t)
	dest := filepath.Join(granted, "saida.bin")

	got, err := fs.ResolveWrite(dest)
	if err != nil {
		t.Fatalf("escrita concedida deveria passar: %v", err)
	}
	if got != dest {
		t.Errorf("destino = %q", got)
	}
}

// E continua valendo o que NÃO foi concedido. Sem isto, "conceder uma pasta"
// seria indistinguível de "conceder o sistema de arquivos".
func TestWriteOutsideRootIsRefusedWhenNotGranted(t *testing.T) {
	fs, _, _, _ := grantedFixture(t)
	base, _ := filepath.EvalSymlinks(t.TempDir())
	alheio := filepath.Join(base, "alheio")
	if err := os.MkdirAll(alheio, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.ResolveWrite(filepath.Join(alheio, "x")); err == nil {
		t.Fatal("escrita fora do projeto sem concessao deveria ser recusada")
	}
}

// Ler tambem: o pedido do usuario foi "ler E escrever fora do projeto", e o
// /grant responde a isso. Um grant so de escrita deixaria o agente inspecionar
// uma pasta que ele nao pode alterar — e assimetricidade assim e sulitil.
func TestReadOutsideRootIsAllowedWhenGranted(t *testing.T) {
	fs, _, _, granted := grantedFixture(t)
	f := filepath.Join(granted, "entrada.txt")
	if err := os.WriteFile(f, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := fs.Resolve(f)
	if err != nil {
		t.Fatalf("leitura concedida deveria passar: %v", err)
	}
	if got != f {
		t.Errorf("destino = %q", got)
	}
	if _, err := fs.Resolve("/etc/passwd"); err == nil {
		t.Fatal("leitura de /etc/passwd nunca deveria passar")
	}
}

// Conceder uma pasta NAO concede o resto do sistema, mesmo com path traversal
// apontando para fora dela.
func TestGrantedDirDoesNotLeakViaTraversal(t *testing.T) {
	fs, _, _, granted := grantedFixture(t)
	base, _ := filepath.EvalSymlinks(t.TempDir())
	alheio := filepath.Join(base, "alheio")
	if err := os.MkdirAll(alheio, 0o700); err != nil {
		t.Fatal(err)
	}
	// granted/../alheio/x resolve para alheio/x, que NAO foi concedido.
	viaTravessia := filepath.Join(granted, "..", "alheio", "x")
	if _, err := fs.ResolveWrite(viaTravessia); err == nil {
		t.Fatalf("travessia saiu da pasta concedida: %q", viaTravessia)
	}
}

// Symlink: a concessao vale para o caminho RESOLVIDO. Um link dentro da pasta
// concedida apontando para fora deve ser recusado — senão conceder /tmp dá
// acesso ao sistema inteiro.
func TestGrantDoesNotCoverSymlinkEscape(t *testing.T) {
	fs, _, _, granted := grantedFixture(t)
	base, _ := filepath.EvalSymlinks(t.TempDir())
	alheio := filepath.Join(base, "alheio")
	if err := os.MkdirAll(alheio, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(granted, "atalho")
	if err := os.Symlink(alheio, link); err != nil {
		t.Skipf("symlink indisponivel: %v", err)
	}
	// O path中含有 a pasta concedida, mas resolve para fora dela.
	if _, err := fs.ResolveWrite(filepath.Join(link, "x")); err == nil {
		t.Fatal("symlink para fora da pasta concedida deveria ser recusado")
	}
}

// Path RELATIVO fora do projeto continua recusado como sempre: relative é
// interpretado contra a raiz do projeto, e a concessão só vale para caminho
// absoluto e explícito. Sem isso, um ".." relativo escaparia pela brecha.
func TestRelativeTraversalStillRefused(t *testing.T) {
	fs, _, _, granted := grantedFixture(t)
	if _, err := fs.ResolveWrite(filepath.Join("..", filepath.Base(granted), "x")); err == nil {
		t.Fatal("travessia relativa para uma pasta concedida deveria ser recusada")
	}
}

// Sem concessao nenhuma, o comportamento é o de sempre: raiz relativa, absoluto
// recusado. O modo concessions-off não pode afrouxar nada.
func TestNoGrantsKeepsStrictBehaviour(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	proj := filepath.Join(base, "projeto")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	fs := NewFilesGranted(func() string { return proj }, nil, 0)

	if _, err := fs.ResolveWrite("ok.txt"); err != nil {
		t.Fatalf("relativo dentro da raiz: %v", err)
	}
	if _, err := fs.ResolveWrite(filepath.Join(base, "fora.txt")); err == nil {
		t.Fatal("absoluto fora da raiz deveria ser recusado sem concessao")
	}
}
