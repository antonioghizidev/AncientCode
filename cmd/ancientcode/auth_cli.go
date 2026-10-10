package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/antonioghizidev/ancientcode/internal/config"
	"github.com/antonioghizidev/ancientcode/internal/connect"
	"github.com/antonioghizidev/ancientcode/internal/creds"
	"github.com/antonioghizidev/ancientcode/internal/loop"
	"github.com/antonioghizidev/ancientcode/internal/provider"
	"github.com/antonioghizidev/ancientcode/internal/state"
)

// verifyTimeout bounds the WHOLE credential check — every attempt, not each one.
//
// It has to cover the number of attempts, not a single call. Measured on the
// Inspiron: NVIDIA's cold model start takes tens of seconds per request, and the
// chat probe may walk a dozen catalog entries looking for one that answers
// (see chatProbeCandidates). At 90s the budget covered roughly ONE attempt, so
// the probe died on the third with "context deadline exceeded" and a perfectly
// valid key was refused as unconfirmed.
//
// Six minutes is bounded but honest about the work. The operator is looking at a
// masked prompt with a visible progress line, not at a hung process, and the
// per-request context is still what stops a single hung call.
const verifyTimeout = 6 * time.Minute

// verifyProbeTimeout bounds ONE request of the probe, so a single hung call
// cannot eat the whole budget and hide the remaining attempts.
const verifyProbeTimeout = 75 * time.Second

// credStore opens (or creates the directory for) the credential store.
func credStore() (*creds.Store, error) {
	path, err := creds.DefaultPath()
	if err != nil {
		return nil, err
	}
	return creds.Open(path)
}

// stateStore opens the preference store (last model per provider).
func stateStore() (*state.Store, error) {
	path, err := state.DefaultPath()
	if err != nil {
		return nil, err
	}
	return state.Open(path)
}

// cdPersister returns the preference store used to remember the last /cd.
//
// Returning nil on failure is deliberate: persistence is a convenience, and a
// state file that cannot be opened must not stop /cd from working in this
// session. persistRoot treats nil as "do not persist".
func cdPersister(_ *config.Config) rootPersister {
	st, err := stateStore()
	if err != nil {
		return nil
	}
	return st
}

// credSvc builds the connect service wired to a real HTTP verifier.
func credSvc() (*connect.Service, error) {
	store, err := credStore()
	if err != nil {
		return nil, err
	}
	return connect.New(store, connect.HTTPVerifier{
		Client: &http.Client{Timeout: verifyTimeout},
	}), nil
}

// credSvcForTUI builds the connect.Service the TUI panel needs. It returns nil
// when the store cannot be opened; the TUI then refuses /connect with an
// explanatory banner instead of pretending the panel works.
func credSvcForTUI() *connect.Service {
	svc, err := credSvc()
	if err != nil {
		return nil
	}
	return svc
}

// runAuthCLI implements `ancientcode auth <login|logout|list>`.
//
// No subcommand ever accepts a key as an argument. `auth login` asks on the
// terminal, because argv is world-readable via /proc/<pid>/cmdline and lands in
// shell history (M14 §3.5).
func runAuthCLI(ctx context.Context, args []string) int {
	if len(args) == 0 {
		authUsage()
		return 2
	}
	sub := strings.ToLower(args[0])
	rest := args[1:]

	store, err := credStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		return 1
	}
	svc := connect.New(store, connect.HTTPVerifier{
		Client: &http.Client{Timeout: verifyTimeout},
	})

	switch sub {
	case "list", "ls":
		names, err := store.List()
		if err != nil {
			fmt.Fprintln(os.Stderr, "erro:", err)
			return 1
		}
		if len(names) == 0 {
			fmt.Println("nenhuma credencial guardada (rode: ancientcode auth login)")
			return 0
		}
		fmt.Println("providers com credencial guardada:")
		for _, n := range names {
			_, fixed, err := store.Get(n)
			if err != nil {
				fmt.Fprintln(os.Stderr, "erro:", err)
				return 1
			}
			src := creds.SourceStoreLoose
			if fixed {
				src = creds.SourceStoreFixed
			}
			fmt.Printf("  %-12s %s\n", n, src)
		}
		return 0

	case "login":
		return authLogin(ctx, svc, rest)

	case "logout":
		name, err := oneProviderArg(rest)
		if err != nil {
			fmt.Fprintln(os.Stderr, "erro:", err)
			return 2
		}
		if err := svc.Logout(name); err != nil {
			fmt.Fprintln(os.Stderr, "erro:", err)
			return 1
		}
		fmt.Printf("credencial e pin removidos para %s\n", name)
		return 0
	}

	fmt.Fprintf(os.Stderr, "erro: subcomando desconhecido %q\n", sub)
	authUsage()
	return 2
}

