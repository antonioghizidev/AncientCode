package creds

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// secretNeverLeaks is the value planted in every store write. A test that finds
// it inside an error message, a String() or a path is a failure.
const secretNeverLeaks = "nvapi-SUPERSECRETVALUE123"

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

// --- S1. Permissoes: 0600 no arquivo, 0700 no diretorio -------------------

func TestS1FilePerms0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforced on Windows")
	}
	s := newStore(t)
	if err := s.Save("nvidia", secretNeverLeaks); err != nil {
		t.Fatalf("Save: %v", err)
	}
	fi, err := os.Stat(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("auth file perm = %o, want 600", perm)
	}
	di, err := os.Stat(filepath.Dir(s.path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Fatalf("auth dir perm = %o, want 700", perm)
	}
}

// Um arquivo pre-existente frouxo e reescrito tem que ser corrigido: senao um
// `cp` mal feito deixaria a chave legivel e nada no caminho normal reclamaria.
func TestS1TightensLooseExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforced on Windows")
	}
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("nvidia", secretNeverLeaks); err != nil {
		t.Fatalf("Save: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("permissao frouxa sobreviveu ao Save: %o", perm)
	}
}

// --- S2. Escrita atomica --------------------------------------------------

// Um store cujo diretorio e arquivo-alvo estao corrompidos nao pode deixar
// credencial truncada nem legivel: ou grava inteiro, ou nao grava.
func TestS2AtomicWriteLeavesNoPartialCredential(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforced on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("nvidia", secretNeverLeaks); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Torna o diretorio somente-leitura: a criacao do tmp tem de falhar.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)

	_ = s.Save("openrouter", "sk-outra-chave")

	// O arquivo anterior tem de continuar integro e legivel.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("arquivo sumiu apos falha: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("conteudo corrompido:\nantes: %s\ndepois: %s", before, after)
	}
	// Nenhum arquivo temporario com a chave abandonedado no diretorio.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), secretNeverLeaks) {
			t.Fatalf("arquivo temporario com segredo abandonado: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("residuo no diretorio apos falha: %v", names)
	}
}

// --- S3. O segredo nunca sai por erro, log ou String() --------------------

func TestS3SecretNeverInErrorOrString(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("nvidia", secretNeverLeaks); err != nil {
		t.Fatal(err)
	}

	// Get de provider ausente: o erro nao pode carregar segredo de outro.
	if _, _, err := s.Get("openrouter"); err == nil {
		t.Fatal("Get de provider ausente deveria falhar")
	} else if strings.Contains(err.Error(), secretNeverLeaks) {
		t.Fatalf("erro de Get carrega o segredo: %v", err)
	}

	// Nome de provider invalido.
	for _, bad := range []string{"", "nvidia/../etc", "../../etc/passwd", "NVIDIA", "nvidia\n"} {
		if err := s.Save(bad, secretNeverLeaks); err == nil {
			t.Fatalf("Save(%q) deveria falhar", bad)
		} else if strings.Contains(err.Error(), secretNeverLeaks) {
			t.Fatalf("erro de Save(%q) carrega o segredo: %v", bad, err)
		}
	}

	// List devolve so nomes de provider, nunca valores.
	names, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if strings.Contains(n, secretNeverLeaks) {
			t.Fatalf("List carrega o segredo: %q", n)
		}
	}
	if got := strings.Join(names, ","); got != "nvidia" {
		t.Fatalf("List = %q, want nvidia", got)
	}
}

// Chave ausente devolve o nome do provider e nada mais. A mensagem de erro do
// pinpointing tem que ser util sem ser um oráculo de segredo.
func TestS3MissingEntryNamesProviderOnly(t *testing.T) {
	s := newStore(t)
	_, _, err := s.Get("openrouter")
	if err == nil {
		t.Fatal("esperava erro para provider sem credencial")
	}
	if !strings.Contains(err.Error(), "openrouter") {
		t.Fatalf("erro deveria nomear o provider: %v", err)
	}
}

// --- S8. Schema estrito: campo desconhecido e erro, nao silencio ------------

