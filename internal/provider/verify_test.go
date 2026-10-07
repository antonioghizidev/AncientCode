package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const verifyTestKey = "nvapi-SUPERSECRETVALUE123"

func modelsServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+verifyTestKey {
			// O servidor de teste so aceita a chave conhecida: assim um teste
			// que mande a chave errada falha pelo motivo certo.
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const catalogBody = `{"data":[{"id":"nvidia/nemotron-3.5"},{"id":"openai/gpt-oss-120b"}]}`

func TestVerifyCredentialAcceptsLiveKey(t *testing.T) {
	srv := modelsServer(t, http.StatusOK, catalogBody)
	got, err := VerifyCredential(context.Background(), srv.Client(), srv.URL, verifyTestKey)
	if err != nil {
		t.Fatalf("VerifyCredential: %v", err)
	}
	if got.ModelCount != 2 {
		t.Fatalf("ModelCount = %d, quero 2", got.ModelCount)
	}
}

// A propriedade central do /connect: chave recusada NAO e guardada. Aqui so
// garantimos que o erro distingue "recusada" de "problema de rede", que e o que
// permite ao /connect dizer "digitou errado" em vez de "algo deu errado".
func TestVerifyCredentialReportsUnauthorizedDistinctly(t *testing.T) {
	srv := modelsServer(t, http.StatusOK, catalogBody)
	_, err := VerifyCredential(context.Background(), srv.Client(), srv.URL, "chave-errada")
	if err == nil {
		t.Fatal("chave recusada deveria dar erro")
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("errors.Is(err, ErrUnauthorized) = false: %v", err)
	}
	if strings.Contains(err.Error(), "chave-errada") {
		t.Fatalf("erro carrega a chave recusada: %v", err)
	}
}

// O corpo de um 401 costuma ecoar a chave submetida. Ela nao pode chegar ao
// operador nem a um log.
func TestVerifyUnauthorizedBodyNeverLeaksKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid key ` + verifyTestKey + `"}`))
	}))
	defer srv.Close()
	_, err := VerifyCredential(context.Background(), srv.Client(), srv.URL, verifyTestKey)
	if err == nil {
		t.Fatal("esperava erro")
	}
	if strings.Contains(err.Error(), verifyTestKey) {
		t.Fatalf("corpo do 401 vazou a chave: %v", err)
	}
}

func TestVerifyNeverLeaksKeyInAnyError(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		baseURL string
		key     string
	}{
		{"http 500 com a chave no corpo", http.StatusInternalServerError, `{"error":"` + verifyTestKey + `"}`, "", verifyTestKey},
		{"json invalido", http.StatusOK, `{nao é json`, "", verifyTestKey},
		{"catalogo vazio", http.StatusOK, `{"data":[]}`, "", verifyTestKey},
		{"entradas sem id", http.StatusOK, `{"data":[{"name":"sem id"}]}`, "", verifyTestKey},
		{"base_url http nao-loopback", http.StatusOK, catalogBody, "http://exemplo.com", verifyTestKey},
		{"esquema invalido", http.StatusOK, catalogBody, "ftp://exemplo.com", verifyTestKey},
		{"chave vazia", http.StatusOK, catalogBody, "", "  "},
	}
	for _, c := range cases {
		base := c.baseURL
		if base == "" {
			s := modelsServer(t, c.status, c.body)
			base = s.URL
		}
		_, err := VerifyCredential(context.Background(), http.DefaultClient, base, c.key)
		if err == nil {
			t.Errorf("%s: esperava erro", c.name)
			continue
		}
		if strings.Contains(err.Error(), verifyTestKey) {
			t.Errorf("%s: erro carrega a chave: %v", c.name, err)
		}
	}
}

// http so e aceito em loopback: um base_url cleartext nao-loopback durante a
// VERIFICACAO mandaria a chave candidata em texto puro — e essa chave nunca foi
// avaliada por ninguem antes.
func TestVerifyRefusesInsecureBaseURL(t *testing.T) {
	_, err := VerifyCredential(context.Background(), http.DefaultClient, "http://exemplo.example/v1", verifyTestKey)
	if err == nil {
		t.Fatal("http nao-loopback deveria ser recusado")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("erro deveria explicar a regra do loopback: %v", err)
	}
	// Loopback real continua aceito (LiteLLM local).
	for _, ok := range []string{"http://127.0.0.1:4000/v1", "http://localhost:4000/v1", "https://api.exemplo/v1"} {
		if err := checkCredentialBaseURL(ok); err != nil {
			t.Errorf("checkCredentialBaseURL(%q) = %v, deveria passar", ok, err)
		}
	}
}

// O corpo lido e limitado: um provider que responde infinitamente nao pode
// esgotar a memoria do Inspiron.
func TestVerifyBoundsResponseSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"ok/one"}],"pad":"`))
		chunk := strings.Repeat("A", 32<<10)
		for i := 0; i < 64; i++ {
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	// Nao deve travar nem estourar memoria; pode falhar ao decodificar.
	if _, err := VerifyCredential(context.Background(), srv.Client(), srv.URL, verifyTestKey); err != nil {
		if strings.Contains(err.Error(), verifyTestKey) {
			t.Fatalf("erro carrega a chave: %v", err)
		}
	}
}

// O contexto cancelado tem de vencer: sem deadline, um provider lento trava o
// onboarding.
func TestVerifyRespectsContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := VerifyCredential(ctx, srv.Client(), srv.URL, verifyTestKey); err == nil {
		t.Fatal("contexto cancelado deveria dar erro")
	}
}
