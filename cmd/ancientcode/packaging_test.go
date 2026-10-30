package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestReleaseWorkflowUsesSharedPackager e' um teste de CALL SITE, e existe por
// um motivo especifico: o defeito que ele trava nao estava em nenhum helper.
//
// O workflow montava o tarball por conta propria (tar -czf ...
// ancientcode_<tag>_linux_amd64, com diretorio de topo) e o teste montava o
// dele por outro caminho (tar -czf ... ancientcode, plano). Cada um estava
// "certo" sozinho; a divergencia entre os dois e' que era o bug. Nenhuma
// mutacao no empacotador pegaria isso, porque o erro morava no chamador - a
// mesma armadilha registrada em D-75.
//
// Por isso o workflow agora chama scripts/package-release.sh, o mesmo script
// que buildFakeRelease usa. Este teste impede que o workflow volte a empacotar
// por conta propria.
func TestReleaseWorkflowUsesSharedPackager(t *testing.T) {
	root := installScriptRoot(t)
	wf := filepath.Join(root, ".github", "workflows", "release.yml")
	raw, err := os.ReadFile(wf)
	if err != nil {
		t.Fatalf("release.yml nao encontrado: %v", err)
	}
	s := string(raw)

	if !strings.Contains(s, "scripts/package-release.sh") {
		t.Fatal("o workflow de release nao chama scripts/package-release.sh: " +
			"o empacotamento voltou a ser duplicado, e o layout do tarball pode " +
			"divergir do que o install.sh espera")
	}

	// Um `run:` que chame tar diretamente no workflow significa um segundo
	// empacotador de novo. O tar pode existir no script; aqui, nao.
	bareTar := regexp.MustCompile(`(?m)^\s*run:.*\btar\s+-`)
	if bareTar.MatchString(s) {
		t.Fatal("o workflow de release voltou a montar o tarball na mao: " +
			"use scripts/package-release.sh, ou o layout vai divergir do teste")
	}

	// O `workflow_dispatch` tem github.ref = refs/heads/main. A
	// action-gh-release deriva a tag de github.ref e, quando o ref nao e'
	// refs/tags/*, fica com tag vazia: sem `tag_name` explicito, o botao "Run
	// workflow" constroi o artefato e falha ao publicar. Este e' o caminho que
	// o projeto usa para publicar, entao ele precisa estar travado.
	if !strings.Contains(s, "tag_name: ${{ steps.tag.outputs.tag }}") {
		t.Fatal("o passo de release nao fixa tag_name: num workflow_dispatch a " +
			"action fica sem tag (github.ref = refs/heads/main) e nao publica nada")
	}

	script := filepath.Join(root, "scripts", "package-release.sh")
	body, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("scripts/package-release.sh nao existe: %v", err)
	}
	// O tarball tem que ser plano: o install.sh procura <extraido>/ancientcode,
	// nao <extraido>/<algum diretorio>/ancientcode.
	if !strings.Contains(string(body), `-C "$DIR" .`) {
		t.Fatal("package-release.sh deixou de gerar um tarball plano " +
			`(esperado: tar -czf ... -C "$DIR" .): o install.sh vai recusar a release`)
	}
}
