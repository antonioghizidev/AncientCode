package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

// --- O id do modelo e entrada REMOTA: e o provider que o devolve -----------

// O catalogo de um provider e conteudo nao confiavel: quem escreve o id e o
// provider, nao o operador. Se o id puder conter sequencia ANSI, o picker
// desenha o que quiser na tela do operador; se contiver newline ou espaco, o
// id vaza para linha de comando e saida de log. Este e o unico lugar onde a
// entrada remota e barrada antes de chegar a disco E a UI.
func TestModelIDRejectsInjection(t *testing.T) {
	bad := []string{
		"evil\x1b[2J\x1b[H",       // ANSI:Limpa a tela do operador
		"evil\x1b[31mred",         // ANSI: cor
		"model\nfake: outo-model", // injeta linha na saida
		"model\tcom tab",
		"model com espaco",
		"model; rm -rf /", // metachar de shell
		"model|cat",       // pipe
		"model`id`",       // substituicao de comando
		"model$(id)",
		"model&", // background de shell
		"model&&outro",
		"-comeca-com-traco", // id que parece flag
		"../escape",         // traversal
		"",                  // vazio
	}
	for _, id := range bad {
		if err := ValidateModelID(id); err == nil {
			t.Fatalf("ValidateModelID(%q) deveria falhar", id)
		}
		s := newStore(t)
		if err := s.SetModel("nvidia", id); err == nil {
			t.Fatalf("SetModel aceitou id perigoso %q", id)
		}
		// Nada pode ter chegado a disco.
		data, err := os.ReadFile(s.path)
		if err == nil && strings.Contains(string(data), id) && id != "" {
			t.Fatalf("id perigoso %q foi persistido", id)
		}
	}
}

// O que o opencode/NIM realmente manda: barra, dois pontos, arroba, ponto,
// traco e mais. Tem que passar, senao o filtro e inútil.
func TestModelIDAcceptsRealIDs(t *testing.T) {
	good := []string{
		"nvidia/nemotron-3.5-lightning-30b-a3b",
		"meta-llama/llama-3.3-70b-instruct",
		"anthropic/claude-sonnet-4.5",
		"gpt-4o-mini",
		"qwen/qwen3-coder-30b-a3b-instruct",
		"zai-org/glm-4.6",
		"openai/gpt-oss-120b",
	}
	for _, id := range good {
		if err := ValidateModelID(id); err != nil {
			t.Fatalf("ValidateModelID(%q) = %v, deveria passar", id, err)
		}
	}
}

// --- Estado por provedor: o comportamento que o /models depende ------------

func TestSetModelIsPerProvider(t *testing.T) {
	s := newStore(t)
	if err := s.SetModel("nvidia", "nvidia/nemotron-3.5"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetModel("openrouter", "anthropic/claude-sonnet-4.5"); err != nil {
		t.Fatal(err)
	}
	// Trocar de provedor nao pode apagar o modelo do anterior.
	id, ok, err := s.GetModel("nvidia")
	if err != nil || !ok || id != "nvidia/nemotron-3.5" {
		t.Fatalf("nvidia = %q ok=%v err=%v", id, ok, err)
	}
	id, ok, err = s.GetModel("openrouter")
	if err != nil || !ok || id != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("openrouter = %q ok=%v err=%v", id, ok, err)
	}
	profs, err := s.Providers()
	if err != nil {
		t.Fatal(err)
	}
	if len(profs) != 2 || profs[0] != "nvidia" || profs[1] != "openrouter" {
		t.Fatalf("Providers = %v, want [nvidia openrouter]", profs)
	}
}

// Provider nunca usado devolve ok=false sem erro: e o sinal que faz o
// SwitchProvider pedir /models em vez de chutar um modelo (D-68).
func TestUnknownProviderIsMissingNotError(t *testing.T) {
	s := newStore(t)
	id, ok, err := s.GetModel("litellm")
	if err != nil {
		t.Fatalf("GetModel de provider desconhecido nao deveria ser erro: %v", err)
	}
	if ok {
		t.Fatalf("GetModel de provider desconhecido retornou ok=true, id=%q", id)
	}
	if id != "" {
		t.Fatalf("GetModel de provider desconhecido retornou id=%q", id)
	}
}

func TestGetModelOnMissingStore(t *testing.T) {
	s := newStore(t)
	if _, ok, err := s.GetModel("nvidia"); ok || err != nil {
		t.Fatalf("store vazio: ok=%v err=%v, quero false/nil", ok, err)
	}
}

func TestDeleteForgetsModel(t *testing.T) {
	s := newStore(t)
	if err := s.SetModel("nvidia", "m1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("nvidia"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetModel("nvidia"); ok {
		t.Fatal("Delete nao esqueceu o modelo")
	}
	// Esquecer duas vezes nao e erro.
	if err := s.Delete("nvidia"); err != nil {
		t.Fatalf("Delete repetido nao deveria ser erro: %v", err)
	}
}

func TestReopenPreservesModels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetModel("nvidia", "nvidia/nemotron-3.5"); err != nil {
		t.Fatal(err)
	}
	// Reabre: e o que acontece em cada execucao seguinte do binario.
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	id, ok, err := s2.GetModel("nvidia")
	if err != nil || !ok || id != "nvidia/nemotron-3.5" {
		t.Fatalf("apos reabrir: id=%q ok=%v err=%v", id, ok, err)
	}
}

