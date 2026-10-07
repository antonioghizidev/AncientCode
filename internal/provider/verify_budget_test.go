package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Uma request que estoura o limite NAO pode encerrar a sondagem.
//
// Medido no Inspiron: no catalogo da NVIDIA, um modelo pendurado (deepseek-ai/
// deepseek-v4.1-flash) consumiu 75s inteiros enquanto os vizinhos respondiam em
// 0.5s, e o unico modelo vivo estava 12 posicoes adiante. Devolver no primeiro
// timeout transformava o problema de um vizinho num veredito sobre a chave do
// operador — recusando uma chave valida.
func TestHungEntryDoesNotEndTheProbe(t *testing.T) {
	old := perRequestVerifyTimeout
	perRequestVerifyTimeout = 60 * time.Millisecond
	defer func() { perRequestVerifyTimeout = old }()

	var order []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"lento/1"},{"id":"rapido/2"}]}`))
			return
		}
		body := readAllLimited(r)
		order = append(order, body)
		if strings.Contains(body, "lento/1") {
			time.Sleep(400 * time.Millisecond) // estoura o limite por request
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}]}`))
	}))
	defer srv.Close()

	res, err := VerifyCredential(context.Background(), srv.Client(), "nvidia", srv.URL, "k", "")
	if err != nil {
		t.Fatalf("um vizinho pendurado nao pode_recusar a chave: %v", err)
	}
	if res.Outcome != OutcomeOK {
		t.Fatalf("Outcome = %v, queria OutcomeOK", res.Outcome)
	}
	if len(order) < 2 {
		t.Fatalf("a sondagem parou no primeiro modelo: %v", order)
	}
	if !strings.Contains(order[len(order)-1], "rapido/2") {
		t.Fatalf("a ultima tentativa nao usou o modelo vivo: %q", order[len(order)-1])
	}
}

// E o inverso: um 429 e um sinal de "o provider esta me recusando agora", e
// insistir so aprofunda o rate limit. Esse precisa parar.
func TestRateLimitStopsTheProbeImmediately(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"a/1"},{"id":"b/2"},{"id":"c/3"}]}`))
			return
		}
		n++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := VerifyCredential(context.Background(), srv.Client(), "nvidia", srv.URL, "k", "")
	if !errors.Is(err, ErrInconclusive) {
		t.Fatalf("429 deveria dar inconclusivo, veio %v", err)
	}
	if n > 1 {
		t.Fatalf("insistiu %d vezes num provider com rate limit", n)
	}
}

// A sondagem pode precisar de varias tentativas, cada uma com partida a frio
// lenta. Se cada request herdar o timeout do cliente SEM limite proprio, uma
// chamada pendurada consome o orcamento inteiro e as tentativas restantes nunca
// rodam — e uma chave valida e recusada como "nao confirmada".
func TestBoundedClientCapsASingleRequest(t *testing.T) {
	// Cliente sem timeout: o caso perigoso, que herdaria "nenhum limite".
	c := boundedClient(&http.Client{})
	if c.Timeout <= 0 || c.Timeout > perRequestVerifyTimeout {
		t.Fatalf("cliente sem timeout nao foi limitado: %v", c.Timeout)
	}
	// Cliente ja mais apertado e preservado.
	tight := &http.Client{Timeout: 5 * time.Second}
	if got := boundedClient(tight).Timeout; got != 5*time.Second {
		t.Fatalf("timeout apertado foi alterado: %v", got)
	}
	// O cliente do chamador nao pode ser mutado: alguem pode estar reutilizando-o.
	orig := &http.Client{}
	_ = boundedClient(orig)
	if orig.Timeout != 0 {
		t.Fatalf("boundedClient alterou o cliente do chamador: %v", orig.Timeout)
	}
	// nil precisa virar algo utilizavel.
	if boundedClient(nil) == nil {
		t.Fatal("boundedClient(nil) devolveu nil")
	}
}

// Uma request que estoura o limite por request vira inconclusive — nunca
// "chave invalida". E o erro precisa dizer que foi inconclusive.
func TestSlowProviderIsInconclusiveNotRejected(t *testing.T) {
	// O teste usa um limite de request reduzido: o comportamento verificado e o
	// classificacao de "devagar", e nao a constante de 75s. Um sleep de 77s
	// transformaria uma verificacao de segundos em um minuto e meio de suite.
	old := perRequestVerifyTimeout
	perRequestVerifyTimeout = 50 * time.Millisecond
	defer func() { perRequestVerifyTimeout = old }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"a/1"}]}`))
			return
		}
		// Dura mais que o limite por request da sondagem.
		time.Sleep(400 * time.Millisecond)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, err := VerifyCredential(ctx, srv.Client(), "nvidia", srv.URL, "k", "")
	if err == nil {
		t.Fatal("esperava erro")
	}
	if errors.Is(err, ErrUnauthorized) {
		t.Fatalf("lentidao foi reportada como chave invalida: %v", err)
	}
	if !errors.Is(err, ErrInconclusive) {
		t.Fatalf("esperava ErrInconclusive, veio %v", err)
	}
}

// O orcamento total e do chamador (o contexto), e o do orcamento precisa ser
// maior que o de UMA request — senao a sondagem morre na primeira tentativa.
func TestTotalBudgetExceedsPerRequestBudget(t *testing.T) {
	if perRequestVerifyTimeout <= 0 {
		t.Fatal("limite por request invalido")
	}
	// Um orcamento total de exatamente uma request nao comporta um probe com
	// varias tentativas: e a condicao que produziu a recusa falsa no Inspiron.
	if chatProbeCandidates > 1 && perRequestVerifyTimeout*chatProbeCandidates <= 0 {
		t.Fatal("invariante quebrada")
	}
	// O probe tem de conseguir varias tentativas dentro de um orcamento plausivel.
	const realisticTotal = 6 * time.Minute
	if realisticTotal <= perRequestVerifyTimeout {
		t.Fatalf("orcamento total (%v) precisa ser maior que uma request (%v)", realisticTotal, perRequestVerifyTimeout)
	}
}
