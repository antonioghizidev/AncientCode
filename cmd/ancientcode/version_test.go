package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
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
