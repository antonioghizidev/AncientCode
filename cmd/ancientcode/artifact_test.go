package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// vexRe pega mnemonicos com codificacao VEX (prefixo V): sao as instrucoes
// AVX/AVX2. A codificacao VEX e' justamente o que o Core 2 Duo nao executa -
// ele quebra com #UD no meio do comando, sem aviso.
var vexRe = regexp.MustCompile(`^V[A-Z0-9]`)

// runGoTool roda uma ferramenta do Go dentro do ambiente certo: o WSL no host
// de desenvolvimento Windows, nativo nos demais. O tool do Go e' usado em vez
// de binutils porque nao depende de nada instalado alem do proprio Go.
func runGoTool(t *testing.T, args ...string) string {
	t.Helper()
	var cmd *exec.Cmd
	// `go version` nao e' `go tool version`: sao comandos diferentes, e o
	// segundo responde "no such tool".
	base := []string{"/usr/local/go/bin/go"}
	native := []string{"go"}
	if len(args) > 0 && args[0] != "version" {
		base = append(base, "tool")
		native = append(native, "tool")
	}
	if runtime.GOOS == "windows" {
		full := append(base, args...)
		cmd = exec.Command("wsl", append([]string{"-d", "Ubuntu-24.04", "--"}, full...)...)
	} else {
		full := append(native, args...)
		cmd = exec.Command(full[0], full[1:]...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go tool %v falhou: %v\n%s", args, err, out)
	}
	return string(out)
}

// buildReleaseArtifact compila o artefato com os flags de alvo da release e
// devolve o caminho (em notacao WSL quando o host e' Windows).
func buildReleaseArtifact(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "ancientcode")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	// Mesmos flags de alvo da release: CGO, GOOS, GOARCH e, sobretudo,
	// GOAMD64=v1 - que e' o que determina o codigo gerado.
	//
	// Sem "-s -w", ao contrario da release: esses flags removem a tabela de
	// simbolos, e um binario sem simbolos nao pode ser disassemblado ("no symbol
	// section"). Eles nao mudam codegen - so removem simbolo e DWARF -, entao a
	// checagem continua valendo para o artefato publicado.
	build := exec.Command("go", "build", "-trimpath", "-o", bin, ".")
	build.Env = append(os.Environ(),
		"CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64", "GOAMD64=v1")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build do artefato v1 falhou: %v\n%s", err, out)
	}
	if runtime.GOOS == "windows" {
		return wslPath(t, bin)
	}
	return bin
}

func requireGoToolchain(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if err := exec.Command("wsl", "-d", "Ubuntu-24.04", "--", "/bin/sh", "-c", "echo ok").Run(); err != nil {
			t.Skipf("WSL indisponivel: %v", err)
		}
		return
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go indisponivel")
	}
}

// TestReleaseArtifactDeclaresV1Target (spec M16 S6, parte 1) e' o teste que
// pega o erro real: o workflow de release compilar com o GOAMD64 errado.
//
// Medido nesta sessao: v1 e v3 produzem EXATAMENTE as mesmas instrucoes no
// codigo deste projeto - zero VEX nos dois. Um teste que so varresse
// instrucoes passaria com o build errado, e eu teria escrito que ele protegia
// algo que nao protegia. O que distingue v1 de v3 aqui nao e' o codigo: e' o
// metadado de build, que o Go grava no binario.
func TestReleaseArtifactDeclaresV1Target(t *testing.T) {
	if testing.Short() {
		t.Skip("compila o artefato")
	}
	requireGoToolchain(t)

	bin := buildReleaseArtifact(t)
	out := runGoTool(t, "version", "-m", bin)

	for _, want := range []string{"GOAMD64=v1", "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64"} {
		if !strings.Contains(out, want) {
			t.Fatalf("build info sem %q; o artefato nao casa com o alvo (constitution 7).\n%s", want, out)
		}
	}
}

// TestReleaseArtifactHasNoAVXInProjectCode (spec M16 S6, parte 2) cobre o
// outro risco: assembly escrito a mao no codigo do projeto, que o compilador
// nao pode vetar porque o autor pediu.
//
// Escopo deliberado: as FUNCOES DESTE MODULO, nao o binario inteiro. O binario
// v1 contem ~40 instrucoes VEX, todas do assembly da stdlib (crypto/aes,
// crypto/sha256), guardado por deteccao de CPU em runtime (internal/cpu) - e so
// executa onde a instrucao existe. Isso e' seguro por construcao. Um teste que
// exigisse "zero VEX no artefato" falharia por causa da stdlib sem apontar
// defeito nosso, e a exigencia ja seria falsa antes deste milestone.
func TestReleaseArtifactHasNoAVXInProjectCode(t *testing.T) {
	if testing.Short() {
		t.Skip("compila e disassembly o artefato")
	}
	requireGoToolchain(t)

	bin := buildReleaseArtifact(t)
	text := strings.ReplaceAll(runGoTool(t, "objdump",
		"-s", `^github\.com/antonioghizidev/ancientcode`, bin), "\t", "|")

	var vex []string
	funcs := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "TEXT ") {
			funcs++
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}
		if fields := strings.Fields(parts[3]); len(fields) > 0 && vexRe.MatchString(fields[0]) {
			vex = append(vex, fields[0])
		}
	}

	if funcs == 0 {
		t.Fatalf("nenhuma funcao do modulo foi disassemblada: o filtro do objdump pode ter mudado de sintaxe (%d linhas)", len(text))
	}
	if len(vex) > 0 {
		sort.Strings(vex)
		t.Fatalf("codigo do projeto compilado com instrucao VEX/AVX, que quebra o Core 2 Duo sem aviso: %v", vex)
	}
	t.Logf("ok: %d funcoes do modulo disassembladas, nenhuma instrucao VEX", funcs)
}
