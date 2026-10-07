package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// Gate de encoding dos arquivos versionados (D-2026-10-02-62).
//
// A seção D-56..61 do DECISOES.md foi escrita com um here-string piped para
// Add-Content no PowerShell 5.1, que destruiu os acentos: 159 caracteres viraram
// U+FFFD ("sess?o", "Decis?o"). Nenhum outro gate pega isso — a suíte continua
// verde, o build passa, e o arquivo versionado é lixo para quem lê.
//
// Este teste é o portão: texto de .md/.go versionado tem de ser UTF-8 válido e
// não pode conter U+FFFD, que é o carimbo do PowerShell 5.1 ao converter texto.

// arquivos versionados que carregam texto legível por humanos.
var encSuffixes = []string{".md", ".go", ".yaml", ".yml"}

// encRoots limita a varredura aos diretórios de conteúdo e documentação.
var encRoots = []string{
	filepath.Join("docs"),
	filepath.Join("specs"),
	filepath.Join("cmd"),
	filepath.Join("internal"),
	// "" = a raiz do repositorio. Sem ela os gates tinham um ponto cego: README.md,
	// AGENTS.md e go.mod nunca eram verificados — e o README e o arquivo mais lido
	// do projeto. Descoberto ao injetar o nome antigo no README e ver o gate do
	// rename passar.
	"",
}

func TestVersionedTextIsValidUTF8WithoutReplacementChars(t *testing.T) {
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
	for _, rel := range files {
		p := filepath.Join(root, rel)
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("%s: leitura falhou: %v", rel, err)
			continue
		}
		if !utf8.Valid(raw) {
			t.Errorf("%s: bytes nao sao UTF-8 valido (encoding corrompido)", rel)
			continue
		}
		s := string(raw)
		if n := strings.Count(s, "\uFFFD"); n > 0 {
			// Amostra a primeira ocorrência com contexto: ela identifica o trecho.
			i := strings.Index(s, "\uFFFD")
			lo := i - 40
			if lo < 0 {
				lo = 0
			}
			hi := i + 40
			if hi > len(s) {
				hi = len(s)
			}
			t.Errorf("%s: %d caractere(s) U+FFFD (texto corrompido) perto de %q",
				rel, n, s[lo:hi])
		}
	}
}

// TestVersionedMarkdownHasNoRawHTMLTags fecha o segundo lado da lição do M13: um
// `<id>` solto no título de uma entrada do DECISOES.md fez o leitor de markdown
// do operador engolir tudo que vinha depois — bullets e o título seguinte
// passaram a aparecer como texto literal. O sintoma apontava para "formatação
// quebrada depois do título", mas o markdown estava correto: `<id>` é lido como
// tag HTML.
//
// Code spans (“ `--resume <id>` “) e blocos cercados são seguros; o que quebra
// é ângulo cru no texto corrido. Este gate exige que não exista.
func TestVersionedMarkdownHasNoRawHTMLTags(t *testing.T) {
	if testing.Short() {
		t.Skip("varre os .md versionados")
	}
	root, err := repoRoot()
	if err != nil {
		t.Skipf("raiz do repo nao encontrada: %v", err)
	}
	files := markdownFiles(t, root)
	if len(files) == 0 {
		t.Skip("nenhum .md versionado encontrado")
	}
	// <tag> cru no texto: nome começa por letra, para nao casar com "<= 90",
	// "->", "<-", "-> " ou comparacoes de Go/Python em prosa.
	re := regexp.MustCompile(`<[a-zA-Z][a-zA-Z0-9_-]*>`)
	fence := regexp.MustCompile("^\\s*(```|~~~)")
	for _, rel := range files {
		p := filepath.Join(root, rel)
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("%s: leitura falhou: %v", rel, err)
			continue
		}
		inFence := false
		for i, line := range strings.Split(string(raw), "\n") {
			if fence.MatchString(line) {
				inFence = !inFence
				continue
			}
			if inFence {
				continue // dentro de bloco cercado o HTML nao e interpretado
			}
			// Remove code spansinline: `` `--resume <id>` `` e seguro.
			prose := codeSpan.ReplaceAllString(line, "")
			if m := re.FindString(prose); m != "" {
				t.Errorf("%s:%d: tag HTML crua %q quebra o markdown a partir daqui: %s",
					rel, i+1, m, strings.TrimSpace(line))
			}
		}
	}
}

// codeSpan casa `...` (inclusive com ticks duplos), para o gate ignorar o que já
// está protegido por crase.
var codeSpan = regexp.MustCompile("`+[^`]*`+")

func markdownFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	for _, rel := range versionedFiles(t, root) {
		if strings.HasSuffix(rel, ".md") {
			files = append(files, rel)
		}
	}
	return files
}

// encTestIsInRootPackage is a guard: this file lives in cmd/ancientcode, which is a
// main package; repoRoot and versionedFiles are defined here so the gate has no
// hidden dependency on test helpers that may move.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", os.ErrNotExist
}

func versionedFiles(t *testing.T, root string) []string {
	t.Helper()
	out := exec.Command("git", "-C", root, "ls-files")
	raw, err := out.Output()
	if err != nil {
		t.Logf("git indisponivel (%v): usando varredura de diretorio", err)
		return walkFiles(root)
	}
	var files []string
	for _, rel := range strings.Split(string(raw), "\n") {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		if !hasAnySuffix(rel, encSuffixes) {
			continue
		}
		if !underAnyRoot(rel, encRoots) {
			continue
		}
		files = append(files, filepath.FromSlash(rel))
	}
	return files
}

func walkFiles(root string) []string {
	var files []string
	for _, r := range encRoots {
		base := filepath.Join(root, r)
		_ = filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			if hasAnySuffix(p, encSuffixes) {
				files = append(files, p)
			}
			return nil
		})
	}
	return files
}

func hasAnySuffix(s string, suffixes []string) bool {
	for _, x := range suffixes {
		if strings.HasSuffix(s, x) {
			return true
		}
	}
	return false
}

// underAnyRoot reports whether rel is inside one of the scanned roots.
//
// A root of "" means the repository root itself: files with NO slash in their
// path (README.md, AGENTS.md, go.mod). Without it the gates had a blind spot —
// they scanned docs/, specs/, cmd/ and internal/, and the most-read file in the
// repo (README.md) was never checked. Found by injecting the old name in README
// and watching the rename gate pass.
func underAnyRoot(rel string, roots []string) bool {
	r := filepath.ToSlash(rel)
	for _, root := range roots {
		root = filepath.ToSlash(root)
		if root == "" {
			if !strings.Contains(r, "/") {
				return true
			}
			continue
		}
		if r == root || strings.HasPrefix(r, root+"/") {
			return true
		}
	}
	return false
}
