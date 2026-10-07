package connect

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antonioghizidev/ancientcode/internal/creds"
	"github.com/antonioghizidev/ancientcode/internal/provider"
)

const candKey = "nvapi-CANDIDATASECRET999"

type fakeVerifier struct {
	modelCount  int
	err         error
	calls       int
	gotKey      string
	gotBaseURL  string
	gotProvider string
}

func (f *fakeVerifier) Verify(_ context.Context, providerID, baseURL, key, _ string) (int, provider.Outcome, error) {
	f.calls++
	f.gotProvider = providerID
	f.gotKey = key
	f.gotBaseURL = baseURL
	if f.err != nil {
		return 0, provider.OutcomeInvalid, f.err
	}
	return f.modelCount, provider.OutcomeOK, nil
}

func newSvc(t *testing.T, v Verifier) (*Service, *creds.Store) {
	t.Helper()
	store, err := creds.Open(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	return New(store, v), store
}

// --- A propriedade central: chave recusada NAO e guardada ------------------

func TestRejectedKeyIsNotStored(t *testing.T) {
	v := &fakeVerifier{err: provider.ErrUnauthorized}
	svc, store := newSvc(t, v)

	_, err := svc.Connect(context.Background(), "nvidia", "https://x/v1", candKey, "")
	if err == nil {
		t.Fatal("chave recusada deveria dar erro")
	}
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("errors.Is(err, ErrRejected) = false: %v", err)
	}
	// Nada em disco.
	if _, _, err := store.Get("nvidia"); err == nil {
		t.Fatal("chave recusada foi guardada — o defeito central do /connect")
	}
	names, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Fatalf("store deveria estar vazio, tem %v", names)
	}
}

// Falha de rede tambem nao guarda: guardar por "deve ser a rede" seria adivinhar
// a chave do operador.
func TestNetworkFailureIsNotStored(t *testing.T) {
	v := &fakeVerifier{err: errors.New("dial tcp: timeout")}
	svc, store := newSvc(t, v)

	if _, err := svc.Connect(context.Background(), "nvidia", "https://x/v1", candKey, ""); err == nil {
		t.Fatal("esperava erro")
	}
	if _, _, err := store.Get("nvidia"); err == nil {
		t.Fatal("chave foi guardada apesar da falha de rede")
	}
}

func TestAcceptedKeyIsStoredPinned(t *testing.T) {
	v := &fakeVerifier{modelCount: 81}
	svc, store := newSvc(t, v)

	got, err := svc.Connect(context.Background(), "nvidia", "https://integrate.api.nvidia.com/v1", candKey, "")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !got.Stored || got.ModelCount != 81 || got.Replaced {
		t.Fatalf("Result = %+v", got)
	}
	if got.Source != creds.SourceStoreFixed {
		t.Fatalf("Source = %q, quero %q", got.Source, creds.SourceStoreFixed)
	}
	if v.gotKey != candKey || v.gotBaseURL != "https://integrate.api.nvidia.com/v1" {
		t.Fatalf("verificador recebeu key=%q baseURL=%q", v.gotKey, v.gotBaseURL)
	}
	e, fixed, err := store.Get("nvidia")
	if err != nil {
		t.Fatal(err)
	}
	if e.Key != candKey || !fixed {
		t.Fatalf("guardou key errada ou sem pin: fixed=%v", fixed)
	}
}

// Reconectar SOBRESCREVE: querer trocar a chave e o motivo de reconectar.
func TestReconnectReplacesAndReportsIt(t *testing.T) {
	v := &fakeVerifier{modelCount: 5}
	svc, store := newSvc(t, v)
	if _, err := svc.Connect(context.Background(), "nvidia", "https://x/v1", candKey, ""); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Connect(context.Background(), "nvidia", "https://x/v1", "outra-chave-mesmo-tamanho", "")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Replaced {
		t.Fatal("Result.Replaced deveria ser true na segunda conexao")
	}
	e, _, _ := store.Get("nvidia")
	if e.Key == candKey {
		t.Fatal("reconectar nao substituiu a chave")
	}
}

// Chave vazia nunca chega ao verificador nem ao disco.
func TestEmptyKeyRefusedBeforeVerifier(t *testing.T) {
	v := &fakeVerifier{}
	svc, _ := newSvc(t, v)
	for _, k := range []string{"", "   ", "\t\n"} {
		if _, err := svc.Connect(context.Background(), "nvidia", "https://x/v1", k, ""); err == nil {
			t.Fatalf("chave %q deveria ser recusada", k)
		}
	}
	if v.calls != 0 {
		t.Fatalf("verificador foi chamado %d vezes com chave vazia", v.calls)
	}
}

// Provider invalido nem chega ao verificador.
func TestInvalidProviderRefused(t *testing.T) {
	v := &fakeVerifier{}
	svc, _ := newSvc(t, v)
	for _, p := range []string{"", "../etc", "NVIDIA", "a/b"} {
		if _, err := svc.Connect(context.Background(), p, "https://x/v1", candKey, ""); err == nil {
			t.Fatalf("provider %q deveria ser recusado", p)
		}
	}
	if v.calls != 0 {
		t.Fatal("verificador foi chamado com provider invalido")
	}
}

