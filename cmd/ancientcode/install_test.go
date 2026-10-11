// Testes de seguranca do install.sh (spec M16, casos S1-S5).
//
// A regra de qualidade do projeto vale aqui com o mesmo peso do Go: o teste
// tem que FALHAR se a protecao for removida. Por isso cada caso injeta o
// ataque de verdade (checksum adulterado, checksum ausente, plataforma errada)
// contra o script real, executado num shell de verdade - nao uma reimplementacao
// em Go, que testaria a reimplementacao.
//
// Rodam no WSL: o script e /bin/sh e precisa de um shell Linux.
package main

import (
	"os"
	"sort"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// wslPrefixtranslate um caminho Windows para o que o WSL ve (/mnt/c/...).
func wslPath(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	vol := strings.ToUpper(abs[:1])
	rest := strings.ReplaceAll(abs[3:], `\`, "/")
	return "/mnt/" + strings.ToLower(vol) + "/" + rest
}

// installScriptRoot localiza o install.sh na raiz do repositorio. Nao chama a
// repoRoot() do encoding_gate_test.go porque aquela devolve dois valores e mora
// num arquivo que pode mudar; aqui o caminho e fixo e explicito.
func installScriptRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Dir(filepath.Dir(wd)) // cmd/ancientcode -> repo
}

// runInstall executa o install.sh real no WSL com o ambiente controlado.
//
// O ambiente vai NA LINHA DE COMANDO, e nao em exec.Cmd.Env: o WSL so repassa
// ao Linux as variaveis listadas em WSLENV, e um Cmd.Env setado aqui chega
// vazio dentro do shell. A primeira versao deste teste "passava" porque o
// instalador consultava a internet de verdade e recebia 404 - a protecao nao
// estava sendo exercitada, so o DNS estava.
func runInstall(t *testing.T, env map[string]string, script string) (stdout string, exitCode int) {
	t.Helper()
	var exports strings.Builder
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		exports.WriteString("export " + k + "='" + strings.ReplaceAll(env[k], "'", `'\''`) + "'; ")
	}
	full := exports.String() + script

	sh := exec.Command("wsl", "-d", "Ubuntu-24.04", "--", "sh", "-c", full)
	out, err := sh.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("wsl falhou: %v\n%s", err, out)
	}
	return string(out), code
}

// buildFakeRelease monta um "release" local com o MESMO layout que o GitHub
// usa: <base>/<tag>/<artefato> e <base>/<tag>/SHA256SUMS. O teste precisa
// respeitar esse desenho, ou exercita um caminho que o instalador nunca usa.
func buildFakeRelease(t *testing.T, tag string, tamper bool) string {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, tag)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(root, "ancientcode")
	// Um binario falso basta: o instalador nao executa o conteudo, ele extrai.
	if err := os.WriteFile(payload, []byte("#!/bin/sh\necho ancientcode\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	asset := "ancientcode_" + tag + "_linux_amd64.tar.gz"
	cmd := exec.Command("wsl", "-d", "Ubuntu-24.04", "--", "sh", "-c",
		"cd '"+wslPath(t, root)+"' && tar -czf '"+wslPath(t, filepath.Join(root, asset))+"' ancientcode")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tar falhou: %v\n%s", err, out)
	}
	sum := exec.Command("wsl", "-d", "Ubuntu-24.04", "--", "sh", "-c",
		"cd '"+wslPath(t, root)+"' && sha256sum "+asset)
	sumOut, err := sum.Output()
	if err != nil {
		t.Fatalf("sha256sum falhou: %v", err)
	}
	line := strings.TrimSpace(string(sumOut))
	if tamper {
		// S1: checksum que nao corresponde ao conteudo.
		fields := strings.Fields(line)
		line = strings.Repeat("0", 64) + "  " + fields[1]
	}
	if err := os.WriteFile(filepath.Join(root, "SHA256SUMS"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return base
}

// TestInstallRefusesTamperedChecksum (S1) e o teste central: se o checksum
// divergir, o instalador tem que recusar E NAO deixar binario instalado.
func TestInstallRefusesTamperedChecksum(t *testing.T) {
	rel := buildFakeRelease(t, "v0.0.1-teste", true)
	prefix := t.TempDir()
	out, code := runInstall(t, map[string]string{
		"PATH":                   "/usr/bin:/bin",
		"HOME":                   wslPath(t, t.TempDir()),
		"ANCIENTCODE_VERSION":    "v0.0.1-teste",
		"ANCIENTCODE_PREFIX":     wslPath(t, prefix),
		"ANCIENTCODE_LOCAL_ASSET": wslPath(t, filepath.Join(rel, "v0.0.1-teste", "ancientcode_v0.0.1-teste_linux_amd64.tar.gz")),
		"ANCIENTCODE_RELEASE_BASE": "file://" + wslPath(t, rel),
		"ANCIENTCODE_RAW_BASE":   "file://" + wslPath(t, rel),
	}, "cat '"+wslPath(t, installScriptRoot(t))+"/install.sh' | sh")

	if code == 0 {
		t.Fatalf("instalador aceitou checksum divergente:\n%s", out)
	}
	if !strings.Contains(out, "checksum divergente") {
		t.Fatalf("recusa pela razao errada:\n%s", out)
	}
	// Nada pode ter sido escrito no prefixo.
	entries, _ := os.ReadDir(prefix)
	if len(entries) != 0 {
		t.Fatalf("instalou apesar do checksum divergente: %v", entries)
	}
}

// TestInstallRefusesMissingChecksumFile (S2): sem SHA256SUMS, nao ha o que
// verificar, e "sem checksum" e erro - nunca instalacao sem verificacao.
func TestInstallRefusesMissingChecksumFile(t *testing.T) {
	rel := buildFakeRelease(t, "v0.0.2-teste", false)
	if err := os.Remove(filepath.Join(rel, "v0.0.2-teste", "SHA256SUMS")); err != nil {
		t.Fatal(err)
	}
	prefix := t.TempDir()
	out, code := runInstall(t, map[string]string{
		"PATH": "/usr/bin:/bin",
		"HOME": wslPath(t, t.TempDir()),
		"ANCIENTCODE_VERSION": "v0.0.2-teste",
		"ANCIENTCODE_PREFIX":  wslPath(t, prefix),
		"ANCIENTCODE_RELEASE_BASE": "file://" + wslPath(t, rel),
		"ANCIENTCODE_RAW_BASE": "file://" + wslPath(t, rel),
	}, "cat '"+wslPath(t, installScriptRoot(t))+"/install.sh' | sh")

	if code == 0 {
		t.Fatalf("instalador aceitou ausência de SHA256SUMS:\n%s", out)
	}
	entries, _ := os.ReadDir(prefix)
	if len(entries) != 0 {
		t.Fatalf("instalou sem checksum: %v", entries)
	}
}

// TestInstallRefusesForeignChecksumEntry (S4): o SHA256SUMS precisa ter uma
// entrada para o artefato DESTA versao. Um arquivo que so cita outro nome nao
// pode ser aceito - e um arquivo com caminho "../../etc/passwd" tambem nao
// pode ser lido (da para onde o `sha256sum -c` apontaria).
func TestInstallRefusesForeignChecksumEntry(t *testing.T) {
	rel := buildFakeRelease(t, "v0.0.4-teste", false)
	sum := strings.Repeat("0", 64) + "  ancientcode_v9.9.9-outra_linux_amd64.tar.gz\n"
	if err := os.WriteFile(filepath.Join(rel, "v0.0.4-teste", "SHA256SUMS"), []byte(sum), 0o644); err != nil {
		t.Fatal(err)
	}
	prefix := t.TempDir()
	out, code := runInstall(t, map[string]string{
		"PATH": "/usr/bin:/bin",
		"HOME": wslPath(t, t.TempDir()),
		"ANCIENTCODE_VERSION": "v0.0.4-teste",
		"ANCIENTCODE_PREFIX":  wslPath(t, prefix),
		"ANCIENTCODE_LOCAL_ASSET": wslPath(t, filepath.Join(rel, "v0.0.4-teste", "ancientcode_v0.0.4-teste_linux_amd64.tar.gz")),
		"ANCIENTCODE_RELEASE_BASE": "file://" + wslPath(t, rel),
		"ANCIENTCODE_RAW_BASE": "file://" + wslPath(t, rel),
	}, "cat '"+wslPath(t, installScriptRoot(t))+"/install.sh' | sh")

	if code == 0 {
		t.Fatalf("aceitou SHA256SUMS sem entrada para o artefato desta versao:\n%s", out)
	}
	if !strings.Contains(out, "nao contem uma entrada") {
		t.Fatalf("recusa pela razao errada:\n%s", out)
	}
}

// TestInstallRefusesPathTraversalInSums (S4, endurecimento): uma entrada com
// ".." nao pode ser obedecida. Um `sha256sum -c` leria o arquivo apontado e
// poderia devolver 0, o que transformaria a verificacao em decoracao.
func TestInstallRefusesPathTraversalInSums(t *testing.T) {
	rel := buildFakeRelease(t, "v0.0.5-teste", false)
	asset := "ancientcode_v0.0.5-teste_linux_amd64.tar.gz"
	sum := strings.Repeat("0", 64) + "  ../../etc/passwd\n" +
		strings.Repeat("0", 64) + "  " + asset + "\n"
	if err := os.WriteFile(filepath.Join(rel, "v0.0.5-teste", "SHA256SUMS"), []byte(sum), 0o644); err != nil {
		t.Fatal(err)
	}
	prefix := t.TempDir()
	_, code := runInstall(t, map[string]string{
		"PATH": "/usr/bin:/bin",
		"HOME": wslPath(t, t.TempDir()),
		"ANCIENTCODE_VERSION": "v0.0.5-teste",
		"ANCIENTCODE_PREFIX":  wslPath(t, prefix),
		"ANCIENTCODE_LOCAL_ASSET": wslPath(t, filepath.Join(rel, "v0.0.5-teste", asset)),
		"ANCIENTCODE_RELEASE_BASE": "file://" + wslPath(t, rel),
		"ANCIENTCODE_RAW_BASE": "file://" + wslPath(t, rel),
	}, "cat '"+wslPath(t, installScriptRoot(t))+"/install.sh' | sh")

	if code == 0 {
		t.Fatal("aceitou SHA256SUMS com caminho de traversal")
	}
}

// TestInstallRefusesNonLinuxPlatform (S3, o outro lado): a recusa de plataforma
// tem duas pernas - sistema e arquitetura. A mutacao que remove a checagem de
// SO nao e vista pelo teste de arquitetura, e vice-versa; sem os dois, metade da
// protecao nao tem teste.
func TestInstallRefusesNonLinuxPlatform(t *testing.T) {
	rel := buildFakeRelease(t, "v0.0.7-teste", false)
	prefix := t.TempDir()
	out, code := runInstall(t, map[string]string{
		"PATH": "/usr/bin:/bin",
		"HOME": wslPath(t, t.TempDir()),
		"ANCIENTCODE_VERSION":    "v0.0.7-teste",
		"ANCIENTCODE_PREFIX":     wslPath(t, prefix),
		"ANCIENTCODE_LOCAL_ASSET": wslPath(t, filepath.Join(rel, "v0.0.7-teste", "ancientcode_v0.0.7-teste_linux_amd64.tar.gz")),
		"ANCIENTCODE_RELEASE_BASE": "file://" + wslPath(t, rel),
		"ANCIENTCODE_RAW_BASE": "file://" + wslPath(t, rel),
		"ANCIENTCODE_FAKE_OS":   "Darwin",
		"ANCIENTCODE_FAKE_ARCH": "x86_64",
	}, "cat '"+wslPath(t, installScriptRoot(t))+"/install.sh' | sh")

	if code == 0 {
		t.Fatalf("instalador aceitou Darwin:\n%s", out)
	}
	if !strings.Contains(out, "so linux/amd64") {
		t.Fatalf("recusa pela razao errada:\n%s", out)
	}
	entries, _ := os.ReadDir(prefix)
	if len(entries) != 0 {
		t.Fatalf("instalou em plataforma nao suportada: %v", entries)
	}
}

// TestInstallRefusesUnsupportedArch (S3, arquitetura): o par (os, arch) tem duas
// pernas e ambas precisam de teste - remover so a checagem de SO nao e vista
// pelo teste de arquitetura, e vice-versa.
func TestInstallRefusesUnsupportedArch(t *testing.T) {
	rel := buildFakeRelease(t, "v0.0.8-teste", false)
	prefix := t.TempDir()
	out, code := runInstall(t, map[string]string{
		"PATH": "/usr/bin:/bin",
		"HOME": wslPath(t, t.TempDir()),
		"ANCIENTCODE_VERSION":    "v0.0.8-teste",
		"ANCIENTCODE_PREFIX":     wslPath(t, prefix),
		"ANCIENTCODE_LOCAL_ASSET": wslPath(t, filepath.Join(rel, "v0.0.8-teste", "ancientcode_v0.0.8-teste_linux_amd64.tar.gz")),
		"ANCIENTCODE_RELEASE_BASE": "file://" + wslPath(t, rel),
		"ANCIENTCODE_RAW_BASE": "file://" + wslPath(t, rel),
		"ANCIENTCODE_FAKE_OS":   "Linux",
		"ANCIENTCODE_FAKE_ARCH": "aarch64",
	}, "cat '"+wslPath(t, installScriptRoot(t))+"/install.sh' | sh")

	if code == 0 {
		t.Fatalf("instalador aceitou aarch64:\n%s", out)
	}
	if !strings.Contains(out, "so linux/amd64") {
		t.Fatalf("recusa pela razao errada:\n%s", out)
	}
	entries, _ := os.ReadDir(prefix)
	if len(entries) != 0 {
		t.Fatalf("instalou em plataforma nao suportada: %v", entries)
	}
}

// TestInstallVerifiesBeforeExtractingOrder (S5, estrutural).
//
// A propriedade do S5 e uma ORDEM: nada do artefato pode ser extraido antes de
// o checksum passar. Um teste comportamental com canario seria o ideal, mas o
// GNU tar remove o "/" inicial ao extrair (sem -P), o que faz o caminho
// absoluto do payload malicioso cair dentro do diretorio de trabalho - o canario
// nunca aparece e o teste passa com a protecao REMOVIDA. Ja tentado: o teste
// comportamental "verde" aqui era cego.
//
// Este teste afirma a ordem no texto do script. E mais fraco que um
// comportamental, e o motivo esta escrito: ele cobre a reordenacao, que e a
// unica forma de quebrar a invariante sem mexer no codigo de verificacao - e
// essa forma ja e coberta pelos testes S1/S4, que sao comportamentais.
func TestInstallVerifiesBeforeExtractingOrder(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(installScriptRoot(t), "install.sh"))
	if err != nil {
		t.Fatalf("leitura do install.sh: %v", err)
	}
	s := string(b)
	verify := strings.Index(s, `|| die "checksum divergente`)
	extract := strings.Index(s, "tar -xzf")
	if verify < 0 || extract < 0 {
		t.Fatalf("nao encontrei os blocos (verify=%d extract=%d)", verify, extract)
	}
	if verify > extract {
		t.Fatalf("S5 quebrado: o checksum e conferido (pos %d) DEPOIS da extracao (pos %d)", verify, extract)
	}
}