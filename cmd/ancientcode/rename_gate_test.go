package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Gate de consistência do nome (rename AncientAI -> AncientCode).
//
// Um rename de 90 arquivos é o tipo de mudança que "parece pronta" porque o build
// passa e a suíte fica verde: o que sobra é um nome velho em comentário de docs,
// num default de config ou no banner. Nenhum desses quebra nada — só confunde.
// Este teste falha se sobrar o nome antigo em arquivo versionado.
//
// Tolerância deliberada: .gitignore mantem as duas entradas do binário antigo, e
// DECISOES/session notes citam o nome antigo como histórico do próprio rename. Ver
// renameAllowlist.

// renameAllowlist: arquivo -> motivo. Entradas aqui são intencionais.
var renameAllowlist = map[string]string{
	".gitignore": "mantem /ancientai e /ancientai.exe para nao deixar um binario antigo " +
		"ser comitado por engano depois do rename",
	// A dica de erro cita o nome antigo DE PROPÓSITO: quem ainda tem
	// ancientai.yaml precisa saber que o arquivo deixou de ser lido. O gate
	// permite, mas nao perde o sinal: se o resto do projeto for limpo e so
	// sobrar essa linha, o comentario e o unico lugar que importa.
	"cmd/ancientcode/main.go": "a dica de erro de config cita ancientai.yaml de proposito, " +
		"para o operador com o arquivo antigo entender por que a config parou de ser lida",
}

// renameDocsCiteOldName are docs that must keep the old name because they RECORD
// the rename (or predate it). Editing them would rewrite history.
var renameDocsCiteOldName = map[string]bool{
	"docs/DECISOES.md": true,
}

// renameForbidden is the old name in any casing, plus the old module path.
var renameForbidden = []string{"ancientai", "AncientAI", "ANCIENTAI"}

func TestNoStaleOldNameInVersionedFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("varre a arvore versionada")
	}
	root, err := repoRoot()
	if err != nil {
		t.Skipf("raiz do repo nao encontrada: %v", err)
	}
	files := versionedFiles(t, root)
	if len(files) == 0 {
		t.Skip("nenhum arquivo versionado encontrado")
	}
	checked := 0
	for _, rel := range files {
		slashed := filepath.ToSlash(rel)
		if reason, ok := renameAllowlist[slashed]; ok {
			t.Logf("permitido: %s (%s)", slashed, reason)
			continue
		}
		if renameDocsCiteOldName[slashed] {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("%s: leitura falhou: %v", rel, err)
			continue
		}
		checked++
		s := string(raw)
		for _, bad := range renameForbidden {
			idx := strings.Index(s, bad)
			if idx < 0 {
				continue
			}
			lo := idx - 50
			if lo < 0 {
				lo = 0
			}
			hi := idx + 50
			if hi > len(s) {
				hi = len(s)
			}
			line := 1 + strings.Count(s[:idx], "\n")
			t.Errorf("%s:%d: sobrou o nome antigo %q: %q",
				slashed, line, bad, strings.ReplaceAll(s[lo:hi], "\n", " "))
		}
	}
	if checked == 0 {
		t.Fatal("nenhum arquivo verificado: o gate passaria vazio, o que e pior que nao existir")
	}
	t.Logf("%d arquivos verificados sem nome antigo", checked)
}

// TestModulePathMatchesRepoLock the go.mod module path to the canonical GitHub
// path. A rename exists partly so `go install github.com/antonioghizidev/
// ancientcode@latest` works; a module path that is not resolvable remotely makes
// that impossible and is exactly the drift this test catches.
func TestModulePathMatchesRepo(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Skipf("raiz do repo nao encontrada: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Skipf("go.mod nao encontrado: %v", err)
	}
	want := "module github.com/antonioghizidev/ancientcode"
	got := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "module ") {
			got = strings.TrimSpace(line)
			break
		}
	}
	if got != want {
		t.Errorf("go.mod: %q, esperado %q", got, want)
	}
}

// TestConfigDefaultFilenameMatchesBinaryName keeps the default config filename
// and the binary name in sync. They drifted apart silently before.
func TestConfigDefaultFilenameMatchesBinaryName(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Skipf("raiz do repo nao encontrada: %v", err)
	}
	main := filepath.Join(root, "cmd", "ancientcode", "main.go")
	raw, err := os.ReadFile(main)
	if err != nil {
		t.Skipf("main.go nao encontrado: %v", err)
	}
	s := string(raw)
	if !strings.Contains(s, `flag.String("config", "ancientcode.yaml"`) {
		t.Errorf("o --config default deveria ser ancientcode.yaml")
	}
	// O exemplo distribuido precisa existir com esse nome, ou a dica do erro de
	// config aponta para um arquivo que nao esta no repo.
	example := filepath.Join(root, "ancientcode.example.yaml")
	if _, err := os.Stat(example); err != nil {
		t.Errorf("ancientcode.example.yaml ausente: %v", err)
	}
}

// versionedTextFiles is a local list so this test does not depend on the helper
// living next to it.
func versionedTextFiles(t *testing.T, root string) []string {
	t.Helper()
	out := exec.Command("git", "-C", root, "ls-files")
	raw, err := out.Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, rel := range strings.Split(string(raw), "\n") {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		if hasAnySuffix(rel, []string{".go", ".md", ".yaml", ".yml", ".tmpl", ".gitignore"}) {
			files = append(files, filepath.FromSlash(rel))
		}
	}
	return files
}