// --- --use-env e logout ---------------------------------------------------

func TestUseEnvKeepsKeyButReleasesPin(t *testing.T) {
	v := &fakeVerifier{}
	svc, store := newSvc(t, v)
	if _, err := svc.Connect(context.Background(), "nvidia", "https://x/v1", candKey, ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.UseEnv("nvidia"); err != nil {
		t.Fatal(err)
	}
	e, fixed, err := store.Get("nvidia")
	if err != nil {
		t.Fatalf("--use-env apagou a chave: %v", err)
	}
	if e.Key != candKey {
		t.Fatal("--use-env alterou o valor da chave")
	}
	if fixed {
		t.Fatal("--use-env nao soltou o pin")
	}
}

func TestUseEnvWithoutCredentialFails(t *testing.T) {
	svc, _ := newSvc(t, &fakeVerifier{})
	if err := svc.UseEnv("nvidia"); err == nil {
		t.Fatal("soltar pin sem credencial deveria falhar")
	}
}

func TestLogoutRemovesKeyAndPin(t *testing.T) {
	svc, store := newSvc(t, &fakeVerifier{})
	if _, err := svc.Connect(context.Background(), "nvidia", "https://x/v1", candKey, ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout("nvidia"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Get("nvidia"); err == nil {
		t.Fatal("logout nao apagou a credencial")
	}
}

// --- Status: a UI precisa saber a FONTE -----------------------------------

func TestStatusReportsSource(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-ambiente")
	v := &fakeVerifier{}
	svc, _ := newSvc(t, v)

	// Sem nada: orienta /connect.
	st := svc.StatusFor("nvidia", "NVIDIA_API_KEY")
	if st.Configured {
		t.Fatalf("deveria estar sem credencial: %+v", st)
	}
	if !strings.Contains(st.Describe(), "/connect") {
		t.Fatalf("Describe() deveria orientar /connect: %q", st.Describe())
	}

	// So com env: a origem tem de ser env.
	st = svc.StatusFor("openrouter", "OPENROUTER_API_KEY")
	if !st.Configured || st.Source != creds.SourceEnv {
		t.Fatalf("openrouter: %+v", st)
	}
	if !strings.Contains(st.Describe(), "env") {
		t.Fatalf("Describe() = %q, deveria dizer env", st.Describe())
	}

	// Apos /connect: origem store fixada. Este e o ponto do D-66.
	if _, err := svc.Connect(context.Background(), "nvidia", "https://x/v1", candKey, ""); err != nil {
		t.Fatal(err)
	}
	st = svc.StatusFor("nvidia", "NVIDIA_API_KEY")
	if !st.Configured || st.Source != creds.SourceStoreFixed {
		t.Fatalf("apos /connect: %+v", st)
	}
	if strings.Contains(st.Describe(), candKey) {
		t.Fatalf("Describe() vazou a chave: %q", st.Describe())
	}
}

// --- Segredo: em nenhum lugar fora do store -------------------------------

func TestSecretNeverLeaksInErrorsOrStatus(t *testing.T) {
	v := &fakeVerifier{}
	svc, _ := newSvc(t, v)

	// Erro de verificacao (nao-recusado) com a chave no meio.
	v.err = errors.New("dial tcp " + candKey)
	_, err := svc.Connect(context.Background(), "nvidia", "https://x/v1", candKey, "")
	if err == nil {
		t.Fatal("esperava erro")
	}
	if strings.Contains(err.Error(), candKey) {
		t.Fatalf("erro de Connect carrega a chave: %v", err)
	}
	if strings.Contains(err.Error(), candKey) {
		t.Fatalf("erro de Connect carrega a chave: %v", err)
	}

	// Provider invalido com a chave colada.
	if _, err := svc.Connect(context.Background(), "../x", "https://x/v1", candKey, ""); err != nil {
		if strings.Contains(err.Error(), candKey) {
			t.Fatalf("erro de provider invalido carrega a chave: %v", err)
		}
	}

	// Result nao carrega a chave.
	v.err = nil
	res, err := svc.Connect(context.Background(), "nvidia", "https://x/v1", candKey, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.String(), candKey) {
		t.Fatalf("Result.String() carrega a chave: %s", res.String())
	}
}

// --- Verificador nil: guarda nao verificavel ------------------------------

// Um Service sem verificador guarda sem provar nada. Isso existe para teste e
// para um caminho explicito --no-verify; nunca deve ser o padrao, e por isso o
// Result deixa isso visivel.
func TestNoVerifierIsNotSilent(t *testing.T) {
	svc, store := newSvc(t, nil)
	res, err := svc.Connect(context.Background(), "nvidia", "https://x/v1", candKey, "")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Stored {
		t.Fatal("deveria guardar")
	}
	if res.Verified {
		t.Fatal("Result.Verified nao pode ser true sem verificador")
	}
	if _, _, err := store.Get("nvidia"); err != nil {
		t.Fatalf("nao guardou: %v", err)
	}
}
