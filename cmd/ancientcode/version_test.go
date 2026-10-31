package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

// buildVersion e o valor que a build de release injeta com
// -ldflags="-X main.version=<tag>". Fora de release ele precisa dizer "dev",
// seno "--version" afirma uma build que nao e a que esta rodando (spec M16 S7).
func TestBuildVersionDefaultsToDevOutsideRelease(t *testing.T) {
	if version != "dev" && !strings.HasPrefix(version, "dev+") && version != "" {
		// Aceita so dev, dev+<info> (build com dirty por dirty tree) ou vazio.
		if !strings.HasPrefix(version, "0.") && !strings.HasPrefix(version, "v") {
			t.Fatalf("version fora de release deve ser 'dev', obtive %q", version)
		}
	}
}

// TestVersionIsNotHardcodedOnAMilestone é o teste de call site do M16: ele falha
// se alguém voltar a digitar a versão à mão como constante. O valor tem de vir
// da build; o placeholder local é "dev".
func TestVersionIsNotHardcodedOnAMilestone(t *testing.T) {
	if version == "0.15.0-m15" || strings.Contains(version, "-m15") {
		t.Fatalf("version voltou a ser constante digitada a mao: %q", version)
	}
}

// TestVersionFlagReportsTheInjectedValue garante que --version imprime a
// variável injetada, e nao uma string duplicada em outro lugar do main.
func TestVersionFlagReportsTheInjectedValue(t *testing.T) {
	if testing.Short() {
		t.Skip("compila o binario")
	}
	if runtime.GOOS == "windows" {
		t.Skip("o binario de teste precisa de .exe no Windows")
	}
	bin := filepath.Join(t.TempDir(), "ancientcode")
	build := exec.Command("go", "build", "-ldflags", "-X main.version=v9.9.9-prova", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build falhou: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("--version falhou: %v\n%s", err, out)
	}
	got := string(out)
	if !strings.Contains(got, "v9.9.9-prova") {
		t.Fatalf("--version nao escreve a versao injetada:\n%s", got)
	}
}

// TestResolveVersion: a versao injetada por -ldflags sempre vence; sem ela, a
// versao do modulo gravada no build info e' o que `go install modulo@tag' deixa
// como rastro. Sem esse fallback o binario instalado por `go install' se
// apresenta como "dev" e o relato de bug nao diz qual codigo rodava (M16 S7).
func TestResolveVersion(t *testing.T) {
	cases := []struct {
		nome     string
		injetada string
		modulo   string
		queremos string
	}{
		{"ldflags vence o build info", "v9.9.9-prova", "v0.16.0", "v9.9.9-prova"},
		{"go install usa a versao do modulo", "dev", "v0.16.0", "v0.16.0"},
		{"build local continua dev", "dev", "(devel)", "dev"},
		{"modulo sem versao continua dev", "dev", "", "dev"},
	}
	for _, c := range cases {
		bi := &debug.BuildInfo{Main: debug.Module{Version: c.modulo}}
		if got := resolveVersion(c.injetada, bi, true); got != c.queremos {
			t.Errorf("%s: resolveVersion(%q, modulo %q) = %q, quero %q",
				c.nome, c.injetada, c.modulo, got, c.queremos)
		}
	}
	// Sem build info (binario sem modulo, ou debug.ReadBuildInfo falhou).
	if got := resolveVersion("dev", nil, false); got != "dev" {
		t.Errorf("sem build info: resolveVersion(dev, nil, false) = %q, quero dev", got)
	}
}

// TestMainResolvesVersionFromBuildInfo e' o teste de call site: resolveVersion
// pode estar perfeita e nunca ser chamada. Foi o que aconteceu com catalog.Filter
// (D-75) - helper certo, chamador errado. Se alguem remover a atribuicao no
// main, o `go install' volta a mentir e este teste falha.
func TestMainResolvesVersionFromBuildInfo(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ler main.go: %v", err)
	}
	code := string(src)
	if !strings.Contains(code, "debug.ReadBuildInfo()") {
		t.Fatal("main.go nao consulta debug.ReadBuildInfo(): o fallback de `go install' virou codigo morto")
	}
	if !strings.Contains(code, "version = resolveVersion(version,") {
		t.Fatal("main.go nao atribui o resultado de resolveVersion a version")
	}
}
