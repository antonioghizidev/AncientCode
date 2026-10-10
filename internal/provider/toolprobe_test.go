package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// probeServer is a canned /chat/completions endpoint that records the request
// body, so a test can assert what the probe actually sent.
type probeServer struct {
	body   string
	status int
	block  bool
	seen   *string
	srv    *httptest.Server
}

func startProbe(t *testing.T, body string, status int) (*Client, *probeServer) {
	t.Helper()
	ps := &probeServer{body: body, status: status, seen: new(string)}
	ps.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		*ps.seen = string(raw)
		if ps.block {
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(ps.status)
		_, _ = w.Write([]byte(ps.body))
	}))
	t.Cleanup(ps.srv.Close)
	return newTestClient(t, ps.srv.URL), ps
}

// A model that returns an actual tool_calls block has proven support.
func TestToolProbeDetectsRealToolCall(t *testing.T) {
	const reply = `{"choices":[{"message":{"role":"assistant","tool_calls":[` +
		`{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{}"}}]},` +
		`"finish_reason":"tool_calls"}]}`
	c, ps := startProbe(t, reply, 200)

	res, err := c.ToolProbe(context.Background(), "m1")
	if err != nil {
		t.Fatalf("sonda: %v", err)
	}
	if !res.SupportsTools {
		t.Fatal("respondeu com tool_calls e veio SupportsTools=false")
	}
	if res.Model != "m1" {
		t.Errorf("Model = %q, esperava m1", res.Model)
	}
	// Without tool_choice the model may answer in prose and the probe proves
	// nothing at all.
	if !strings.Contains(*ps.seen, "tool_choice") {
		t.Errorf("a sonda nao forcou tool_choice; pedido = %s", *ps.seen)
	}
}

// The whole point: answering in prose is not evidence of tool-calling support.
func TestToolProbeProseIsNotToolSupport(t *testing.T) {
	c, _ := startProbe(t, `{"choices":[{"message":{"role":"assistant","content":"Sure, I can read files."}}]}`, 200)

	res, err := c.ToolProbe(context.Background(), "m1")
	if err != nil {
		t.Fatalf("sonda: %v", err)
	}
	if res.SupportsTools {
		t.Fatal("respondeu em prosa e veio SupportsTools=true")
	}
}

func TestToolProbeHTTPErrorFails(t *testing.T) {
	c, _ := startProbe(t, `{"error":"no such model"}`, 404)

	res, err := c.ToolProbe(context.Background(), "ghost")
	if err == nil {
		t.Fatal("404 deveria virar erro")
	}
	if res.SupportsTools {
		t.Fatal("falhou e veio SupportsTools=true")
	}
}

// A provider that echoes the key back must not reach the operator's terminal.
func TestToolProbeErrorIsRedacted(t *testing.T) {
	c, _ := startProbe(t, `{"error":"invalid key `+testKey+`"}`, 401)

	_, err := c.ToolProbe(context.Background(), "m1")
	if err == nil {
		t.Fatal("401 deveria virar erro")
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatalf("vazou segredo no erro: %v", err)
	}
}

// A hung provider must not hang the REPL.
func TestToolProbeHonoursContextCancel(t *testing.T) {
	c, _ := startProbe(t, "", 200)
	c.mu.Lock()
	c.http = &http.Client{Timeout: 0}
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := c.ToolProbe(ctx, "m1"); err == nil {
		t.Fatal("provider pendurado deveria dar erro por timeout")
	}
}

// Bounded read: an endless body must be cut, not fatal — the probe only needs
// the first choice's tool_calls.
func TestToolProbeBoundsResponseRead(t *testing.T) {
	huge := `{"choices":[{"message":{"content":"` + strings.Repeat("A", 4<<20) + `"}}]}`
	c, _ := startProbe(t, huge, 200)

	res, err := c.ToolProbe(context.Background(), "m1")
	if err != nil {
		t.Fatalf("corpo grande deve ser cortado sem virar erro: %v", err)
	}
	if !res.Truncated {
		t.Fatal("corpo acima do teto deveria marcar Truncated")
	}
	if res.SupportsTools {
		t.Fatal("conteudo gigante sem tool_calls nao e suporte")
	}
}

// Truncated is not the same as "unsupported". Reporting a cut-off response as a
// clean negative would turn "we ran out of read budget" into a verdict about
// the model — a lie the operator cannot distinguish from a real answer.
func TestToolProbeMalformedJSONIsAnErrorNotTruncation(t *testing.T) {
	c, _ := startProbe(t, `{"choices":[`, 200)
	res, err := c.ToolProbe(context.Background(), "m1")
	if err == nil {
		t.Fatal("JSON malformado deveria ser erro")
	}
	if res.Truncated {
		t.Fatal("JSON malformado nao e truncamento")
	}
}

