package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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
//
// Este teste aponta TAMBEM `ANCIENTCODE_RELEASES_BASE` para file://. Sem isso a
// resolucao de `latest' consultava o GitHub de verdade: enquanto o repositorio
// era privado a consulta dava 404 e o teste passava pelo motivo errado; ao ficar
// publico, o redirect resolveu e o teste comecou a instalar. A armadilha e' a
// mesma que o comentario de runInstall descreve.
func TestInstallLatestResolvesTagFromRedirect(t *testing.T) {
	rel := buildFakeRelease(t, "v0.16.0", false)
	prefix := t.TempDir()
	out, code := runInstall(t, map[string]string{
		"PATH":                      "/usr/bin:/bin",
		"HOME":                      wslPath(t, t.TempDir()),
		"ANCIENTCODE_VERSION":       "latest",
		"ANCIENTCODE_PREFIX":        wslPath(t, prefix),
		"ANCIENTCODE_RELEASE_BASE":  "file://" + wslPath(t, rel),
		"ANCIENTCODE_RELEASES_BASE": "file://" + wslPath(t, rel),
		"ANCIENTCODE_RAW_BASE":      "file://" + wslPath(t, rel),
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

// TestInstallLatestFollowsRedirect e' o caminho positivo do `latest': um
// servidor que responde 302 em /releases/latest leva a /releases/tag/<TAG>, e o
// instalador tem de baixar o artefato daquela tag. Sem este teste, so' o caso
// negativo estava travado.
//
// Roda apenas no Linux: no Windows o instalador executa dentro do WSL, que nao
// alcanca o 127.0.0.1 do servidor httptest do host.
func TestInstallLatestFollowsRedirect(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("servidor httptest do host nao e' alcancavel do WSL; roda no Linux (CI)")
	}
	const tag = "v0.0.9-teste"
	rel := buildFakeRelease(t, tag, false)
	asset := "ancientcode_" + tag + "_linux_amd64.tar.gz"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			http.Redirect(w, r, "/releases/tag/"+tag, http.StatusFound)
		case "/releases/tag/" + tag:
			// O GitHub serve a pagina da tag com 200; `curl -f -L -I` segue o
			// redirect ate' aqui e falharia com 404 antes de ler a URL efetiva.
			w.WriteHeader(http.StatusOK)
		case "/releases/download/" + tag + "/" + asset:
			http.ServeFile(w, r, filepath.Join(rel, tag, asset))
		case "/releases/download/" + tag + "/SHA256SUMS":
			http.ServeFile(w, r, filepath.Join(rel, tag, "SHA256SUMS"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	prefix := t.TempDir()
	out, code := runInstall(t, map[string]string{
		"PATH":                      "/usr/bin:/bin",
		"HOME":                      shellPath(t, t.TempDir()),
		"ANCIENTCODE_VERSION":       "latest",
		"ANCIENTCODE_PREFIX":        shellPath(t, prefix),
		"ANCIENTCODE_RELEASES_BASE": srv.URL + "/releases",
		"ANCIENTCODE_RAW_BASE":      srv.URL,
	}, "cat '"+shellPath(t, installScriptRoot(t))+"/install.sh' | sh")

	if code != 0 {
		t.Fatalf("`latest' nao seguiu o redirect (exit %d):\n%s", code, out)
	}
	if !strings.Contains(out, "ultima release: "+tag) {
		t.Fatalf("nao anunciou a tag resolvida pelo redirect:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(prefix, "ancientcode")); err != nil {
		t.Fatalf("o binario nao foi instalado: %v\n%s", err, out)
	}
}

// TestInstallDefaultsToPinnedVersion: sem ANCIENTCODE_VERSION, o instalador
// instala a versao FIXADA, nao `latest'. Reprodutibilidade vale mais que
// conveniencia - rodar o instalador amanha tem que dar a mesma build de hoje.
//
// O pin NAO e' comparado com uma versao literal: ele muda a cada release (e o
// portao de release exige que mude, D-95). Cravar `v0.16.0' aqui quebrava o
// teste a cada bump sem testar nada de util - o que importa e' o default ser
// uma tag fixa, nao `latest'.
func TestInstallDefaultsToPinnedVersion(t *testing.T) {
	script, err := os.ReadFile(filepath.Join(installScriptRoot(t), "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(script)
	pin := pinnedDefaultVersion(t)
	if !regexp.MustCompile(`^v\d+\.\d+\.\d+`).MatchString(pin) {
		t.Fatalf("o default deixou de ser uma versao fixada: %q", pin)
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