// banner builds the first lines the operator sees, as a PURE function.
//
// It used to be an inline Fprintf reading cfg.Provider and cfg.Model, and the
// Inspiron smoke caught why that is a bug waiting to happen: the session was
// running the model restored from state.json while the banner announced the
// YAML's. cfg.Provider has the same problem — it never changes on /provider.
// Everything here is asked of the loop, which is the only component that knows
// what is actually in use.
//
// Pure so it can be tested: an Fprintf in main() had no test at all, and that
// missing test is how the defect survived a green suite.
//
// projectRoot is asked of the WORKSPACE, not of cfg, for the same reason the
// provider is asked of the loop: the banner must report what is in use, and
// /cd changes the root after startup.
func banner(version string, agent *loop.Loop, sessionName, toolboxMode, projectRoot string) string {
	provider, model := "", ""
	restored := false
	if agent != nil {
		provider, model = agent.CurrentProvider(), agent.CurrentModel()
		restored = agent.ModelRestoredFromMemory()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ancientcode %s — provider=%s model=%s session=%s\n",
		version, provider, model, sessionName)
	if projectRoot != "" {
		// Always visible (M15): without it, `pwd` in a shell and the folder the
		// file tools confine to can disagree, and the operator edits one project
		// believing it is another.
		fmt.Fprintf(&b, "projeto: %s\n", projectRoot)
	}
	if restored && model != "" {
		fmt.Fprintf(&b, "modelo restaurado de state.json (provider %s)\n", provider)
	}
	fmt.Fprintf(&b, "toolbox_mode=%s (plan: nada mutante executa sem o seu 'y')\n%s\n",
		toolboxMode, helpText)
	return b.String()
}

// the banner can say so instead of leaving the operator to wonder why the model
// differs from the YAML (the Inspiron smoke found exactly that confusion).
func rememberedModelFor(agent *loop.Loop) (string, bool, error) {
	if agent == nil {
		return "", false, nil
	}
	st, err := stateStore()
	if err != nil {
		return "", false, err
	}
	model, ok, err := st.GetModel(agent.CurrentProvider())
	return model, ok, err
}

// configuredModelHint returns the model from the config, so credential
// verification probes the model the session will actually use.
//
// This is not cosmetic. Measured on the Inspiron: NVIDIA's catalog leads with
// models that have RETIRED for the account (they answer 404 to a valid key), so
// probing the first catalog entries refused a working key as "inconclusive". The
// configured model is both the fairest test and the likeliest to answer.
//
// Best effort by design: a missing or unreadable config yields "", and the probe
// falls back to the provider catalog. Failing to read a config must not stop the
// operator from connecting.
func configuredModelHint() string {
	cfg, err := config.Load(*flagConfig)
	if err != nil {
		return ""
	}
	return cfg.Model
}