func TestToolProbeEmptyChoicesIsNotToolSupport(t *testing.T) {
	c, _ := startProbe(t, `{"choices":[]}`, 200)
	res, err := c.ToolProbe(context.Background(), "m1")
	if err != nil {
		t.Fatalf("sonda: %v", err)
	}
	if res.SupportsTools {
		t.Fatal("sem choices nao ha tool call")
	}
}

// The probe must never touch the session's active model: it is a side query.
func TestToolProbeDoesNotChangeActiveModel(t *testing.T) {
	c, _ := startProbe(t, `{"choices":[]}`, 200)

	if _, err := c.ToolProbe(context.Background(), "outro/model"); err != nil {
		t.Fatalf("sonda: %v", err)
	}
	if c.Model() != "test/model" {
		t.Fatalf("a sonda trocou o modelo da sessao para %q", c.Model())
	}
}

// ...AND it must probe the model it was ASKED about, not the session's.
//
// Without this, --check would probe the active model 40 times and report the
// same verdict 40 times: the page would "verify" while saying nothing about the
// 39 models the operator is actually looking at.
func TestToolProbeTestsTheRequestedModelNotTheSessionOne(t *testing.T) {
	c, ps := startProbe(t, `{"choices":[]}`, 200)

	if _, err := c.ToolProbe(context.Background(), "alvo/pedido"); err != nil {
		t.Fatalf("sonda: %v", err)
	}
	if !strings.Contains(*ps.seen, "alvo/pedido") {
		t.Errorf("a sonda pediu o modelo da sessao, nao o informado: %s", *ps.seen)
	}
	if strings.Contains(*ps.seen, "test/model") {
		t.Errorf("a sonda usou o modelo ativo da sessao: %s", *ps.seen)
	}
}

// A hung provider must not hang --check. This asserts the ceiling is really
// WIRED IN, not merely that the helper produces the right number: a test that
// only checked boundedClient(...) stayed green when the probe stopped calling it.
func TestToolProbeAppliesItsCeilingAgainstAHungServer(t *testing.T) {
	// The handler is released by an explicit channel rather than by the request
	// context: httptest.Server.Close waits for outstanding handlers, and
	// relying on the client-side timeout to also release the server side makes
	// the test's teardown depend on the very thing under test.
	release := make(chan struct{})
	ps := &probeServer{block: true, status: 200, seen: new(string)}
	ps.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*ps.seen = "pediu"
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer func() {
		close(release)
		ps.srv.Close()
	}()

	c := newTestClient(t, ps.srv.URL)
	c.mu.Lock()
	// No client timeout and no context deadline: the ONLY thing that can end this
	// request is the probe's own ceiling.
	c.http = &http.Client{}
	c.probeTimeout = 200 * time.Millisecond
	c.mu.Unlock()

	ctx := context.Background()
	start := time.Now()
	if _, err := c.ToolProbe(ctx, "m1"); err == nil {
		t.Fatal("servidor pendurado deveria dar erro por timeout")
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("a sonda demorou %v: o teto por requisicao nao foi aplicado", el)
	}
}

// The per-request ceiling is the only thing between one hung provider and 40
// sequential probes hanging the REPL.
func TestToolProbeHasItsOwnPerRequestCeiling(t *testing.T) {
	if toolProbeTimeout <= 0 {
		t.Fatal("toolProbeTimeout precisa ser positivo")
	}
	// It must be tighter than the default 5-minute client, or it never applies:
	// boundedClient only LOWERS an already-looser timeout.
	five := &http.Client{Timeout: 5 * time.Minute}
	if got := boundedClient(five, toolProbeTimeout); got.Timeout != toolProbeTimeout {
		t.Fatalf("cliente de 5min nao foi apertado para %v: %v", toolProbeTimeout, got.Timeout)
	}
	if five.Timeout != 5*time.Minute {
		t.Fatalf("a sonda alterou o cliente do chamador: %v", five.Timeout)
	}
	// A client with NO timeout is the dangerous case: it inherits nothing.
	if got := boundedClient(&http.Client{}, toolProbeTimeout); got.Timeout != toolProbeTimeout {
		t.Fatalf("cliente sem timeout nao foi limitado: %v", got.Timeout)
	}
	got := boundedClient(nil, toolProbeTimeout)
	if got == nil || got.Timeout != toolProbeTimeout {
		t.Fatalf("nil nao virou cliente utilizavel: %v", got)
	}
}
