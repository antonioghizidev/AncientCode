package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/antonioghizidev/ancientcode/internal/config"
	"github.com/antonioghizidev/ancientcode/internal/connect"
	"github.com/antonioghizidev/ancientcode/internal/creds"
	"github.com/antonioghizidev/ancientcode/internal/loop"
)

// runConnectREPL implements /connect on the line REPL.
//
// The rules it must never break (M14 §3.5, D-2026-10-02-66):
//   - the key is asked for interactively, never taken from the command line
//     (argv is visible in /proc/<pid>/cmdline and in shell history);
//   - the key is verified against the provider before it is stored;
//   - a rejected key is not stored, and the message says which provider said so;
//   - on success the source is reported, never a bare "saved".
func runConnectREPL(ctx context.Context, cmd command, svc *connect.Service, agent *loop.Loop, cfg *config.Config) {
	if svc == nil {
		fmt.Fprintln(os.Stderr, "erro: /connect indisponivel (store de credenciais nao inicializado)")
		return
	}

	name, err := connectTarget(cmd.arg, cmd.connOpt.useEnv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		return
	}

	// --use-env: solta o pin sem apagar a chave.
	if cmd.connOpt.useEnv {
		if name == "" {
			name = agent.CurrentProvider()
		}
		if err := svc.UseEnv(name); err != nil {
			fmt.Fprintln(os.Stderr, "erro:", err)
			return
		}
		fmt.Printf("provider %s: pin solto; a chave do ambiente volta a valer\n", name)
		fmt.Println("  a chave guardada foi mantida; /connect <p> reativa o pin")
		return
	}

	if name == "" {
		name = agent.CurrentProvider()
	}

	// Verificacao exige terminal para a chave ser digitada sem eco.
	if !hasStdinTerminal() {
		fmt.Fprintln(os.Stderr, "erro: /connect precisa de um terminal para ler a chave sem eco")
		fmt.Fprintln(os.Stderr, "      alternativa: export "+envNameFor(agent, name)+"=... e rode sem /connect")
		return
	}

	baseURL := agent.CurrentProviderBaseURL()
	if b, _, ok := agent.ProviderBaseURLFor(name); ok && name != agent.CurrentProvider() {
		baseURL = b
	}

	key, err := readSecret(fmt.Sprintf("chave de %s (nao sera exibida): ", name))
	if err != nil {
		if errors.Is(err, errNotTerminal) {
			fmt.Fprintln(os.Stderr, "erro: /connect precisa de um terminal para ler a chave sem eco")
			return
		}
		fmt.Fprintln(os.Stderr, "erro:", err)
		return
	}
	key = trimSecret(key)
	if key == "" {
		fmt.Fprintln(os.Stderr, "erro: chave vazia; nada foi guardado")
		return
	}

	res, err := svc.Connect(ctx, name, baseURL, key, agent.CurrentModel())
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		// As duas recusas sao distintas e o operador precisa saber qual houve:
		// "recusada" manda revisar a chave; "não confirmada" diz que o
		// provedor não respondeu o suficiente para dizer qualquer coisa.
		if errors.Is(err, connect.ErrRejected) {
			fmt.Fprintln(os.Stderr, "      a chave foi recusada pelo provedor; nada foi guardado")
		} else if errors.Is(err, connect.ErrUnconfirmed) {
			fmt.Fprintln(os.Stderr, "      o provedor nao deu uma resposta definitiva; nada foi guardado")
			fmt.Fprintln(os.Stderr, "      (a chave pode estar correta — tente de novo, ou export "+envNameFor(agent, name)+"=...)")
		}
		return
	}

	switch {
	case res.Replaced:
		fmt.Printf("provider %s: chave substituida, verificada, guardada%s\n", name, verifiedCatalogNote(res))
	default:
		fmt.Printf("provider %s: chave guardada, verificada%s\n", name, verifiedCatalogNote(res))
	}
	fmt.Printf("  origem da chave em uso: %s\n", res.Source)
	if name != agent.CurrentProvider() {
		fmt.Printf("  /provider %s para usar este provider\n", name)
	}
	if model, ok, _ := lastModelFor(svc, name); ok {
		fmt.Printf("  ultimo modelo registrado: %s (/models)\n", model)
	}
	_ = cfg
}

// connectTarget validates the optional provider argument. A flag where a
// provider name was expected is refused rather than treated as a name.
//
// useEnv must be passed in, and the reason is a bug the Inspiron smoke found:
// the argument reaches here as the whole remainder ("nvidia --use-env"), so
// validating it verbatim rejected the very flag this command is meant to
// accept — and /connect --use-env silently did nothing, leaving the pin set.
// The known flag is stripped; anything else starting with "-" is still an
// unknown option and must be reported.
func connectTarget(arg string, useEnv bool) (string, error) {
	var providerTokens []string
	for _, tok := range strings.Fields(arg) {
		if strings.HasPrefix(tok, "-") {
			if useEnv && tok == "--use-env" {
				continue
			}
			return "", fmt.Errorf("opcao desconhecida %q; use /connect [provedor] [--use-env]", tok)
		}
		providerTokens = append(providerTokens, tok)
	}
	name := strings.ToLower(strings.Join(providerTokens, ""))
	if len(providerTokens) > 1 {
		return "", fmt.Errorf("um provider por vez; voce passou %d", len(providerTokens))
	}
	if name == "" {
		return "", nil
	}
	if err := creds.ValidateProvider(name); err != nil {
		return "", fmt.Errorf("provedor invalido %q; use /connect para ver as opcoes", name)
	}
	return name, nil
}

// envNameFor returns the api_key_env name of a provider, so messages can name
// the variable without ever naming its value.
func envNameFor(agent *loop.Loop, name string) string {
	if _, env, ok := agent.ProviderBaseURLFor(name); ok && env != "" {
		return env
	}
	return agent.CurrentAPIKeyEnv()
}

// lastModelFor reads the remembered model for a provider from the state store.
// It is best-effort UI sugar: a missing state file must never break /connect.
func lastModelFor(svc *connect.Service, name string) (string, bool, error) {
	st, err := stateStore()
	if err != nil {
		return "", false, err
	}
	return st.GetModel(name)
}
