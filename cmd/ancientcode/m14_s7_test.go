package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antonioghizidev/ancientcode/internal/creds"
)

// S7a: auth.json e state.json nunca podem entrar no repositorio.
//
// O teste le `git ls-files` de verdade em vez de confiar no .gitignore: o
// .gitignore so afeta o que NAO esta rastreado, e um arquivo ja rastreado
// continua no historico mesmo depois de a regra aparecer.
func TestCredentialStoresAreNotTracked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git nao disponivel neste ambiente")
	}
	out, err := exec.Command("git", "ls-files").Output()
	if err != nil {
		t.Skipf("git ls-files falhou: %v", err)
	}
	for _, name := range strings.Fields(string(out)) {
		base := filepath.Base(name)
		if base == "auth.json" || base == "state.json" {
			t.Errorf("%s esta RASTREADO no repositorio (S7)", name)
		}
	}
}

// S7b: os dois arquivos tem de ficar fora de code_root, senao o modelo os le
// como arquivo do projeto — o que colocaria a chave no prompt.
func TestStoresAreOutsideCodeRootByDefault(t *testing.T) {
	credPath, err := creds.DefaultPath()
	if err != nil {
		t.Skipf("nao consegui resolver o home: %v", err)
	}
	// O store vive sob ~/.local/share, nunca sob o diretorio de trabalho.
	if strings.Contains(credPath, "/ancientcode/") && strings.Contains(credPath, "code_root") {
		t.Fatalf("o store cai dentro de code_root: %s", credPath)
	}
	if !strings.HasSuffix(credPath, filepath.Join(".local", "share", "ancientcode", "auth.json")) {
		t.Fatalf("caminho inesperado para o store: %s", credPath)
	}
}
