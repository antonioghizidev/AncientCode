package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func newModelsTestClient(t *testing.T, h http.HandlerFunc) (*Client, func()) {
	t.Helper()
	srv := httptest.NewServer(h)
	c := NewOpenAICompatible(srv.URL+"/v1", "m", "TEST_KEY_ENV", srv.Client())
	return c, srv.Close
}

func withKey(t *testing.T) {
	t.Helper()
	t.Setenv("TEST_KEY_ENV", "nvapi-abcdef0123456789abcdef")
}

func TestListModelsParsesMinimalAndRichEntries(t *testing.T) {
	withKey(t)
	body := `{"data":[
		{"id":"meta/llama-3.1-8b-instruct","object":"model","owned_by":"meta"},
		{"id":"openai/gpt-4o","name":"GPT-4o","context_length":128000,
		 "pricing":{"prompt":"0.0000025","completion":"0.00001"},
		 "architecture":{"input_modalities":["text","image"]},
		 "supported_parameters":["tools","temperature"]}
	]}`
	c, done := newModelsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q, want /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
			t.Errorf("missing bearer auth: %q", got)
		}
		fmt.Fprint(w, body)
	})
	defer done()

	got, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	// sorted by id: meta/... before openai/...
	if got[0].ID != "meta/llama-3.1-8b-instruct" || got[0].OwnedBy != "meta" {
		t.Errorf("minimal entry = %+v", got[0])
	}
	if got[0].PriceKnown() {
		t.Error("NIM-style entry has no pricing: PriceKnown must be false")
	}
	if got[0].Free() {
		t.Error("unknown price must NOT be reported as free")
	}
	rich := got[1]
	if rich.Name != "GPT-4o" || rich.ContextLength != 128000 {
		t.Errorf("rich entry = %+v", rich)
	}
	if !rich.PriceKnown() || rich.Free() {
		t.Errorf("priced entry: known=%v free=%v", rich.PriceKnown(), rich.Free())
	}
	if !rich.SupportsTextInput() {
		t.Error("text modality must be detected")
	}
}

// Security: a free model is only free when the provider said zero.
func TestFreeOnlyWhenPriceIsExplicitZero(t *testing.T) {
	m := ModelInfo{PricingPrompt: "0", PricingCompletion: "0"}
	if !m.Free() || !m.PriceKnown() {
		t.Errorf("zero price must be free: %+v", m)
	}
	if (ModelInfo{PricingPrompt: "0"}).Free() {
		t.Error("prompt-only zero must not claim free (completion unknown)")
	}
	if (ModelInfo{}).Free() || (ModelInfo{}).PriceKnown() {
		t.Error("absent pricing must be unknown, not free")
	}
}

func TestListModelsDeduplicatesAndDropsEmptyIDs(t *testing.T) {
	withKey(t)
	c, done := newModelsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"a/m"},{"id":"a/m"},{"id":""},{"id":"b/n"}]}`)
	})
	defer done()
	got, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (dedup + drop empty): %+v", len(got), got)
	}
}

// Security: the response body is read under a hard limit; a hostile/broken
// endpoint must not be able to exhaust memory.
func TestListModelsBoundsResponseSize(t *testing.T) {
	withKey(t)
	c, done := newModelsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		// Valid prefix, then far more than the limit: the JSON cannot be
		// decoded, and we must fail closed instead of returning a partial list.
		fmt.Fprint(w, `{"data":[{"id":"a/m"}]}`)
		blob := strings.Repeat(`{"id":"x/y","pad":"`+strings.Repeat("z", 512)+`"},`, 20000)
		fmt.Fprint(w, `{"pad":"`+blob+`"}`)
	})
	defer done()
	got, err := c.ListModels(context.Background())
	if err == nil {
		t.Fatalf("oversized body must fail closed, got %d entries", len(got))
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error should explain the truncated/undecodable body: %v", err)
	}
}

// Security: provider-controlled strings must never carry terminal control
// sequences (an id is data, not a terminal instruction).
func TestListModelsSanitizesProviderStrings(t *testing.T) {
	withKey(t)
	c, done := newModelsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"evil/\u001b[2Jmodel\u0007","name":"\u001b[31mred"}]}`)
	})
	defer done()
	got, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	for _, s := range []string{got[0].ID, got[0].Name} {
		for _, r := range s {
			if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
				t.Errorf("control rune %U survived in %q", r, s)
			}
		}
	}
	if !strings.Contains(got[0].ID, "model") {
		t.Errorf("readable part of the id must survive: %q", got[0].ID)
	}
}

// Security: a secret echoed back by the provider must not survive in the catalog.
func TestListModelsRedactsSecretInIDs(t *testing.T) {
	withKey(t)
	c, done := newModelsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"leak/sk-abcdefGHIJKLMNOPQRSTUVWXyz123456"}]}`)
	})
	defer done()
	got, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got[0].ID, "abcdefGHIJKLMNOPQ") {
		t.Errorf("catalog id leaked a secret: %q", got[0].ID)
	}
}

// Security: the API key must never appear in any error path.
func TestListModelsNeverLeaksKeyInErrors(t *testing.T) {
	const secret = "nvapi-abcdef0123456789abcdef"
	cases := []struct {
		name    string
		env     string
		handler http.HandlerFunc
	}{
		{"missing env", "", func(w http.ResponseWriter, r *http.Request) {}},
		{"401", secret, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, "unauthorized for token "+secret)
		}},
		{"invalid json", secret, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "not json "+secret)
		}},
		{"empty data", secret, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"data":[]}`)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env == "" {
				os.Unsetenv("TEST_KEY_ENV")
			} else {
				t.Setenv("TEST_KEY_ENV", secret)
			}
			c, done := newModelsTestClient(t, tc.handler)
			defer done()
			_, err := c.ListModels(context.Background())
			if err == nil {
				t.Fatal("expected an error")
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("key leaked in error: %v", err)
			}
		})
	}
}

func TestListModelsFailsClosedWithoutKey(t *testing.T) {
	os.Unsetenv("TEST_KEY_ENV")
	c, done := newModelsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("request must not be made without a key")
	})
	defer done()
	if _, err := c.ListModels(context.Background()); err == nil {
		t.Fatal("missing key must fail closed")
	}
}

// Security: the caller's context must actually cancel the request.
func TestListModelsHonoursContext(t *testing.T) {
	withKey(t)
	release := make(chan struct{})
	c, done := newModelsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
	})
	defer func() { close(release); done() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.ListModels(ctx); err == nil {
		t.Fatal("cancelled context must fail the call")
	}
}

func TestSanitizeCatalogCollapsesWhitespace(t *testing.T) {
	if got := sanitizeCatalog("a  b\tc\nd"); got != "a b c d" {
		t.Errorf("got %q", got)
	}
	if got := sanitizeCatalog("\x1b[2Jrm -rf /"); strings.ContainsRune(got, 0x1b) {
		t.Errorf("ESC survived: %q", got)
	}
}