// verifiedCatalogNote renders the model count only when it is actually KNOWN.
//
// The Inspiron smoke printed "guardada e verificada (0 modelos)" for OpenRouter,
// which publishes 464: the catalog read hit a 64 KiB cap, the parse failed, and
// zero was reported as if it were a fact. Saying nothing is honest; saying "0"
// is not.
func verifiedCatalogNote(res connect.Result) string {
	if !res.CatalogKnown {
		return ""
	}
	return fmt.Sprintf(" (%d modelos)", res.ModelCount)
}
func authLogin(ctx context.Context, svc *connect.Service, args []string) int {
	// Uma chave passada como argumento seria lida por qualquer usuario do
	// mesmo host e ficaria no historico. Recusamos explicitamente para que a
	// recusa ensine o caminho certo.
	for _, a := range args {
		if !strings.HasPrefix(a, "-") && looksLikeKey(a) {
			fmt.Fprintln(os.Stderr, "erro: a chave nao vai no comando.")
			fmt.Fprintln(os.Stderr, "      argumente fica visivel em /proc/<pid>/cmdline e no historico do shell.")
			fmt.Fprintln(os.Stderr, "      use: ancientcode auth login <provedor>  e cole a chave no prompt.")
			return 2
		}
	}
	name, err := oneProviderArg(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		return 2
	}
	if !hasStdinTerminal() {
		fmt.Fprintln(os.Stderr, "erro: auth login precisa de um terminal para ler a chave sem eco")
		return 1
	}

	baseURL, env, ok := config.LookupProviderDefault(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "erro: provider %q desconhecido\n", name)
		return 2
	}

	key, err := readSecret(fmt.Sprintf("chave de %s (nao sera exibida): ", name))
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		return 1
	}
	key = trimSecret(key)
	if key == "" {
		fmt.Fprintln(os.Stderr, "erro: chave vazia; nada foi guardado")
		return 1
	}

	res, err := svc.Connect(ctx, name, baseURL, key, configuredModelHint())
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		if errors.Is(err, connect.ErrRejected) {
			fmt.Fprintln(os.Stderr, "      a chave foi recusada pelo provedor; nada foi guardado")
		} else if errors.Is(err, connect.ErrUnconfirmed) {
			fmt.Fprintln(os.Stderr, "      o provedor nao deu uma resposta definitiva; nada foi guardado")
			fmt.Fprintln(os.Stderr, "      (a chave pode estar correta — tente de novo, ou export "+env+"=...)")
		}
		return 1
	}
	if res.Replaced {
		fmt.Printf("provider %s: chave substituida e verificada%s\n", name, verifiedCatalogNote(res))
	} else {
		fmt.Printf("provider %s: chave guardada e verificada%s\n", name, verifiedCatalogNote(res))
	}
	return 0
}

// looksLikeKey is a heuristic for the argv refusal. It is deliberately broad:
// a false positive costs a confusing message, a false negative leaks the key.
func looksLikeKey(s string) bool {
	for _, p := range []string{"sk-", "nvapi-", "sk-or-v1-", "AIza", "gsk_", "xai-"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func oneProviderArg(args []string) (string, error) {
	var names []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			names = append(names, strings.ToLower(strings.TrimSpace(a)))
		}
	}
	if len(names) == 0 {
		return "", errors.New("informe o provider (ex.: auth login nvidia)")
	}
	if len(names) > 1 {
		return "", fmt.Errorf("um provider por vez; voce passou %d", len(names))
	}
	if err := creds.ValidateProvider(names[0]); err != nil {
		return "", err
	}
	return names[0], nil
}

// knownCandidates lists every built-in provider with the env variable that
// would supply its key. The onboarding consults all of them so an operator who
// already exported one is not asked to connect.
func knownCandidates() []connect.ProviderCandidate {
	var out []connect.ProviderCandidate
	for _, id := range config.ProviderNames() {
		_, env, ok := config.LookupProviderDefault(id)
		if !ok {
			continue
		}
		out = append(out, connect.ProviderCandidate{ID: id, EnvName: env})
	}
	return out
}

// keyResolverFor builds the per-request key source the provider Client calls.
//
// It is keyed by the api_key_env NAME, not by the provider, because that is the
// value that actually selects a credential: /provider installs a new env name on
// every switch, and the resolver has to follow it (D-2026-10-02-66). The store
// decides with its pin; when it has nothing, the environment is used — so every
// pre-M14 setup keeps working untouched.
//
// The returned closure performs no I/O beyond a small JSON read and never logs.
func keyResolverFor(store *creds.Store) provider.KeyResolver {
	return func(envName string) string {
		if envName == "" {
			return ""
		}
		providerID := providerForEnv(envName)
		if err := creds.ValidateProvider(providerID); err != nil {
			return ""
		}
		key, _, err := store.Resolve(providerID, envName, os.Getenv)
		if err != nil {
			// No credential from either source. The caller reports the env var
			// NAME; the empty string is the signal to do so.
			return ""
		}
		return key
	}
}

