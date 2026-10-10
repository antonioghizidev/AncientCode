package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func tmpDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	// t.TempDir pode devolver um caminho com symlink (/tmp -> /private/tmp em
	// alguns sistemas); os testes resolvem, então comente antes de comparar.
	r, err := filepath.EvalSymlinks(d)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// siblingHome cria base/agent e devolve (base, home).
//
// A casa do agente e a raiz do projeto são IRMÃS por desenho. Um teste que
// usa `base` como casa do agente rejeita a topologia INVALIDA em vez da
// validação — e foi assim que os primeiros testes passaram (ou falharam) por
// motivo errado.
func siblingHome(t *testing.T) (string, string) {
	t.Helper()
	base := tmpDir(t)
	home := filepath.Join(base, "agent")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	return base, home
}

// New recusa o que não é diretório existente. Um /cd para caminho errado
// deixaria o agente sem chão e a próxima tool falharia com erro de baixo
// nível, muito depois do ponto em que o operador achou que a troca deu certo.
func TestNewRejectsMissingAndNonDirectory(t *testing.T) {
	base := tmpDir(t)
	notDir := filepath.Join(base, "arquivo.txt")
	if err := os.WriteFile(notDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := New(filepath.Join(base, "nao-existe"), base); err == nil {
		t.Fatal("caminho inexistente deveria ser recusado")
	}
	if _, err := New(notDir, base); err == nil {
		t.Fatal("arquivo comum nao e diretorio")
	}
	if _, err := New("", base); err == nil {
		t.Fatal("raiz vazia")
	}
}

// /cd cria a pasta se ela nao existe. O operador digita o caminho do projeto
// que ainda vai clonar; recusar por isso seria atrito sem seguranca, ja que o
// .path e do operador, nao do modelo.
func TestNewCreatesProjectRootWhenMissing(t *testing.T) {
	base := tmpDir(t)
	proj := filepath.Join(base, "projeto-novo")
	_, home := siblingHome(t)

	w, err := New(proj, home)
	if err != nil {
		t.Fatalf("deveria criar: %v", err)
	}
	if st, err := os.Stat(proj); err != nil || !st.IsDir() {
		t.Fatalf("pasta nao criada: %v", err)
	}
	if w.ProjectRoot() != proj {
		t.Errorf("ProjectRoot = %q", w.ProjectRoot())
	}
}

// A casa do agente guarda auth.json. Se /cd apontasse para dentro dela, o
// agente passaria a poder ler e reescrever a propria credencial - e nenhuma
// das tools de arquivo distingue "arquivo de credencial" de "arquivo comum",
// porque o confine e por diretorio, nao por nome.
func TestSetRejectsAgentHome(t *testing.T) {
	base, home := siblingHome(t)
	proj := filepath.Join(base, "projeto")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	w, err := New(proj, home)
	if err != nil {
		t.Fatal(err)
	}

	// A casa do agente em si.
	if err := w.SetProjectRoot(home); err == nil {
		t.Fatal("/cd para a casa do agente deveria ser recusado")
	}
	// Qualquer coisa DENTRO dela, que e onde auth.json de fato mora.
	if err := w.SetProjectRoot(filepath.Join(home, "sub")); err == nil {
		t.Fatal("/cd para dentro da casa do agente deveria ser recusado")
	}
	// E o ancestral da casa do agente tambem: /home permitiria chegar nela.
	if err := w.SetProjectRoot(base); err == nil {
		t.Fatal("/cd para o ancestral da casa do agente deveria ser recusado")
	}
}

// O projeto NAO pode conter a casa do agente: seria o mesmo problema pelo
// outro lado, com o agente ganhando um caminho para o arquivo de credencial.
func TestNewRejectsProjectContainingAgentHome(t *testing.T) {
	base, home := siblingHome(t)
	if _, err := New(base, home); err == nil {
		t.Fatal("projeto que contem a casa do agente deveria ser recusado")
	}
}

// Ampliar precisa ser distinguivel de estreitar: e isso que decide se o
// operador e consultado. Teste a propriedade, nao a implementacao.
func TestWidensReportsAncestorRelation(t *testing.T) {
	base, home := siblingHome(t)
	proj := filepath.Join(base, "workspace", "projeto")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	w, err := New(proj, home)
	if err != nil {
		t.Fatal(err)
	}

	// Subir para o pai: amplia.
	if !w.Widens(filepath.Dir(proj)) {
		t.Error("subir para o pai deveria contar como ampliar")
	}
	// Ir para um irmao: nem amplia nem estreita.
	if w.Widens(filepath.Join(base, "outro")) {
		t.Error("irmao nao amplia nem estreita")
	}
	// Descer: estreita.
	if w.Widens(filepath.Join(proj, "interno")) {
		t.Error("descer nao amplia")
	}
	// A propria raiz nao amplia.
	if w.Widens(proj) {
		t.Error("a propria raiz nao amplia")
	}
}

// O rascunho é o que permite ao agente gerar log e artefato sem sujar o
// repositório — é a razão de ele existir. Se deixar de ser lugar de escrita
// livre, o agente volta a despejar `go build > out.txt` na raiz do projeto, e
// ninguém notaria até o git status ficar sujo.
func TestScratchAndProjectRootAreWritableWithoutGrant(t *testing.T) {
	base, home := siblingHome(t)
	proj := filepath.Join(base, "projeto")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	w, err := New(proj, home)
	if err != nil {
		t.Fatal(err)
	}

	// O proprio projeto.
	if !w.Granted(filepath.Join(proj, "main.go")) {
		t.Error("a raiz do projeto deveria ser lugar de escrita")
	}
	// O rascunho, e o que estiver dentro dele.
	scratch := w.Scratch()
	if scratch == "" {
		t.Fatal("Scratch vazio")
	}
	if !w.Granted(filepath.Join(scratch, "build.log")) {
		t.Error("o rascunho deveria ser lugar de escrita livre")
	}
	// E o resto da casa do agente NAO: so o subdiretorio de rascunho conta.
	if w.Granted(filepath.Join(home, "auth.json")) {
		t.Error("auth.json nao deveria ser lugar de escrita")
	}
	if w.Granted(filepath.Join(base, "qualquer-coisa")) {
		t.Error("lugar nenhum fora de projeto+rascunho deveria estar liberado")
	}
}

// Grant concede escrita num diretorio e /grant e do operador; o caminho
// concedido nao pode estar dentro da casa do agente pelo mesmo motivo da raiz.
func TestGrantRejectsAgentHomeAndAcceptsElsewhere(t *testing.T) {
	base := tmpDir(t)
	home := filepath.Join(base, "agent")
	proj := filepath.Join(base, "projeto")
	artifacts := filepath.Join(base, "artefatos")
	for _, d := range []string{home, proj, artifacts} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	w, err := New(proj, home)
	if err != nil {
		t.Fatal(err)
	}

	if err := w.Grant(filepath.Join(home, "segredo")); err == nil {
		t.Fatal("grant dentro da casa do agente deveria ser recusado")
	}
	if err := w.Grant(artifacts); err != nil {
		t.Fatalf("grant de artefatos deveria passar: %v", err)
	}
	if !w.Granted(artifacts) {
		t.Error("artifacts deveria estar concedido")
	}
	if w.Granted(filepath.Join(base, "nao-concedido")) {
		t.Error("pasta nao concedida retornou concedida")
	}
	// Concedido inclui a propria pasta e o que esta dentro dela.
	if !w.Granted(filepath.Join(artifacts, "saida.bin")) {
		t.Error("grant deveria cobrir o conteudo da pasta")
	}
}

// Symlink: a raiz precisa ser o caminho RESOLVIDO. Sem isso, um link para
// fora passaria a validação e o confine passaria a valer para o alvo.
func TestSetResolvesSymlinks(t *testing.T) {
	base := tmpDir(t)
	real := filepath.Join(base, "projeto-real")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "atalho")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink indisponivel: %v", err)
	}
	home := filepath.Join(base, "agent")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	w, err := New(real, home)
	if err != nil {
		t.Fatal(err)
	}

	if err := w.SetProjectRoot(link); err != nil {
		t.Fatalf("link para projeto legitimo: %v", err)
	}
	if got := w.ProjectRoot(); got != real {
		t.Errorf("ProjectRoot = %q, esperava o caminho resolvido %q", got, real)
	}
}

// Symlink para a casa do agente tambem e recusado — a validacao tem de rodar
// DEPOIS de resolver, senao um link contorna a recusa por caminho.
func TestSetRejectsSymlinkIntoAgentHome(t *testing.T) {
	base := tmpDir(t)
	home := filepath.Join(base, "agent")
	proj := filepath.Join(base, "projeto")
	for _, d := range []string{home, proj} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(base, "parece-projeto")
	if err := os.Symlink(home, link); err != nil {
		t.Skipf("symlink indisponivel: %v", err)
	}
	w, err := New(proj, home)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetProjectRoot(link); err == nil {
		t.Fatal("link apontando para a casa do agente deveria ser recusado")
	}
}
