package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// RootRef e a indirecao que permite trocar a raiz em runtime.
//
// O motivo e estrutural: files.Files, code.Toolset e sandbox.Executor capturam
// a raiz NA CONSTRUCAO e nenhum tem setter, e tools.Registry.Register recusa
// duplicata (tool.go:102-104) — reinstalar a tool com a raiz nova nao existe.
// Logo a raiz precisa ser lida de um lugar vivo, nao guardada num campo.
func TestRefFollowsSetProjectRoot(t *testing.T) {
	base, home := siblingHome(t)
	proj := filepath.Join(base, "projeto")
	outro := filepath.Join(base, "outro-projeto")
	for _, d := range []string{proj, outro} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	w, err := New(proj, home)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRef(w)

	if r.CodeRoot() != proj {
		t.Errorf("CodeRoot = %q, esperava %q", r.CodeRoot(), proj)
	}
	if err := w.SetProjectRoot(outro); err != nil {
		t.Fatal(err)
	}
	if r.CodeRoot() != outro {
		t.Errorf("apos /cd, CodeRoot = %q, esperava %q", r.CodeRoot(), outro)
	}
}

// O executor do sandbox precisa da raiz no momento do comando, nao no da
// construcao — senao o shell continua na pasta antiga depois do /cd, que e
// justamente o defeito que este milestone existe para remover.
func TestRefWorkdirFollowsProjectRoot(t *testing.T) {
	base, home := siblingHome(t)
	proj := filepath.Join(base, "projeto")
	outro := filepath.Join(base, "outro")
	for _, d := range []string{proj, outro} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	w, err := New(proj, home)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRef(w)

	if r.Workdir() != proj {
		t.Errorf("Workdir = %q, esperava a raiz do projeto", r.Workdir())
	}
	if err := w.SetProjectRoot(outro); err != nil {
		t.Fatal(err)
	}
	if r.Workdir() != outro {
		t.Errorf("apos /cd, Workdir = %q, esperava %q", r.Workdir(), outro)
	}
}

// O shell precisa saber onde fica o rascunho sem que o operador digite o caminho
// absoluto: e o que impede `go build > log.txt` de sujar o repositorio.
func TestRefExposesScratch(t *testing.T) {
	base, home := siblingHome(t)
	proj := filepath.Join(base, "projeto")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	w, err := New(proj, home)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRef(w)
	if r.Scratch() != w.Scratch() {
		t.Errorf("Scratch = %q", r.Scratch())
	}
	if !sameOrUnder(r.Scratch(), home) {
		t.Error("o rascunho deveria morar dentro da casa do agente")
	}
}

// Writability: um consumidor (a tool de escrita) precisa perguntar "posso
// escrever aqui?" numa unica chamada, e a resposta tem que incluir rascunho e
// concessoes — senao o rascunho vira mais um lugar proibido na pratica.
func TestRefWritableCoversProjectScratchAndGrants(t *testing.T) {
	base, home := siblingHome(t)
	proj := filepath.Join(base, "projeto")
	art := filepath.Join(base, "artefatos")
	for _, d := range []string{proj, art} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	w, err := New(proj, home)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRef(w)

	if !r.Writable(filepath.Join(proj, "main.go")) {
		t.Error("raiz do projeto deveria ser gravavel")
	}
	if !r.Writable(filepath.Join(w.Scratch(), "out.log")) {
		t.Error("rascunho deveria ser gravavel")
	}
	if r.Writable(filepath.Join(art, "x")) {
		t.Error("sem grant, artefatos nao deveria ser gravavel")
	}
	if err := w.Grant(art); err != nil {
		t.Fatal(err)
	}
	if !r.Writable(filepath.Join(art, "x")) {
		t.Error("apos grant, artefatos deveria ser gravavel")
	}
	if r.Writable(filepath.Join(home, "auth.json")) {
		t.Error("auth.json nunca deveria ser gravavel")
	}
}
