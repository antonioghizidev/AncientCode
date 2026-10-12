package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstallLatestResolvesTagFromRedirect (M16) cobre a correcao do `latest'.
//
// O bug: o instalador descobria a versao procurando um asset chamado
// "ancientcode_latest_linux_amd64.tar.gz". Esse asset NUNCA existiu - o workflow
// nomeia o arquivo com a tag. O caminho `latest' estava quebrado desde o
// primeiro dia e so apareceu quando a primeira release foi publicada de fato.
//
// A correcao resolve pela URL efetiva de /releases/latest, que o GitHub
// redireciona para /releases/tag/<TAG>.
func TestInstallLatestResolvesTagFromRedirect(t *testing.T) {
	rel := buildFakeRelease(t, "v0.16.0", false)
	prefix := t.TempDir()
	out, code := runInstall(t, map[string]string{
		"PATH":                   "/usr/bin:/bin",
		"HOME":                   wslPath(t, t.TempDir()),
		"ANCIENTCODE_VERSION":    "latest",
		"ANCIENTCODE_PREFIX":     wslPath(t, prefix),
		"ANCIENTCODE_RELEASE_BASE": "file://" + wslPath(t, rel),
		"ANCIENTCODE_RAW_BASE":   "file://" + wslPath(t, rel),
	}, "cat '"+wslPath(t, installScriptRoot(t))+"/install.sh' | sh")

	// Com file:// o redirect nao existe, entao `latest' NAO pode resolver: o
	// instalador tem de recusar em vez de instalar a versao errada.
	if code == 0 {
		t.Fatalf("`latest' resolveu contra um servidor sem redirect e instalou mesmo assim:\n%s", out)
	}
	entries, _ := os.ReadDir(prefix)
	if len(entries) != 0 {
		t.Fatalf("instalou apesar de nao conseguir resolver a versao: %v", entries)
	}
}

// TestInstallDefaultsToPinnedVersion: sem ANCIENTCODE_VERSION, o instalador
// instala a versao FIXADA, nao `latest'. Reprodutibilidade vale mais que
// conveniencia - rodar o instalador amanha tem que dar a mesma build de hoje.
func TestInstallDefaultsToPinnedVersion(t *testing.T) {
	script, err := os.ReadFile(filepath.Join(installScriptRoot(t), "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(script)
	if !strings.Contains(s, `ANCIENTCODE_DEFAULT_VERSION:-v0.16.0`) {
		t.Fatalf("o default deixou de ser uma versao fixada:\n%s", s[:200])
	}
	if strings.Contains(s, `WANT_VERSION="${ANCIENTCODE_VERSION:-latest}"`) {
		t.Fatal("o default voltou a ser `latest' - reprodutibilidade perdida")
	}
}

// TestInstallNeverProbesAnAssetNamedLatest: trava o bug pelo outro lado. O
// instalador nao pode voltar a tentar descobrir a versão pelo nome do asset,
// porque o nome do asset carrega a tag.
func TestInstallNeverProbesAnAssetNamedLatest(t *testing.T) {
	script, err := os.ReadFile(filepath.Join(installScriptRoot(t), "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(script), "ancientcode_latest_linux_amd64.tar.gz") {
		t.Fatal("o instalador voltou a sondar um asset chamado 'latest', que nunca existe")
	}
}