func TestS8UnknownFieldFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	raw := `{"version":1,"providers":{"nvidia":{"key":"k","fixed":true}},` +
		`"providers2":{"x":{"key":"y"}}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get("nvidia"); err == nil {
		t.Fatal("campo desconhecido no topo deveria falhar (fail-closed)")
	}
}

func TestS8UnknownEntryFieldFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	raw := `{"version":1,"providers":{"nvidia":{"key":"k","fixed":true,"pinha":1}}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get("nvidia"); err == nil {
		t.Fatal("campo desconhecido na entrada deveria falhar (fail-closed)")
	}
}

func TestS8FutureVersionRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"providers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get("nvidia"); err == nil {
		t.Fatal("versao futura do store deveria ser rejeitada (fail-closed)")
	}
}

// --- Limites de tamanho: leitura sempre limitada ---------------------------

func TestOversizedEntryRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	big := strings.Repeat("a", maxEntryBytes+1)
	blob, err := json.Marshal(file{Version: storeVersion, Providers: map[string]Entry{
		"nvidia": {Key: big},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get("nvidia"); err == nil {
		t.Fatal("entrada acima do limite deveria ser rejeitada")
	}
}

func TestOversizedFileRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("arquivo grande demais para o modo curto")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxFileBytes+10)), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get("nvidia"); err == nil {
		t.Fatal("arquivo acima do limite deveria ser rejeitado")
	}
}

// --- Funcional: pin, overwrite, --use-env, delete -------------------------

func TestSaveOverwritesPinnedKey(t *testing.T) {
	s := newStore(t)
	if err := s.Save("nvidia", "primeira"); err != nil {
		t.Fatal(err)
	}
	// Reconectar tem que SOBRESCREVER, nao falhar por ja existir.
	if err := s.Save("nvidia", "segunda"); err != nil {
		t.Fatalf("reconectar falhou: %v", err)
	}
	e, fixed, err := s.Get("nvidia")
	if err != nil {
		t.Fatal(err)
	}
	if e.Key != "segunda" || !fixed {
		t.Fatalf("key=%q fixed=%v, want segunda/true", e.Key, fixed)
	}
}

func TestUseEnvReleasesPinButKeepsKey(t *testing.T) {
	s := newStore(t)
	if err := s.Save("nvidia", "guardada"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFixed("nvidia", false); err != nil {
		t.Fatal(err)
	}
	e, fixed, err := s.Get("nvidia")
	if err != nil {
		t.Fatal(err)
	}
	if e.Key != "guardada" {
		t.Fatalf("--use-env apagou a chave: %q", e.Key)
	}
	if fixed {
		t.Fatal("--use-env deveria soltar o pin")
	}
}

func TestSetFixedOnMissingProviderFails(t *testing.T) {
	s := newStore(t)
	if err := s.SetFixed("nvidia", false); err == nil {
		t.Fatal("soltar pin de provider sem credencial deveria falhar")
	}
}

func TestDeleteRemovesEntryAndPin(t *testing.T) {
	s := newStore(t)
	if err := s.Save("nvidia", "k"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("openrouter", "k2"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("nvidia"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get("nvidia"); err == nil {
		t.Fatal("logout deveria ter apagado a credencial")
	}
	names, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "openrouter" {
		t.Fatalf("List = %v, want [openrouter]", names)
	}
	if err := s.Delete("nao-existe"); err == nil {
		t.Fatal("logout de provider inexistente deveria falhar")
	}
}

// --- O store nao pode vazar segredo para fora do arquivo ------------------

func TestEntryStringerDoesNotLeakKey(t *testing.T) {
	s := newStore(t)
	if err := s.Save("nvidia", secretNeverLeaks); err != nil {
		t.Fatal(err)
	}
	// Pega a Entry pelo caminho real (Get), e nao construindo a struct: assim o
	// teste cobre o provider que o store preenche, que e o que String usa.
	e, _, err := s.Get("nvidia")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.String(), secretNeverLeaks) {
		t.Fatalf("String() carrega o segredo: %s", e.String())
	}
	if !strings.Contains(e.String(), "nvidia") {
		t.Fatalf("String() deveria identificar o provider: %s", e.String())
	}
}
