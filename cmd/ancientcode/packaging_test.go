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

// TestReleaseWorkflowConfiguresGitIdentity trava um defeito que so' aparece no
// runner, nunca no dev machine: `git tag -a` grava um TAGGER no objeto da tag,
// e o runner do GitHub nao tem user.name/user.email configurados. O passo
// morria com "Committer identity unknown" e a tag nem era criada - o job
// construia tudo e falhava no primeiro passo.
//
// Medido: num HOME limpo, com GIT_CONFIG_GLOBAL vazia,
// `git tag -a v0.0.1 -m ... HEAD` responde
// "fatal: empty ident name ... not allowed" e sai com 128.
func TestReleaseWorkflowConfiguresGitIdentity(t *testing.T) {
	root := installScriptRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("release.yml nao encontrado: %v", err)
	}
	s := string(raw)

	// Comparar LINHAS, nao posicoes no arquivo: o comentario que explica o
	// defeito cita `git tag -a`, e um strings.Index ingenuo acha a mencao na
	// prosa antes de achar o comando. So' conta linha cujo conteudo comeca
	// pelo comando (comentario comeca com '#').
	lineOf := func(prefix string) int {
		for i, line := range strings.Split(s, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), prefix) {
				return i
			}
		}
		return -1
	}

	tagAt := lineOf("git tag -a")
	if tagAt < 0 {
		t.Fatal("o workflow nao cria tag anotada: este teste ficou sem objeto")
	}

	for _, want := range []string{"git config user.name", "git config user.email"} {
		at := lineOf(want)
		if at < 0 {
			t.Fatalf("o workflow nao configura %q: num runner sem identidade, "+
				"git tag -a morre com \"Committer identity unknown\"", want)
		}
		if at > tagAt {
			t.Fatalf("%q aparece DEPOIS de git tag -a: a tag seria criada antes "+
				"de a identidade existir", want)
		}
	}
}

// pinnedDefaultVersion le a versao fixada do install.sh - a que o link estavel
// do README instala.
func pinnedDefaultVersion(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(installScriptRoot(t), "install.sh"))
	if err != nil {
		t.Fatalf("install.sh: %v", err)
	}
	m := regexp.MustCompile(`ANCIENTCODE_DEFAULT_VERSION:-([^}"]*)}`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("install.sh nao tem ANCIENTCODE_DEFAULT_VERSION")
	}
	return m[1]
}

// TestReleaseWorkflowChecksPinnedVersion e' o portao que faltava quando a
// v0.16.1 saiu: a tag apontava para o codigo novo, mas o install.sh continuava
// fixando v0.16.0, e a pagina da release mandava instalar a versao anterior.
//
// O teste e' de CALL SITE (D-75): o script pode estar certo e nunca ser chamado.
func TestReleaseWorkflowChecksPinnedVersion(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(installScriptRoot(t), ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("release.yml: %v", err)
	}
	s := string(raw)
	if !strings.Contains(s, "scripts/check-release-version.sh") {
		t.Fatal("o workflow de release nao confere a versao fixada do install.sh: " +
			"a release pode sair mandando instalar a versao anterior")
	}
	// So' para tag NOVA: re-gerar uma release antiga a partir da tag nao pode
	// ser bloqueado por um pin que ja' e' historia.
	if !strings.Contains(s, "steps.tag.outputs.created") {
		t.Fatal("o workflow confere a versao fixada sem distinguir tag nova de " +
			"existente: re-gerar uma release antiga passaria a falhar")
	}
}

// TestCheckReleaseVersionScript exercita o portao de verdade, contra o
// install.sh real: aceita a propria versao fixada, recusa outra tag estavel e
// deixa prerelease passar (o canal fixo continua na ultima estavel).
func TestCheckReleaseVersionScript(t *testing.T) {
	script := filepath.Join(installScriptRoot(t), "scripts", "check-release-version.sh")
	pin := pinnedDefaultVersion(t)

	if out, err := shellExec(t, "sh '"+shellPath(t, script)+"' '"+pin+"'"); err != nil {
		t.Fatalf("o portao recusou a propria versao fixada (%s): %v\n%s", pin, err, out)
	}
	if out, err := shellExec(t, "sh '"+shellPath(t, script)+"' 'v0.0.0'"); err == nil {
		t.Fatalf("o portao aceitou uma tag estavel diferente da fixada:\n%s", out)
	}
	if out, err := shellExec(t, "sh '"+shellPath(t, script)+"' 'v0.0.0-pre'"); err != nil {
		t.Fatalf("o portao recusou uma prerelease, que nao precisa ser a fixada: %v\n%s", err, out)
	}
}
