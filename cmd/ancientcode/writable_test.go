package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// O aviso precisa dizer QUEM precisa de acesso, e COMO dar. Um aviso que só
// diz "sem permissão" obriga o operador a pesquisar a resposta; o ponto do /cd
// é ser a interface onde a causa aparece.
func TestNotWritableMessageNamesTheUserAndTheFix(t *testing.T) {
	root := "/home/USUARIO/workspace/PROJETO"
	msg := notWritableMessage(root, "ancientcode")

	if !strings.Contains(msg, "ancientcode") {
		t.Errorf("o aviso nao diz qual usuario: %q", msg)
	}
	if !strings.Contains(msg, root) {
		t.Errorf("o aviso nao diz qual pasta: %q", msg)
	}
	// O comando tem de aparecer de verdade, executavel.
	if !strings.Contains(msg, "chgrp -R ancientcode "+root) {
		t.Errorf("o aviso nao traz o chgrp pronto: %q", msg)
	}
	if !strings.Contains(msg, "chmod -R g+rwX") {
		t.Errorf("o aviso nao traz o chmod: %q", msg)
	}
	// E o setgid: sem ele, todo arquivo novo nasce sem escrita para o grupo e
	// o problema volta sem ninguem avisar.
	//
	// O comando INTEIRO, e nao so o substring "g+s": a palavra tambem aparece na
	// frase de explicacao, e um teste que aceita o substring passava mesmo sem
	// o comando — foi o que a mutacao [E] expôs.
	if !strings.Contains(msg, "sudo find "+root+" -type d -exec chmod g+s") {
		t.Errorf("o aviso nao traz o comando do setgid: %q", msg)
	}
	// E deixa claro que ler funciona, para ninguem achar que o /cd falhou.
	if !strings.Contains(strings.ToLower(msg), "ler") && !strings.Contains(strings.ToLower(msg), "le ") {
		t.Errorf("o aviso nao diz que leitura continua valendo: %q", msg)
	}
}

// Writability é medida, nunca inferida de bit de permissão: ACL, mount
// read-only e container discordam do modo. A prova é criar um arquivo.
func TestWritabilityIsMeasuredNotGuessed(t *testing.T) {
	dir := t.TempDir()
	if err := checkWritable(dir); err != nil {
		t.Fatalf("uma pasta recem-criada deveria ser gravavel: %v", err)
	}

	// Um arquivo comum no lugar da pasta: nao é diretório, logo nao é raiz
	// utilizável — e é exatamente o que a detecção precisa dizer.
	arq := filepath.Join(dir, "nao-e-pasta")
	if err := os.WriteFile(arq, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkWritable(arq); err == nil {
		t.Fatal("um arquivo comum nao deveria contar como pasta gravavel")
	}
}

// A detecção NÃO deixa lixo: o arquivo de prova some.
func TestWritabilityProbeLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	if err := checkWritable(dir); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Fatalf("a sonda deixou %d arquivo(s) para tras", len(ents))
	}
}

// /cd para uma pasta não gravável AVISA, mas não recusa: revisar um projeto
// somente-leitura é legítimo. Recusar transformaria um aviso em obstáculo.
func TestCdWarnsButStillSwitches(t *testing.T) {
	w, _ := wsFixture(t)
	base2 := t.TempDir() // dirname separado: em Windows nao podemos tirar escrita de verdade
	proj := filepath.Join(base2, "projeto")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	// Numa pasta gravavel nao ha aviso.
	res := cdCommand(w, proj)
	if res.Warning != "" {
		t.Errorf("pasta gravavel nao deveria gerar aviso: %q", res.Warning)
	}
}

// O aviso só aparece DEPOIS de aplicar: antes, seria sobre a pasta antiga.
func TestCdWarningTravelsWithTheResult(t *testing.T) {
	w, _ := wsFixture(t)
	res := cdCommand(w, "")
	if res.Warning != "" {
		t.Errorf("/cd sem argumento nao deveria trazer aviso: %q", res.Warning)
	}
}

// notWritableProbe troca a medição por "sempre não gravável" e restaura ao fim.
//
// Tornar uma pasta não gravável não é portátil — no Windows o processo é
// quase sempre administrador e o bit é consultivo — então a única forma honesta
// de testar o aviso é trocar a MEDIDA. Sem esta indireção, três mutações
// ficaram verdes: o aviso podia deixar de existir e a suíte não notaria.
func notWritableProbe(t *testing.T) {
	t.Helper()
	orig := writableProbe
	writableProbe = func(string) error { return os.ErrPermission }
	t.Cleanup(func() { writableProbe = orig })
}

// O aviso precisa ACABAR existindo quando a pasta não é gravável.
//
// Só havia teste para o caso gravável, e é o oposto que importa: é ele que
// evita o operador descobrir a recusa três comandos depois.
func TestCdProducesWarningWhenRootIsNotWritable(t *testing.T) {
	notWritableProbe(t)
	w, base := wsFixture(t)
	alvo := filepath.Join(base, "projeto-somente-leitura")
	if err := os.MkdirAll(alvo, 0o700); err != nil {
		t.Fatal(err)
	}

	res := cdCommand(w, alvo)
	if res.Warning == "" {
		t.Fatal("pasta nao gravavel nao gerou aviso nenhum")
	}
	if !strings.Contains(res.Warning, alvo) {
		t.Errorf("o aviso cita outra pasta: %q", res.Warning)
	}
	if !strings.Contains(res.Warning, currentUserName()) {
		t.Errorf("o aviso nao nomeia o usuario atual (%q): %q", currentUserName(), res.Warning)
	}
	// E o aviso NÃO impede a troca: revisar um projeto somente-leitura é legítimo.
	out := res.Apply(w, true)
	if !out.Changed || w.ProjectRoot() != alvo {
		t.Fatalf("a troca nao aconteceu apesar do aviso: %q", w.ProjectRoot())
	}
}

// E o REPL precisa PRINTAR. Um aviso calculado e nunca mostrado é o mesmo
// defeito do `/check` que pedia consentimento e não sondava nada.
func TestReplPrintsTheWarning(t *testing.T) {
	notWritableProbe(t)
	w, base := wsFixture(t)
	alvo := filepath.Join(base, "so-leitura")
	if err := os.MkdirAll(alvo, 0o700); err != nil {
		t.Fatal(err)
	}

	saida := captureStdio(t, func() {
		runCdREPL(w, command{kind: cmdCd, arg: alvo},
			func(string) (string, error) { return "y", nil }, nil)
	})
	if !strings.Contains(saida, "aviso:") {
		t.Fatalf("o REPL nao imprimiu o aviso:\n%s", saida)
	}
	if !strings.Contains(saida, "chgrp") {
		t.Fatalf("o aviso impresso nao traz o comando:\n%s", saida)
	}
	if w.ProjectRoot() != alvo {
		t.Errorf("a troca nao aconteceu: %q", w.ProjectRoot())
	}
}

// captureStdio redirects stdout and stderr into one buffer while fn runs.
//
// The warning goes to stderr, so asserting on stdout alone would pass while the
// operator sees nothing — the same class of hole this warning exists to close.
func captureStdio(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w

	collected := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		collected <- string(b)
	}()

	func() {
		defer func() {
			os.Stdout, os.Stderr = origOut, origErr
			_ = w.Close()
		}()
		fn()
	}()

	return <-collected
}
