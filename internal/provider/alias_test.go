package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// REGRESSAO de um bug do M12 que so a OpenRouter real revelou: `alias_target` e
// `null` em 451 dos 469 modelos e um OBJETO {name, slug} em 18. Decodificar como
// `string` derrubava a listagem INTEIRA no primeiro objeto — fail-closed no
// sentido errado: um item estranho escondia os 469, e /models imprimia erro
// enquanto todo modelo era perfeitamente legivel.
func TestAliasTargetAcceptsNullStringAndObject(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		slug string
	}{
		{"null", `null`, ""},
		{"string antiga", `"deepseek/deepseek-v4-pro"`, "deepseek/deepseek-v4-pro"},
		{"objeto", `{"name":"DeepSeek V4 Pro","slug":"deepseek/deepseek-v4-pro-0813"}`,
			"deepseek/deepseek-v4-pro-0813"},
		{"objeto parcial", `{"slug":"so/o-slug"}`, "so/o-slug"},
		{"vazio", `""`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var a AliasTarget
			if err := json.Unmarshal([]byte(c.raw), &a); err != nil {
				t.Fatalf("forma %s nao decodificou: %v", c.raw, err)
			}
			if a.Slug != c.slug {
				t.Fatalf("slug = %q, esperava %q", a.Slug, c.slug)
			}
		})
	}
}

// Um payload real da OpenRouter: um objeto no meio de 469 nao pode derrubar a
// listagem.
func TestCatalogSurvivesOneObjectShapedAlias(t *testing.T) {
	const one = `{"id":"~deepseek/deepseek-pro-latest","name":"DeepSeek Pro",
	  "context_length":128000,"supported_parameters":["tools"],
	  "pricing":{"prompt":"0.000001","completion":"0.000001"},
	  "alias_target":{"name":"DeepSeek: V4 Pro","slug":"deepseek/deepseek-v4-pro-0813"}}`
	const two = `{"id":"google/gemini-nano-banana-2.1","name":"Banana","context_length":65536,
	  "supported_parameters":["tools"],"pricing":{"prompt":"0","completion":"0"},
	  "alias_target":null}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[` + one + `,` + two + `]}`))
	}))
	defer srv.Close()

	t.Setenv("ALIAS_TEST_KEY", "x")
	c := NewOpenAICompatible(srv.URL, "m", "ALIAS_TEST_KEY", srv.Client())
	infos, err := c.ListModels(t.Context())
	if err != nil {
		t.Fatalf("um alias_target em formato de objeto derrubou a listagem: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("veio %d modelos, esperava 2", len(infos))
	}
	// O slug foi preservado, e o id com "~" (prefixo de deprecado) continua legivel.
	var found bool
	for _, i := range infos {
		if strings.Contains(i.ID, "deepseek-pro-latest") {
			found = true
			if i.AliasTarget != "deepseek/deepseek-v4-pro-0813" {
				t.Fatalf("alias perdido: %q", i.AliasTarget)
			}
		}
	}
	if !found {
		t.Fatal("o modelo com alias em objeto nao chegou na lista")
	}
}

// Um formato que NAO entendemos vira "sem redirect", nunca um erro fatal: o
// catalogo de um provider terceiro nao pode ser derrumbado por um campo novo.
func TestAliasTargetUnknownShapeDegradesQuietly(t *testing.T) {
	for _, raw := range []string{`123`, `true`, `[]`, `{"weird":1}`} {
		var a AliasTarget
		if err := json.Unmarshal([]byte(raw), &a); err != nil {
			t.Fatalf("forma desconhecida %s causou erro fatal: %v", raw, err)
		}
		if a.Slug != "" {
			t.Fatalf("forma %s produziu slug %q", raw, a.Slug)
		}
	}
}
