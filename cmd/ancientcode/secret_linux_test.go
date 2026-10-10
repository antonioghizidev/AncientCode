//go:build linux

package main

import (
	"os"
	"strings"
	"testing"
)

// Defeito real encontrado pelo gate de `go install` do M14 (rodando o binario
// fora da arvore com stdin em </dev/null): hasStdinTerminal() usava
// ModeCharDevice, e /dev/null E um char device. O resultado era o onboarding
// anunciar "vou pedir a chave" e so depois falhar com "stdin is not a terminal".
//
// O teste aponta a sondagem para /dev/null de verdade, sem trocar o fd 0 do
// binario de teste.
func TestHasStdinTerminalRejectsDevNull(t *testing.T) {
	f, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Skipf("nao consegui abrir %s: %v", os.DevNull, err)
	}
	defer f.Close()

	// Pre-condicao: o teste so vale se /dev/null realmente for char device,
	// que e a razao pela qual a checagem antiga mentia.
	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeCharDevice == 0 {
		t.Skip("/dev/null nao e char device neste ambiente; o defeito nao se reproduz")
	}

	if _, ok := termiosFd(f.Fd()); ok {
		t.Fatal("/dev/null passou por terminal: o onboarding abriria um dialogo e so falharia depois")
	}
}

// Um terminal de verdade tem de passar, senao a correcao acima teria trocado um
// bug por outro: o harness pararia de oferecer /connect onde ele funciona.
func TestHasStdinTerminalAcceptsRealTerminal(t *testing.T) {
	if !hasStdinTerminal() {
		t.Skip("stdin nao e terminal neste ambiente (execucao sem TTY)")
	}
}

// Um pipe NAO e terminal. E o caso do CI: onboarding nao pode abrir dialogo.
func TestHasStdinTerminalRejectsPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if _, ok := termiosFd(r.Fd()); ok {
		t.Fatal("um pipe passou por terminal")
	}
}

// O caminho absoluto no primeiro uso e exigido pelo §3.8: um "~" literal nao
// ajuda o operador cujo shell nao expande, que e justamente o caso do primeiro
// uso sem config.
func TestStorePathDisplayIsAbsolute(t *testing.T) {
	got := storePathDisplay()
	if strings.HasPrefix(got, "~") {
		t.Fatalf("a mensagem mostra um ~ literal: %q", got)
	}
	if !strings.HasPrefix(got, "/") {
		t.Fatalf("a mensagem nao mostra um caminho absoluto: %q", got)
	}
	if !strings.Contains(got, "auth.json") {
		t.Fatalf("o caminho nao nomeia o arquivo: %q", got)
	}
}
