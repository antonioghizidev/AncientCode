package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const verifyTestKey = "chave-de-teste-que-nunca-e-impressa"

// publicCatalogServer reproduces what the REAL providers do, as measured on
// 2026-10-02 against integrate.api.nvidia.com and openrouter.ai:
//
//	GET /models  ->  200 with a valid key, with an INVALID key, and with none.
//
// The old implementation verified with this endpoint and therefore accepted any
// pasted string as "verified" — the smoke caught it by logging in with a
// deliberately wrong key and being told the store had 80 verified models.
//
// This test is the regression for exactly that: a public catalog MUST NOT make
// verification succeed.
func publicCatalogServer(t *testing.T, hits *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			atomic.AddInt32(hits, 1)
			// Identical body whatever the Authorization header says — which is
			// precisely what makes this endpoint useless as an identity check.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"a/1"},{"id":"b/2"},{"id":"c/3"}]}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/key") {
			if r.Header.Get("Authorization") == "Bearer "+verifyTestKey {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"data":{"label":"sk-or-v1-abc"}}`))
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

// --- a licao central --------------------------------------------------------

func TestPublicCatalogAloneNeverCountsAsVerified(t *testing.T) {
	var hits int32
	srv := publicCatalogServer(t, &hits)
	defer srv.Close()

	// This is what the old code did: hit the catalog, see 200, declare victory.
	// It must now be an explicit failure mode for a provider that has no
	// identity endpoint and no working chat probe.
	_, err := VerifyCredential(context.Background(), srv.Client(), "openrouter", srv.URL, "chave-errada-qualquer")
	if err == nil {
		t.Fatal("uma chave errada foi considerada verificada: o catalogo publico basta para passar")
	}
	if !errors.Is(err, ErrUnauthorized) && !errors.Is(err, ErrInconclusive) {
		t.Fatalf("esperava ErrUnauthorized ou ErrInconclusive, veio %v", err)
	}
}

// A key the provider actually rejects must be ErrUnauthorized (definitive),
// and a probe that merely failed must be ErrInconclusive (unknown). Collapsing
// the two is how the operator gets told "your key is wrong" by a rate limiter.
func TestOutcomeDistinguishesRejectedFromUnconfirmed(t *testing.T) {
	for name, status := range map[string]int{
		"401": http.StatusUnauthorized,
		"403": http.StatusForbidden,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()
			_, err := VerifyCredential(context.Background(), srv.Client(), "nvidia", srv.URL, "k")
			if !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("HTTP %d deveria ser ErrUnauthorized, veio %v", status, err)
			}
			if errors.Is(err, ErrInconclusive) {
				t.Fatal("recusa definitiva nao pode virar inconclusivo")
			}
		})
	}

	for name, status := range map[string]int{
		"429": http.StatusTooManyRequests,
		"500": http.StatusInternalServerError,
		"503": http.StatusServiceUnavailable,
	} {
		t.Run("inconclusivo-"+name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()
			_, err := VerifyCredential(context.Background(), srv.Client(), "nvidia", srv.URL, "k")
			if !errors.Is(err, ErrInconclusive) {
				t.Fatalf("HTTP %d deveria ser ErrInconclusive, veio %v", status, err)
			}
			if errors.Is(err, ErrUnauthorized) {
				t.Fatal("rate limit nao pode ser reportado como chave invalida")
			}
		})
	}
}

// A 404 from the chat probe means auth PASSED (the provider resolves auth before
// the model) but that model cannot serve. So the probe must try another model
// rather than declaring the key bad.
func TestChatProbeRetriesAnotherModelOn404(t *testing.T) {
	var attempts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/models"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"morto/1"},{"id":"vivo/2"}]}`))
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			body := readAllLimited(r)
			attempts = append(attempts, body)
			if strings.Contains(body, "morto/1") {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"detail":"model retired"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	res, err := VerifyCredential(context.Background(), srv.Client(), "nvidia", srv.URL, verifyTestKey)
	if err != nil {
		t.Fatalf("um 404 num modelo deveria tentar outro, nao falhar: %v", err)
	}
	if res.Outcome != OutcomeOK {
		t.Fatalf("Outcome = %v, queria OutcomeOK", res.Outcome)
	}
	if len(attempts) < 2 {
		t.Fatalf("esperava 2 tentativas (modelo morto, depois vivo), houve %d", len(attempts))
	}
	if !strings.Contains(attempts[len(attempts)-1], "vivo/2") {
		t.Fatalf("a segunda tentativa nao usou o modelo vivo: %q", attempts[len(attempts)-1])
	}
}

