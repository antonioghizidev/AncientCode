package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A topologia real do operador — agente instalado num lugar, fonte em outro —
// hoje NAO SOBE: Validate exige code_root dentro de workdir. Este teste
// registra o contrato novo: as duas sao irmas.
func TestProjectRootMayLiveOutsideAgentHome(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "agente")
	proj := filepath.Join(base, "workspace", "projeto")
	for _, d := range []string{home, proj} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	c := &Config{
		Provider:    "openrouter",
		Model:       "m/x",
		BaseURL:     "https://exemplo.invalido/v1",
		APIKeyEnv:   "CHAVE",
		Workdir:     home,
		ProjectRoot: proj,
	}
	t.Setenv("CHAVE", "x")
	applyDefaults(c)
	if err := c.Validate(); err != nil {
		t.Fatalf("raiz do projeto fora da casa do agente deveria passar: %v", err)
	}
}

// O inverso tambem tem de passar: mesmo pai, como quem nunca configurou nada.
func TestProjectRootMayBeSiblingOfAgentHome(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "agente")
	proj := filepath.Join(base, "projeto")
	for _, d := range []string{home, proj} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	c := &Config{
		Provider: "openrouter", Model: "m/x",
		BaseURL: "https://exemplo.invalido/v1", APIKeyEnv: "CHAVE",
		Workdir: home, ProjectRoot: proj,
	}
	t.Setenv("CHAVE", "x")
	applyDefaults(c)
	if err := c.Validate(); err != nil {
		t.Fatalf("raiz irmã deveria passar: %v", err)
	}
}

// workdir deixa de ser a pasta do codigo: e a CASA do agente. Se alguém
// deixar project_root vazio, ele tem de cair num lugar previsivel e seguro —
// dentro da casa do agente, nunca fora.
func TestProjectRootDefaultsInsideAgentHome(t *testing.T) {
	home := t.TempDir()
	c := &Config{Workdir: home}
	applyDefaults(c)
	if c.ProjectRoot == "" {
		t.Fatal("ProjectRoot deveria receber default")
	}
	if !withinDir(home, c.ProjectRoot) {
		t.Fatalf("default %q deveria estar dentro da casa do agente %q", c.ProjectRoot, home)
	}
	// E tem de ser uma pasta de verdade, nao o proprio home: o agente nao
	// trabalha em cima de sessions/ e audit.jsonl.
	if c.ProjectRoot == filepath.Clean(home) {
		t.Fatal("o default nao pode ser a propria casa do agente")
	}
}

// O rascunho tambem mora na casa do agente, e nunca e a propria casa: e ali que
// vao log e artefato descartavel, e a casa guarda o audit e a sessao.
func TestScratchDefaultsInsideAgentHome(t *testing.T) {
	home := t.TempDir()
	c := &Config{Workdir: home}
	applyDefaults(c)
	if c.Scratch == "" {
		t.Fatal("Scratch deveria receber default")
	}
	if !withinDir(home, c.Scratch) {
		t.Fatalf("rascunho %q deveria estar dentro de %q", c.Scratch, home)
	}
	if filepath.Clean(c.Scratch) == filepath.Clean(home) {
		t.Fatal("o rascunho nao pode ser a casa do agente")
	}
}

// ANCIENTCODE_WORKDIR continua apontando para a CASA do agente. Confundir
// essa variavel com "pasta de trabalho" foi exatamente o que a nomenclatura
// errada escondia.
func TestWorkdirEnvStillMeansAgentHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANCIENTCODE_WORKDIR", dir)
	c := &Config{Provider: "openrouter", Model: "m/x", BaseURL: "https://e.invalid/v1", APIKeyEnv: "K"}
	applyEnvOverrides(c)
	if c.Workdir != dir {
		t.Errorf("Workdir = %q, esperava %q", c.Workdir, dir)
	}
	if c.ProjectRoot != "" {
		t.Errorf("a env de casa do agente nao deveria mexer na raiz do projeto: %q", c.ProjectRoot)
	}
}