// providerForEnv maps an api_key_env name back to a built-in provider id. The
// mapping is ambiguous in theory; for the built-in set it is not, and an
// unmatched name is treated as "no known provider" (fail-closed).
func providerForEnv(envName string) string {
	envName = strings.TrimSpace(envName)
	for _, id := range config.ProviderNames() {
		if _, env, ok := config.LookupProviderDefault(id); ok && env == envName {
			return id
		}
	}
	return ""
}

// storePathDisplay returns the absolute store path for messages, or a short
// marker when the home cannot be resolved.
//
// Spec §3.8 asks for absolute home paths on first use, and a literal "~" fails
// that for the case that matters most: an operator whose shell does not expand
// "~" in a non-interactive context, who then has no idea where the file is.
// Failing closed, we say so rather than printing a path we could not resolve.
func storePathDisplay() string {
	p, err := creds.DefaultPath()
	if err != nil {
		return "(<home nao resolvido>)"
	}
	return p
}

// printNoCredentialHelp explains how to configure a key the non-interactive way.
// It is the branch taken when no credential exists and there is no terminal to
// ask on (D-2026-10-02-67): no dialog, exit 1, and the operator is told exactly
// which variable to export for each provider.
func printNoCredentialHelp() {
	fmt.Fprintln(os.Stderr, "erro: nenhuma credencial configurada para este provider.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "duas formas de configurar:")
	fmt.Fprintln(os.Stderr, "  1. /connect <provedor> dentro da sessao (recomendado), ou")
	fmt.Fprintln(os.Stderr, "     ancientcode auth login <provedor>  (a chave e lida no terminal)")
	fmt.Fprintln(os.Stderr, "  2. variavel de ambiente (para CI e uso nao-interativo):")
	for _, c := range knownCandidates() {
		fmt.Fprintf(os.Stderr, "       export %s=...  # %s\n", c.EnvName, c.ID)
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "as chaves ficam em %s (0600).\n", storePathDisplay())
	fmt.Fprintln(os.Stderr, "config tambem pode apontar api_key_env para um outro nome.")
}

func authUsage() {
	fmt.Fprintln(os.Stderr, `uso:
  ancientcode auth login [provedor]   guarda a chave (pedida no prompt, nunca no argv)
  ancientcode auth logout <provedor> apaga a chave e o pin
  ancientcode auth list              mostra os providers guardados e a origem`)
}

// runOnboarding is the first-use flow of D-2026-10-02-67: when no provider has
// a resolvable credential, offer /connect before Validate can fail with a
// message the operator does not yet understand.
//
// It never opens a dialog without a terminal, and it writes nothing when the
// operator declines.
func runOnboarding(ctx context.Context, svc *connect.Service) (deployed bool, err error) {
	if svc == nil {
		return false, nil
	}
	has, err := svc.HasAny(knownCandidates())
	if err != nil {
		return false, err
	}
	if has {
		return false, nil
	}
	if !hasStdinTerminal() {
		return false, nil // caller prints the env instructions and exits 1
	}
	fmt.Fprintln(os.Stderr, "nenhum provider configurado: nenhuma credencial no store e nenhuma ANCIENTCODE_* definida.")
	fmt.Fprintf(os.Stderr, "vou pedir a chave de um provider agora; ela fica em %s (0600).\n", storePathDisplay())
	fmt.Fprintln(os.Stderr, "  (Ctrl+C cancela; nada e gravado)")
	fmt.Fprintln(os.Stderr, "")

	key, rerr := readSecret("chave de nvidia (nao sera exibida): ")
	if rerr != nil {
		return false, rerr
	}
	key = trimSecret(key)
	if key == "" {
		return false, nil // declined: nothing written, onboarding returns next time
	}
	base, _, _ := config.LookupProviderDefault("nvidia")
	if _, err := svc.Connect(ctx, "nvidia", base, key, configuredModelHint()); err != nil {
		return false, err
	}
	return true, nil
}
