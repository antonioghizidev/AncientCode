package main

import (
	"os"
	"os/exec"
	"path/filepath"
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

// encTestIsInRootPackage is a guard: this file lives in cmd/ancientai, which is a
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

func underAnyRoot(rel string, roots []string) bool {
	r := filepath.ToSlash(rel)
	for _, root := range roots {
		root = filepath.ToSlash(root)
		if r == root || strings.HasPrefix(r, root+"/") {
			return true
		}
	}
	return false
}
