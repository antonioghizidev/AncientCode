package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/antonioghizidev/ancientcode/internal/connect"
	"github.com/antonioghizidev/ancientcode/internal/creds"
)

// newCredSvcTemp builds a connect.Service over a temporary store, so the tests
// never touch the real ~/.local/share/ancientcode.
func newCredSvcTemp(t *testing.T) *connect.Service {
	t.Helper()
	store, err := creds.Open(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	return connect.New(store, nil)
}

func assertStoreEmpty(t *testing.T, svc *connect.Service) {
	t.Helper()
	names, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Fatalf("o store deveria estar vazio, tem %v", names)
	}
}

// --- A chave NUNCA entra por argv ------------------------------------------

// S5 de M14. A propriedade nao e "readSecret funciona": e que nao existe
// caminho nenhum que aceite a chave como argumento. Um teste por interface.
func TestNoPathAcceptsKeyAsArgv(t *testing.T) {
	// Uma chave colada como argumento tem de ser reconhecida e recusada.
	for _, k := range []string{
		"sk-or-v1-abc123",
		"nvapi-abc123",
		"AIzaSyABC123",
		"gsk_abc123",
		"xai-abc123",
	} {
		if !looksLikeKey(k) {
			t.Errorf("looksLikeKey(%q) = false: uma chave real passaria como argumento", k)
		}
	}
	// E provider/flags tem de continuar sendo reconhecidos como nao-chave, senao
	// a recusa bloquearia o uso legitimo.
	for _, s := range []string{"nvidia", "openrouter", "--use-env", "-h", "litellm"} {
		if looksLikeKey(s) {
			t.Errorf("looksLikeKey(%q) = true: nao e uma chave e o refused bloquearia uso legitimo", s)
		}
	}
}

// --- /connect e /provider sao comandos reconhecidos no parser compartilhado --

func TestConnectIsParsedInEveryFrontend(t *testing.T) {
	c, ok := parseCommand("/connect")
	if !ok || c.kind != cmdConnect {
		t.Fatalf("/connect nao foi reconhecido: ok=%v kind=%v", ok, c.kind)
	}
	c, ok = parseCommand("/connect nvidia")
	if !ok || c.kind != cmdConnect || c.arg != "nvidia" {
		t.Fatalf("/connect nvidia: ok=%v kind=%v arg=%q", ok, c.kind, c.arg)
	}
	c, ok = parseCommand("/connect nvidia --use-env")
	if !ok || c.kind != cmdConnect || !c.connOpt.useEnv {
		t.Fatalf("--use-env nao foi lido: ok=%v useEnv=%v", ok, c.connOpt.useEnv)
	}
	// Case-insensitive, como os demais comandos.
	if c, ok := parseCommand("/CONNECT NVIDIA"); !ok || c.kind != cmdConnect {
		t.Fatal("/CONNECT deveria ser reconhecido")
	}
}

func TestConnectArgIsValidated(t *testing.T) {
	// Uma opcao onde se esperava um nome de provider tem de ser recusada, e
	// nao tratada como provider.
	if _, err := connectTarget("--use-env"); err == nil {
		t.Fatal("connectTarget(--use-env) deveria falhar")
	}
	// Traversal, separadores, espacos e controle EMBUTIDO: recusados.
	for _, bad := range []string{"../etc", "a/b", "a b", "nvidia; rm -rf /", "nvid\nia", "nvid\ta"} {
		if _, err := connectTarget(bad); err == nil {
			t.Fatalf("connectTarget(%q) deveria falhar", bad)
		}
	}
	// Newline no FIM e aceitavel: e o que sobra de uma colagem.
	if name, err := connectTarget("nvidia\n"); err != nil || name != "nvidia" {
		t.Fatalf("connectTarget(\"nvidia\\n\") = %q, %v", name, err)
	}
	// Vazio significa "o provider ativo": nao e erro.
	name, err := connectTarget("")
	if err != nil || name != "" {
		t.Fatalf("connectTarget(\"\") = %q, %v", name, err)
	}
	// Maiusculas sao normalizadas, igual a /provider faz: recusar "NVIDIA" seria
	// uma teimosia sem ganho de seguranca.
	for _, in := range []string{"NVIDIA", "  Nvidia  ", "OpenRouter"} {
		name, err := connectTarget(in)
		if err != nil {
			t.Fatalf("connectTarget(%q) = %v, deveria normalizar", in, err)
		}
		if name != strings.ToLower(strings.TrimSpace(in)) {
			t.Fatalf("connectTarget(%q) = %q", in, name)
		}
	}
}

