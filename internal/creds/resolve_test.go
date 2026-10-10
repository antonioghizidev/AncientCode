package creds

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func writeRaw(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

// envLookup builds a fake environment so the precedence rule is exercised
// without mutating the process environment.
func envLookup(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

// --- A regra de D-66, por provedor -----------------------------------------

// Este e o defeito que a regra veio corrigir: com a chave no ambiente e um
// /connect pinado, a chave colada tem que vencer. Se a precedencia voltar a
// "env sempre ganha", este teste fica vermelho.
func TestPinnedStoreBeatsEnv(t *testing.T) {
	s := newStore(t)
	if err := s.Save("nvidia", "chave-do-connect"); err != nil {
		t.Fatal(err)
	}
	key, src, err := s.Resolve("nvidia", "NVIDIA_API_KEY",
		envLookup(map[string]string{"NVIDIA_API_KEY": "chave-do-ambiente"}))
	if err != nil {
		t.Fatal(err)
	}
	if key != "chave-do-connect" {
		t.Fatalf("key = %q, quero a do /connect (a do ambiente seria silencio)", key)
	}
	if src != SourceStoreFixed {
		t.Fatalf("source = %q, quero %q", src, SourceStoreFixed)
	}
	if !src.Pinned() {
		t.Fatal("SourceStoreFixed deveria reportar Pinned()=true")
	}
}

// --use-env solta o pin: a partir dai o ambiente volta a vencer, sem apagar a
// chave guardada.
func TestUnpinnedStoreLosesToEnv(t *testing.T) {
	s := newStore(t)
	if err := s.Save("nvidia", "chave-do-connect"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFixed("nvidia", false); err != nil {
		t.Fatal(err)
	}
	key, src, err := s.Resolve("nvidia", "NVIDIA_API_KEY",
		envLookup(map[string]string{"NVIDIA_API_KEY": "chave-do-ambiente"}))
	if err != nil {
		t.Fatal(err)
	}
	if key != "chave-do-ambiente" {
		t.Fatalf("key = %q, com o pin solto quem vence e o ambiente", key)
	}
	if src != SourceEnv {
		t.Fatalf("source = %q, quero %q", src, SourceEnv)
	}
}

// Store solta e ambiente vazio: ainda serve a chave guardada, e a UI tem de
// poder dizer que ela esta solta (ou seja, um export a mais passaria a vencer).
func TestUnpinnedStoreUsedWhenEnvEmpty(t *testing.T) {
	s := newStore(t)
	if err := s.Save("nvidia", "guardada"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFixed("nvidia", false); err != nil {
		t.Fatal(err)
	}
	key, src, err := s.Resolve("nvidia", "NVIDIA_API_KEY", envLookup(nil))
	if err != nil {
		t.Fatal(err)
	}
	if key != "guardada" || src != SourceStoreLoose {
		t.Fatalf("key=%q src=%q, quero guardada/%s", key, src, SourceStoreLoose)
	}
	if src.Pinned() {
		t.Fatal("store solta nao pode se declarar fixada")
	}
}

// --- REGRESSAO: quem usava env continua usando env, sem store --------------

// Este e o requisito de "zero regressao": o caminho de CI e de quem nunca
// digitou /connect tem de continuar idêntico ao de antes do M14.
func TestEnvStillWinsWithoutAnyConnect(t *testing.T) {
	s := newStore(t)
	key, src, err := s.Resolve("openrouter", "OPENROUTER_API_KEY",
		envLookup(map[string]string{"OPENROUTER_API_KEY": "sk-do-ambiente"}))
	if err != nil {
		t.Fatalf("env sem store falhou: %v", err)
	}
	if key != "sk-do-ambiente" || src != SourceEnv {
		t.Fatalf("key=%q src=%q, quero sk-do-ambiente/%s", key, src, SourceEnv)
	}
}

// --- Falha: o erro tem que nomear AS DUAS fontes tentadas -----------------

func TestNoCredentialNamesBothSourcesTried(t *testing.T) {
	s := newStore(t)
	_, src, err := s.Resolve("nvidia", "NVIDIA_API_KEY", envLookup(nil))
	if err == nil {
		t.Fatal("sem chave em lugar nenhum deveria falhar")
	}
	msg := err.Error()
	// Um erro que so diz "sem credencial" deixa o operador sem saber o que
	// fazer: faltou exportar, ou falta o /connect?
	for _, want := range []string{"nvidia", "NVIDIA_API_KEY", "/connect", s.path} {
		if !strings.Contains(msg, want) {
			t.Fatalf("erro nao menciona %q: %v", want, err)
		}
	}
	if src != SourceNone {
		t.Fatalf("source = %q, quero %q", src, SourceNone)
	}
}

// --- O pin e POR PROVEDOR: connect no nvidia nao afeta o openrouter ---------

func TestPinIsPerProvider(t *testing.T) {
	s := newStore(t)
	if err := s.Save("nvidia", "chave-nvidia"); err != nil {
		t.Fatal(err)
	}
	env := envLookup(map[string]string{"OPENROUTER_API_KEY": "sk-ambiente"})

	if _, src, _ := s.Resolve("nvidia", "NVIDIA_API_KEY", env); src != SourceStoreFixed {
		t.Fatalf("nvidia src=%q, quero %s", src, SourceStoreFixed)
	}
	key, src, err := s.Resolve("openrouter", "OPENROUTER_API_KEY", env)
	if err != nil {
		t.Fatalf("openrouter falhou: %v", err)
	}
	if key != "sk-ambiente" || src != SourceEnv {
		t.Fatalf("openrouter key=%q src=%q: pinar o nvidia NAO pode affectar outro provider",
			key, src)
	}
}

// --- Store corrompido e fail-closed: nao cai no ambiente -------------------

// Se o store esta corrompido e a resolucao cair silenciosamente no ambiente,
// o operador usaria a chave errada sem nenhum sinal. Corrupcao tem de barulho.
func TestCorruptStoreDoesNotFallThroughToEnv(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/auth.json"
	if err := writeRaw(path, `{"version":1,"providers":{},"lixo":1}`); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, src, err := s.Resolve("nvidia", "NVIDIA_API_KEY",
		envLookup(map[string]string{"NVIDIA_API_KEY": "sk-do-ambiente"}))
	if err == nil {
		t.Fatal("store corrompido deveria falhar, mesmo com env disponivel")
	}
	if src != SourceNone {
		t.Fatalf("source = %q, com store corrompido nada deve ser resolvido", src)
	}
	if !strings.Contains(err.Error(), "lixo") && !strings.Contains(err.Error(), "invalid store") {
		t.Fatalf("erro deveria explicar que o store esta invalido: %v", err)
	}
}

// --- Provider invalido nunca chega a ser resolvido -------------------------

func TestResolveRejectsInvalidProvider(t *testing.T) {
	s := newStore(t)
	for _, p := range []string{"", "../etc", "NVIDIA", "a/b"} {
		if _, _, err := s.Resolve(p, "X_KEY", envLookup(map[string]string{"X_KEY": "v"})); err == nil {
			t.Fatalf("Resolve(%q) deveria falhar", p)
		}
	}
}

// --- A chave resolvida nao aparece em log/error de falha -------------------

func TestResolveErrorNeverCarriesKey(t *testing.T) {
	s := newStore(t)
	if err := s.Save("nvidia", secretNeverLeaks); err != nil {
		t.Fatal(err)
	}
	// openrouter nao tem credencial, mas o store tem a do nvidia: nenhum dos
	// dois erros pode vazar a chave do nvidia.
	_, _, err := s.Resolve("openrouter", "OPENROUTER_API_KEY", envLookup(nil))
	if err == nil {
		t.Fatal("esperava erro")
	}
	if strings.Contains(err.Error(), secretNeverLeaks) {
		t.Fatalf("erro de Resolve carrega a chave: %v", err)
	}
}

// ErrNoCredential precisa ser distinguivel, senao o chamador cai no
// fall-through para o ambiente sem querer.
func TestErrNoCredentialIsMatchable(t *testing.T) {
	s := newStore(t)
	_, _, err := s.Get("nvidia")
	if !errors.Is(err, ErrNoCredential) {
		t.Fatalf("Get de provider ausente: errors.Is(err, ErrNoCredential) = false, err=%v", err)
	}
	// Ja existente: o erro NAO deve casar com ErrNoCredential, senao um
	// arquivo corrompido seria tratado como "sem credencial".
	if err := s.Save("nvidia", "k"); err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Get("nvidia")
	if err != nil {
		t.Fatalf("Get de provider existente: %v", err)
	}
}
