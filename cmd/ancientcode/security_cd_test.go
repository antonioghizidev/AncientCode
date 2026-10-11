package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antonioghizidev/ancientcode/internal/audit"
	"github.com/antonioghizidev/ancientcode/internal/config"
	"github.com/antonioghizidev/ancientcode/internal/tools/files"
	"github.com/antonioghizidev/ancientcode/internal/workspace"
)

// /cd é do OPERADOR. Nenhuma tool pode movê-la.
//
// O modelo que conseguisse mudar a própria raiz sairia do confine sem deixar
// rastro: o gate humano aprovado um comando `write_file` para um diretório, e
// a tool seguinte escreveria em outro. Por isso a propriedade é testada contra
// o registry REAL (buildRegistry), não contra uma lista reescrita no teste —
// assertar uma cópia é o erro de D-75 de novo.
func TestCdIsNotExposedToTheModel(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "agente")
	proj := filepath.Join(base, "projeto")
	for _, d := range []string{home, proj} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ws, err := workspace.New(proj, home)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Provider: "openrouter", Model: "m/x",
		BaseURL: "https://exemplo.invalido/v1", APIKeyEnv: "CHAVE",
		Workdir: home, ProjectRoot: proj,
		ShellTool: "enabled", DockerTool: "disabled", AptTool: "disabled",
	}
	ref := workspace.NewRef(ws)
	reg, _, err := buildRegistry(cfg, ref, files.NewFilesLive(ref.CodeRoot, 65536))
	if err != nil {
		t.Fatal(err)
	}

	// Nenhuma tool pode chamar-se cd, mudar_projeto, chdir, ou similar.
	proibidos := []string{"cd", "chdir", "chdir_tool", "set_project_root", "project_root",
		"mudar_pasta", "grant", "cd_tool"}
	nomes := make([]string, 0)
	for _, n := range reg.Names() {
		nomes = append(nomes, n)
		for _, p := range proibidos {
			if strings.EqualFold(n, p) {
				t.Fatalf("o registry expoe %q ao modelo; /cd e do operador", n)
			}
		}
	}
	// E o schema de nenhuma tool pode ter um campo que seja um caminho de raiz.
	for _, n := range reg.Names() {
		tl, found := reg.Lookup(n)
		if !found {
			continue
		}
		sch := strings.ToLower(string(tl.Parameters()))
		for _, campo := range []string{`"path"`, `"dir"`, `"root"`} {
			if strings.Contains(sch, campo) && strings.Contains(sch, "workspace") {
				t.Fatalf("a tool %q tem um campo que parece mexer na raiz: %s", n, sch)
			}
		}
	}
	t.Logf("tools expostas ao modelo: %s", strings.Join(nomes, ", "))
}

// audit.jsonl NÃO pode acompanhar o /cd.
//
// Este é o teste que fecha a brecha mais discreta do milestone: se o log
// mudasse de pasta junto com o projeto, trocar a raiz abriria um arquivo de
// auditoria NOVO — trilha limpa exatamente no momento de ampliar o alcance,
// que é quando mais importa ter registro.
func TestAuditPathDoesNotFollowTheProjectRoot(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "agente")
	proj := filepath.Join(base, "workspace", "projeto")
	for _, d := range []string{home, proj} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ws, err := workspace.New(proj, home)
	if err != nil {
		t.Fatal(err)
	}

	// O recorder nasce pelo MESMO caminho que main() usa. Construí-lo direto
	// com `home` faria o teste passar mesmo se main() usasse a raiz do projeto
	// — a mutação [B] ficou verde até isso ser corrigido.
	cfg := &config.Config{
		Provider: "openrouter", Model: "m/x",
		BaseURL: "https://exemplo.invalido/v1", APIKeyEnv: "CHAVE",
		Workdir: home, ProjectRoot: proj,
	}
	rec, err := newAuditRecorder(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	antes := rec.Path()

	// O operador sobe a árvore: amplia o alcance.
	//
	// O destino é <base>/workspace, e NÃO <base>: a raiz do projeto não pode
	// CONTER a casa do agente (lá mora o auth.json). Um teste que usasse <base>
	// seria barrado pela própria proteção — e a primeira versão deste teste
	// falhou exatamente assim, o que é o maior elogio a uma proteção.
	subir := filepath.Join(base, "workspace")
	if err := os.MkdirAll(subir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ws.SetProjectRoot(subir); err != nil {
		t.Fatalf("widening deveria passar: %v", err)
	}
	if rec.Path() != antes {
		t.Fatalf("o audit mudou de caminho com o /cd: %q -> %q", antes, rec.Path())
	}
	if !strings.HasPrefix(rec.Path(), home) {
		t.Fatalf("o audit saiu da casa do agente: %q", rec.Path())
	}

	// E continua gravando no MESMO lugar depois do /cd.
	if err := rec.Record(audit.Entry{Tool: "write_file", Status: "executed", Decision: "allow"}); err != nil {
		t.Fatal(err)
	}
	// E Path() tem de ser o arquivo que REALMENTE existe. Sem esta igualdade o
	// método pode mentir de forma consistente — antes e depois da troca batem,
	// o prefixo continua valendo, e o log está em outro lugar sem ninguém ver.
	if st, err := os.Stat(rec.Path()); err != nil || st.Size() == 0 {
		t.Fatalf("o caminho declarado (%q) não é o arquivo gravado: %v", rec.Path(), err)
	}
	// Nenhum audit.jsonl pode ter brotado dentro da nova raiz do projeto.
	novo := filepath.Join(ws.ProjectRoot(), "audit.jsonl")
	if _, err := os.Stat(novo); err == nil {
		t.Fatalf("um audit novo brotou na raiz do projeto: %s", novo)
	}
}