// --- Escrita atomica: um estado truncado perde a preferencia do operador ----

// O credential store tem este teste (TestS2AtomicWrite...); o state tambem
// precisa. Sem ele a escrita pode virar um open/truncate direto no destino e o
// arquivo fica truncado no meio — o operador perde o modelo de todos os
// providers e a UI passa a mentir dizendo que nunca houve escolha. Era
// exatamente este teste que faltava aqui; a mutacao M5 o expôs.
func TestAtomicWriteLeavesNoTruncatedState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforced on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetModel("nvidia", "nvidia/nemotron-3.5"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Diretorio somente-leitura: a criacao do tmp tem de falhar.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)

	_ = s.SetModel("openrouter", "anthropic/claude-sonnet-4.5")

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("state sumiu apos falha: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("estado corrompido:\nantes:  %s\ndepois: %s", before, after)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("residuo apos falha: %v", names)
	}
	// E o estado tem de continuar legivel e coerente.
	id, ok, err := s.GetModel("nvidia")
	if err != nil || !ok || id != "nvidia/nemotron-3.5" {
		t.Fatalf("estado perdido apos falha: id=%q ok=%v err=%v", id, ok, err)
	}
}

// --- Permissoes ------------------------------------------------------------

func TestStateFilePerms0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforced on Windows")
	}
	s := newStore(t)
	if err := s.SetModel("nvidia", "m1"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("state perm = %o, quero sem acesso group/other", perm)
	}
	di, err := os.Stat(filepath.Dir(s.path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("state dir perm = %o, quero sem acesso group/other", perm)
	}
}

// --- Schema estrito --------------------------------------------------------

func TestUnknownFieldFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"models":{},"permmode":"0644"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetModel("nvidia"); err == nil {
		t.Fatal("campo desconhecido deveria falhar (fail-closed)")
	}
}

func TestFutureVersionRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"models":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetModel("nvidia"); err == nil {
		t.Fatal("versao futura deveria ser rejeitada (fail-closed)")
	}
}

// Um state.json escrito a mao com id perigoso nao pode ser aceito: o arquivo e
// local, mas o conteudo pode ter vindo de um backup, de um editor, ou de um
// catalogo antigo. Entrar por outra porta nao contorna o filtro.
// O poison realista: JSON VALIDO que decodifica para byte perigoso.
//
// O ESC vai escrito como \u001b no texto JSON, que e o escape CORRETO de JSON
// e decodifica para um byte ESC de verdade. A primeira versao deste teste
// usava "\x1b" literal, que NAO e escape de JSON: o parse falhava antes da
// validacao do id e o teste passava pelo motivo errado, sem provar nada
// sobre o filtro. Cada caso agora e conferido como JSON valido antes de
// exigir a recusa, justamente para nao voltar a passar por engano.
func TestPoisonedStateFileRejectedOnLoad(t *testing.T) {
	poisoned := []string{
		`{"version":1,"models":{"nvidia":"evil\u001b[2J"}}`,
		`{"version":1,"models":{"nvidia":"evil\u0007bell"}}`,
		`{"version":1,"models":{"nvidia":"a\u0000b"}}`,
		`{"version":1,"models":{"nvidia":"a\nb"}}`,
		`{"version":1,"models":{"nvidia":"model; rm -rf /"}}`,
		`{"version":1,"models":{"nvidia":"model|cat"}}`,
		`{"version":1,"models":{"nvidia":"-flag"}}`,
	}
	for _, raw := range poisoned {
		// Guarda contra o teste voltar a passar pelo motivo errado.
		var probe map[string]any
		if err := json.Unmarshal([]byte(raw), &probe); err != nil {
			t.Fatalf("caso de teste nao e JSON valido, provaria nada: %v", err)
		}

		path := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.GetModel("nvidia"); err == nil {
			t.Fatalf("state.json envenenado aceito: %s", raw)
		}
	}
}

// O mesmo poison barrado pelo caminho normal de escrita.
func TestPoisonedModelIDRejectedOnSave(t *testing.T) {
	s := newStore(t)
	for _, id := range []string{"evil\x1b[2J", "a\nb", "a\x00b", "a\x07b"} {
		if err := s.SetModel("nvidia", id); err == nil {
			t.Fatalf("SetModel aceitou id com byte de controle: %q", id)
		}
	}
	if data, err := os.ReadFile(s.path); err == nil {
		if strings.ContainsRune(string(data), 0x1b) || strings.ContainsRune(string(data), 0x00) {
			t.Fatal("byte de controle chegou ao disco")
		}
	}
}

func TestOversizedModelIDRejected(t *testing.T) {
	s := newStore(t)
	long := "m" + strings.Repeat("x", maxModelIDBytes+10)
	if err := s.SetModel("nvidia", long); err == nil {
		t.Fatal("id acima do limite deveria ser rejeitado")
	}
}

func TestInvalidProviderRejected(t *testing.T) {
	s := newStore(t)
	for _, p := range []string{"", "NVIDIA", "a/b", "a b", "../x"} {
		if err := s.SetModel(p, "m1"); err == nil {
			t.Fatalf("provider %q deveria ser rejeitado", p)
		}
	}
}
