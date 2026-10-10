package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antonioghizidev/ancientcode/internal/workspace"
)

func wsFixture(t *testing.T) (*workspace.Workspace, string) {
	t.Helper()
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
	w, err := workspace.New(proj, home)
	if err != nil {
		t.Fatal(err)
	}
	return w, base
}

// /cd sem argumento e uma PERGUNTA: o operador tem de poder ver onde esta.
// Sem isso o comando vira um no-op silencioso.
func TestCdWithoutArgumentShowsWhereWeAre(t *testing.T) {
	w, _ := wsFixture(t)
	antes := w.ProjectRoot()
	res := cdCommand(w, "")
	if !strings.Contains(res.Message, w.ProjectRoot()) {
		t.Errorf("nao mostrou a raiz atual: %q", res.Message)
	}
	if !strings.Contains(res.Message, w.Scratch()) {
		t.Errorf("nao mostrou o rascunho: %q", res.Message)
	}
	// Nenhum argumento nao pode mudar nada.
	if w.ProjectRoot() != antes {
		t.Fatalf("/cd sem argumento mudou a raiz: %q", w.ProjectRoot())
	}
	if res.Apply(w, true).Changed {
		t.Fatal("/cd sem argumento nao pode trocar a raiz")
	}
}

// Trocar para uma subpasta NAO amplia e nao deve pedir consentimento: um /cd a
// cada diretório ficaria insuportável, e prompt demais ensina a digitar "y".
func TestCdNarrowingDoesNotPrompt(t *testing.T) {
	w, base := wsFixture(t)
	inside := filepath.Join(w.ProjectRoot(), "internal")
	if err := os.MkdirAll(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = base
	res := cdCommand(w, inside)
	if res.NeedsConfirm {
		t.Error("descer nao deveria pedir confirmacao")
	}
	res.Apply(w, false)
	if w.ProjectRoot() != inside {
		t.Errorf("raiz = %q, esperava %q", w.ProjectRoot(), inside)
	}
}

// Subir a_tree amplia o blast radius e PRECISA perguntar, com o caminho
// resolvido — o operador precisa ver para onde o agente vai passar a escrever.
func TestCdWideningAsksWithTheResolvedPath(t *testing.T) {
	w, _ := wsFixture(t)
	proj := w.ProjectRoot()
	parent := filepath.Dir(proj)

	res := cdCommand(w, parent)
	if !res.NeedsConfirm {
		t.Fatal("subir a arvore deveria pedir confirmacao")
	}
	if !strings.Contains(res.Message, parent) {
		t.Errorf("a mensagem nao mostra o caminho resolvido: %q", res.Message)
	}
	// E enquanto nao confirmou, NADA muda.
	if w.ProjectRoot() != proj {
		t.Fatalf("a raiz mudou antes da confirmacao: %q", w.ProjectRoot())
	}
}

// Confirmado, troca. Recusado, nao troca.
func TestCdAppliesOnlyAfterConfirmation(t *testing.T) {
	w, _ := wsFixture(t)
	proj := w.ProjectRoot()
	parent := filepath.Dir(proj)

	// Recusado.
	res := cdCommand(w, parent)
	if res.Apply(w, false).Changed {
		t.Fatal("recusar nao pode trocar a raiz")
	}
	if w.ProjectRoot() != proj {
		t.Fatalf("recusou e mesmo assim trocou: %q", w.ProjectRoot())
	}

	// Confirmado.
	if !res.Apply(w, true).Changed {
		t.Fatal("confirmado deveria trocar a raiz")
	}
	if w.ProjectRoot() != parent {
		t.Errorf("raiz = %q, esperava %q", w.ProjectRoot(), parent)
	}
}

// O destino recusado pelo workspace precisa chegar ao operador como recusa
// clara, e a raiz atual precisa ficar intacta.
func TestCdRefusesAgentHome(t *testing.T) {
	w, _ := wsFixture(t)
	proj := w.ProjectRoot()
	home := w.AgentHome()

	res := cdCommand(w, home)
	if res.Err == nil {
		t.Fatal("/cd para a casa do agente deveria ser recusado")
	}
	if w.ProjectRoot() != proj {
		t.Errorf("raiz mudou numa troca recusada: %q", w.ProjectRoot())
	}
}

// /grant é a resposta honesta a "o agente precisa escrever fora do projeto".
// Conceder dentro da casa do agente entregaria o auth.json — e o teste precisa
// cobrir o caminho do COMANDO, não só o do workspace: um CanUse esquecido aqui
// deixaria a concessão passar enquanto o Grant recusaria depois, com mensagem
// errada.
// Caminho que nao existe: /cd cria, porque o operador pode estar digitando o
// projeto que ainda vai clonar.
func TestCdCreatesMissingDirectory(t *testing.T) {
	w, base := wsFixture(t)
	novo := filepath.Join(base, "projeto-novo")

	res := cdCommand(w, novo)
	if res.Err != nil {
		t.Fatalf("deveria criar: %v", res.Err)
	}
	res.Apply(w, true)
	if st, err := os.Stat(novo); err != nil || !st.IsDir() {
		t.Fatalf("nao criou: %v", err)
	}
}

// /grant é a resposta honesta a "o agente precisa escrever fora do projeto".
// Conceder dentro da casa do agente entregaria o auth.json — e o teste cobre o
// caminho do COMANDO, não só o do workspace: um CanUse esquecido aqui deixaria
// a concessão passar enquanto o Grant recusaria depois, com mensagem errada.
func TestGrantRefusesAgentHomeFromTheCommand(t *testing.T) {
	w, _ := wsFixture(t)
	home := w.AgentHome()
	antes := len(w.Grants())

	msg, err := grantCommand(w, filepath.Join(home, "segredo"))
	if err == nil {
		t.Fatalf("/grant dentro da casa do agente deveria ser recusado, veio %q", msg)
	}
	if !strings.Contains(msg, "agente") {
		t.Errorf("a recusa nao explica o motivo: %q", msg)
	}
	if len(w.Grants()) != antes {
		t.Error("nenhuma concessao deveria ter sido criada")
	}
}

// E o caminho feliz: conceder em outro lugar passa e fica visivel.
func TestGrantAllowsElsewhereAndListsIt(t *testing.T) {
	w, base := wsFixture(t)
	art := filepath.Join(base, "artefatos")

	msg, err := grantCommand(w, art)
	if err != nil {
		t.Fatalf("grant legitimo recusado: %v", err)
	}
	if !strings.Contains(msg, "sessao") {
		t.Errorf("a concessao deveria dizer que vale ate o fim da sessao: %q", msg)
	}
	if len(w.Grants()) != 1 {
		t.Fatalf("concessoes = %v", w.Grants())
	}
	// E o comando sem argumento lista o que esta liberado, para o operador
	// conseguir revogar mentalmente o que concedeu.
	lista, err := grantCommand(w, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lista, "artefatos") {
		t.Errorf("a lista de concessoes nao mostra a pasta: %q", lista)
	}
}

// /grant sem argumento quando nao ha nada concedido precisa dizer isso, e nao
// devolver string vazia — que na UI seria uma linha em branco sem sentido.
func TestGrantEmptyListIsExplicit(t *testing.T) {
	w, _ := wsFixture(t)
	msg, err := grantCommand(w, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(msg) == "" {
		t.Fatal("sem concessoes, a resposta nao pode ser vazia")
	}
	if !strings.Contains(msg, "nenhuma") {
		t.Errorf("esperava dizer que nao ha concessoes: %q", msg)
	}
}