// Se TODOS os candidatos derem 404, o resultado e inconclusivo — nunca
// "verificada" (autenticou nao e o mesmo que consegue responder) e nunca
// "chave invalida" (a autenticacao passou).
func TestChatProbeAllModelsRetiredIsInconclusive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"a/1"},{"id":"b/2"},{"id":"c/3"},{"id":"d/4"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"model retired"}`))
	}))
	defer srv.Close()

	_, err := VerifyCredential(context.Background(), srv.Client(), "nvidia", srv.URL, verifyTestKey)
	if !errors.Is(err, ErrInconclusive) {
		t.Fatalf("todos os modelos aposentados deveria dar inconclusivo, veio %v", err)
	}
	if errors.Is(err, ErrUnauthorized) {
		t.Fatal("nao pode dizer que a chave e invalida: ela passou pela autenticacao")
	}
}

// A sondagem e limitada: "tentar ate algo responder" seria um laco aberto quando
// o catalogo inteiro esta morto.
func TestChatProbeAttemptsAreBounded(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.WriteHeader(http.StatusOK)
			var sb strings.Builder
			sb.WriteString(`{"data":[`)
			for i := 0; i < 50; i++ {
				if i > 0 {
					sb.WriteString(",")
				}
				fmt.Fprintf(&sb, `{"id":"m/%d"}`, i)
			}
			sb.WriteString(`]}`)
			_, _ = w.Write([]byte(sb.String()))
			return
		}
		atomic.AddInt32(&n, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := VerifyCredential(context.Background(), srv.Client(), "nvidia", srv.URL, verifyTestKey); err == nil {
		t.Fatal("esperava inconclusivo")
	}
	if got := atomic.LoadInt32(&n); got > chatProbeCandidates {
		t.Fatalf("tentou %d modelos, o limite e %d", got, chatProbeCandidates)
	}
}

// A estrategia segue o provedor.
func TestVerifyStrategyPerProvider(t *testing.T) {
	if got := verifyStrategyFor("openrouter"); got != strategyKeyIdentity {
		t.Error("openrouter deveria usar /key (medido: 401 vs 200)")
	}
	if got := verifyStrategyFor("nvidia"); got != strategyChatProbe {
		t.Error("nvidia nao tem endpoint de identidade; deveria sondar o chat")
	}
	if got := verifyStrategyFor("litellm"); got != strategyChatProbe {
		t.Error("litellm nao tem contrato medido; a sondagem e a escolha conservadora")
	}
}

// O endpoint /key do openrouter e barato e definitivo: 401 invalida, 200 vale.
func TestKeyIdentityEndpointSettlesIt(t *testing.T) {
	var hits int32
	srv := publicCatalogServer(t, &hits)
	defer srv.Close()

	res, err := VerifyCredential(context.Background(), srv.Client(), "openrouter", srv.URL, verifyTestKey)
	if err != nil || res.Outcome != OutcomeOK {
		t.Fatalf("chave valida deveria verificar: %v (%v)", err, res.Outcome)
	}
	_, err = VerifyCredential(context.Background(), srv.Client(), "openrouter", srv.URL, "errada")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("chave invalida em /key deveria dar ErrUnauthorized, veio %v", err)
	}
}

// --- invariantes de seguranca, inalteradas ---------------------------------

func TestVerifyRefusesEmptyKeyAndInsecureBase(t *testing.T) {
	if _, err := VerifyCredential(context.Background(), http.DefaultClient, "nvidia", "https://x/v1", "  "); err == nil {
		t.Fatal("chave vazia deveria ser recusada")
	}
	// http nao-loopback continua proibido: a chave candidata nunca esteve
	// terminalmente auditada.
	//
	// O teste exige o erro DE TRANSPORTE, nao "algum erro". Antes aceitava
	// qualquer erro, e remover a checagem de base URL nao o fez falhar: a
	// requisicao para o host inexistente falhava por DNS e o teste seguia
	// verde. Era a mesma armadilha da mutacao que nao muda nada.
	_, err := VerifyCredential(context.Background(), http.DefaultClient, "nvidia", "http://exemplo.example/v1", verifyTestKey)
	if err == nil {
		t.Fatal("http para host nao-loopback deveria ser recusado")
	}
	if !strings.Contains(err.Error(), "insecure") || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("o erro deveria ser sobre transporte, nao sobre a request: %v", err)
	}
}

// O id do modelo vem de um catalogo remoto e nao pode ser concatenado em JSON
// sem escape — senao um id malicioso no catalogo reescreve o corpo da request.
func TestProbeModelIDIsJSONEscaped(t *testing.T) {
	if got := jsonString(`a","x":"y`); strings.Contains(got[1:len(got)-1], `"x"`) {
		t.Fatalf("o id do modelo nao foi escapado: %s", got)
	}
}

// A chave nunca aparece num erro.
func TestVerifyErrorNeverCarriesTheKey(t *testing.T) {
	const secret = "nvapi-secreta-que-nao-pode-vazar-em-erro"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// um proxy que devolve o header de autorizacao no corpo do erro
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"echo":"Bearer ` + secret + `"}`))
	}))
	defer srv.Close()
	_, err := VerifyCredential(context.Background(), srv.Client(), "nvidia", srv.URL, secret)
	if err == nil {
		t.Fatal("esperava erro")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("a chave vazou no erro: %v", err)
	}
}

func readAllLimited(r *http.Request) string {
	buf := make([]byte, 4096)
	n, _ := r.Body.Read(buf)
	return string(buf[:n])
}