// --- Onboarding sem TTY: instrucao, exit 1, e NADA escrito -----------------

// S9/S10 de M14. Sem terminal, o onboarding nao abre dialogo (travar num pipe e
// pior que sair com erro) e nao escreve nada em disco.
func TestOnboardingWithoutTerminalWritesNothingAndRefuses(t *testing.T) {
	svc := newCredSvcTemp(t) // store em diretorio temporario
	// hasStdinTerminal() e falso nos testes (stdin nao e char device).
	if hasStdinTerminal() {
		t.Skip("stdin e terminal neste ambiente; o caminho sem TTY nao se aplica")
	}
	deployed, err := runOnboarding(t.Context(), svc)
	if err != nil {
		t.Fatalf("sem TTY o onboarding nao deveria dar erro: %v", err)
	}
	if deployed {
		t.Fatal("sem TTY nada pode ser instalado")
	}
	assertStoreEmpty(t, svc)
}

// --- A regra do env para o onboarding -------------------------------------

// Quem ja exportou a chave nao pode seruve de conectar: seria o harness
// insistindo no armazenamento proprio sobre uma configuracao que funciona.
func TestOnboardingSkippedWhenEnvCredentialExists(t *testing.T) {
	t.Setenv("NVIDIA_API_KEY", "chave-que-ja-existe")
	svc := newCredSvcTemp(t)
	ok, err := svc.HasAny(knownCandidates())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("com NVIDIA_API_KEY no ambiente, HasAny deveria dizer que ja esta configurado")
	}
	assertStoreEmpty(t, svc)
}

func TestOnboardingRunsWhenNothingConfigured(t *testing.T) {
	// Garante que nenhuma das variaveis conhecidas esteja setadas.
	for _, c := range knownCandidates() {
		t.Setenv(c.EnvName, "")
	}
	svc := newCredSvcTemp(t)
	ok, err := svc.HasAny(knownCandidates())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("sem store e sem env, HasAny deveria dizer que falta configurar")
	}
}

// --- /provider mostra a origem ----------------------------------------------

func TestStatusDescribeNamesSourceAndNeverTheKey(t *testing.T) {
	svc := newCredSvcTemp(t)
	st := svc.StatusFor("nvidia", "NVIDIA_API_KEY")
	if st.Describe() == "" {
		t.Fatal("Describe() vazio")
	}
	if strings.Contains(st.Describe(), "nvapi-") || strings.Contains(st.Describe(), "sk-") {
		t.Fatalf("Describe() pode conter material de chave: %q", st.Describe())
	}
	// Sem credencial, orienta /connect — o operador precisa saber o proximo passo.
	if !strings.Contains(st.Describe(), "/connect") {
		t.Fatalf("sem credencial, Describe() = %q, deveria orientar /connect", st.Describe())
	}
}

// A ajuda tem que listar /connect, senao o comando existe mas e invisivel.
func TestHelpListsConnect(t *testing.T) {
	if !strings.Contains(helpText, "/connect") {
		t.Fatal("a ajuda nao menciona /connect")
	}
	if !strings.Contains(helpText, "/provider") {
		t.Fatal("a ajuda nao menciona /provider")
	}
}
