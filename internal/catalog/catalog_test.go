package catalog

import (
	"testing"

	"github.com/antonioghizidev/ancientcode/internal/provider"
)

func nvidiaStyle() []provider.ModelInfo {
	// Shape measured on the Inspiron: NIM returns only id/object/owned_by.
	return []provider.ModelInfo{
		{ID: "nvidia/nemotron-3.5-lightning-30b-a3b", OwnedBy: "nvidia"},
		{ID: "meta/llama-3.1-8b-instruct"},
		{ID: "nvidia/llama-3.1-nemoguard-8b-content-safety"},
		{ID: "nvidia/nv-embedqa-e5-v5"},
		{ID: "baai/bge-reranker-v2-m3"},
		{ID: "nvidia/llama-3.2-nv-vision-72b"},
		{ID: "deepseek-ai/deepseek-r1"},
	}
}

func openRouterStyle() []provider.ModelInfo {
	// Shape measured on the Inspiron: OpenRouter adds capabilities + pricing.
	return []provider.ModelInfo{
		{ID: "deepseek/deepseek-chat", Name: "DeepSeek V3",
			PricingPrompt: "0", PricingCompletion: "0",
			SupportedParameters: []string{"tools"}},
		{ID: "meta-llama/llama-3.3-70b-instruct", Name: "Llama 3.3 70B",
			PricingPrompt: "0.00000012", PricingCompletion: "0.0000003",
			InputModalities:     []string{"text"},
			SupportedParameters: []string{"temperature"}}, // no tools
		{ID: "openai/gpt-4o-mini", Name: "GPT-4o mini",
			PricingPrompt: "0.00000015", PricingCompletion: "0.0000006",
			InputModalities:     []string{"text", "image"},
			SupportedParameters: []string{"tools", "temperature"}},
		{ID: "text-embedding-3-small", Name: "Embedding 3 small",
			PricingPrompt: "0.00000002", PricingCompletion: "0",
			InputModalities: []string{"text"}},
		{ID: "old/model-v1", AliasTarget: "openai/gpt-4o-mini"},
		{ID: "stale/model:batch", AliasTarget: "openai/gpt-4o-mini"},
	}
}

func TestClassifyFamilies(t *testing.T) {
	cases := []struct {
		id   string
		want Kind
	}{
		{"meta/llama-3.1-8b-instruct", KindChat},
		{"nvidia/nemotron-3.5-lightning-30b-a3b", KindChat}, // known chat family
		{"nvidia/llama-3.1-nemoguard-8b-content-safety", KindGuard},
		{"nvidia/nv-embedqa-e5-v5", KindEmbedding},
		{"baai/bge-reranker-v2-m3", KindRerank},
		{"nvidia/llama-3.2-nv-vision-72b", KindVision},
		{"deepseek-ai/deepseek-r1", KindChat},
		{"openai/whisper-large-v3", KindAudio},
		{"stability/stable-diffusion-xl", KindImageGen},
		{"weird/opaque-thing", KindUnknown}, // no hint: stays honest
	}
	for _, c := range cases {
		if got := Classify(provider.ModelInfo{ID: c.id}); got != c.want {
			t.Errorf("Classify(%q) = %v, want %v", c.id, got, c.want)
		}
	}
}

func TestClassifyUsesMetadataForVisionOnly(t *testing.T) {
	// No name hint, but the provider says the only input modality is image.
	m := provider.ModelInfo{ID: "acme/thing", InputModalities: []string{"image"}}
	if got := Classify(m); got != KindVision {
		t.Errorf("vision-only model = %v, want vision", got)
	}
	m = provider.ModelInfo{ID: "acme/thing", InputModalities: []string{"text"}}
	if got := Classify(m); got != KindUnknown {
		t.Errorf("text-capable unknown model = %v, want unknown", got)
	}
}

// The whole point of the catalog: non-chat families are hidden by default, and
// nothing is invented for providers that disclose nothing.
// A model named after a chat family but actually an embedding/rerank must be
// classified as such: the specific hints win, otherwise the picker would offer
// an embedding model as an agent brain.
func TestClassifySpecificHintsBeatChatFamilyNames(t *testing.T) {
	cases := []struct {
		id   string
		want Kind
	}{
		{"nvidia/nv-embedqa-e5-v5", KindEmbedding},
		{"nvidia/llama-3.1-nemoguard-8b", KindGuard},
		{"baai/bge-reranker-v2-m3", KindRerank},
		{"nvidia/llama-3.2-nv-vision-72b", KindVision},
		{"meta/llama-3.1-8b-instruct", KindChat},
		{"nvidia/nemotron-3.5-lightning-30b-a3b", KindChat},
	}
	for _, c := range cases {
		if got := Classify(provider.ModelInfo{ID: c.id}); got != c.want {
			t.Errorf("Classify(%q) = %v, want %v", c.id, got, c.want)
		}
	}
}

func TestFilterHidesNonChatByDefault(t *testing.T) {
	all := Build(nvidiaStyle())
	got := Filter(all, Options{})
	for _, e := range got {
		switch e.Kind {
		case KindEmbedding, KindRerank, KindGuard, KindVision:
			t.Errorf("non-chat entry leaked into the default list: %+v", e.Info)
		}
	}
	if len(got) < 3 {
		t.Errorf("expected the chat-ish entries to remain, got %d", len(got))
	}
	// IncludeNonChat brings them back.
	if len(Filter(all, Options{IncludeNonChat: true})) != len(all) {
		t.Error("IncludeNonChat must not drop entries")
	}
}

// Security-ish: "unknown tool support" must not be silently presented as a yes.
func TestToolSupportIsTriState(t *testing.T) {
	all := Build(nvidiaStyle())
	for _, e := range all {
		if e.Tools != ToolsUnknown {
			t.Errorf("NIM discloses no capabilities; %q got %v", e.Info.ID, e.Tools)
		}
	}
	all = Build(openRouterStyle())
	want := map[string]ToolSupport{
		"deepseek/deepseek-chat":            ToolsYes,
		"meta-llama/llama-3.3-70b-instruct": ToolsNo,
		"openai/gpt-4o-mini":                ToolsYes,
	}
	for id, w := range want {
		e := all[Index(all, id)]
		if e.Tools != w {
			t.Errorf("%s: tools = %v, want %v", id, e.Tools, w)
		}
	}
}

func TestFilterRequireToolsKeepsUnknown(t *testing.T) {
	all := Build(openRouterStyle())
	got := Filter(all, Options{RequireTools: true})
	for _, e := range got {
		if e.Tools == ToolsNo {
			t.Errorf("RequireTools kept a model declared without tools: %+v", e.Info)
		}
	}
	if len(got) == 0 {
		t.Error("RequireTools must not drop unknown ones (NIM would be empty)")
	}
}

func TestFilterQueryMatchesIDNameAndFamily(t *testing.T) {
	all := Build(nvidiaStyle())
	if n := len(Filter(all, Options{Query: "llama"})); n != 1 {
		t.Errorf("query 'llama' matched %d, want 1", n)
	}
	if n := len(Filter(all, Options{Query: "LLAMA"})); n != 1 {
		t.Errorf("query must be case-insensitive, matched %d", n)
	}
	if n := len(Filter(all, Options{Query: "nemotron"})); n != 1 {
		t.Errorf("query 'nemotron' matched %d", n)
	}
	// A family search reveals that family even though it is hidden by default.
	if n := len(Filter(all, Options{Query: "guard"})); n == 0 {
		t.Error("searching for a family must find it")
	}
	if n := len(Filter(all, Options{Query: "zzz"})); n != 0 {
		t.Errorf("no match expected, got %d", n)
	}
}

// The "free" filter must only be offered when the provider prices models at all.
func TestFreeFilterOnlyWherePriced(t *testing.T) {
	nim := Build(nvidiaStyle())
	if FreeFilterAvailable(nim) {
		t.Error("NIM has no pricing: free filter must be reported unavailable")
	}
	if n := len(Filter(nim, Options{FreeOnly: true})); n != 0 {
		t.Errorf("free filter on an unpriced catalog must yield 0, got %d", n)
	}
	or := Build(openRouterStyle())
	if !FreeFilterAvailable(or) {
		t.Error("OpenRouter prices models: free filter must be available")
	}
	got := Filter(or, Options{FreeOnly: true})
	if len(got) != 1 || got[0].Info.ID != "deepseek/deepseek-chat" {
		t.Errorf("free filter = %+v", got)
	}
}

func TestBuildDropsDuplicatesAndAliases(t *testing.T) {
	all := Build(openRouterStyle())
	for _, e := range all {
		if e.Info.AliasTarget != "" {
			t.Errorf("alias entry kept: %+v", e.Info)
		}
	}
	dup := Build([]provider.ModelInfo{{ID: "a/m"}, {ID: "a/m"}})
	if len(dup) != 1 {
		t.Errorf("duplicates kept: %+v", dup)
	}
	if n := len(Build([]provider.ModelInfo{{ID: "  "}})); n != 0 {
		t.Error("blank id must be dropped")
	}
}

func TestRankPutsActiveFirstThenVerifiedThenAlpha(t *testing.T) {
	all := Build([]provider.ModelInfo{
		{ID: "z/alpha"}, {ID: "a/beta"}, {ID: "m/active"},
	})
	all[1].Check = CheckOK // a/beta is verified
	got := Rank(all, "m/active")
	if got[0].Info.ID != "m/active" {
		t.Errorf("active must come first, got %s", got[0].Info.ID)
	}
	if got[1].Info.ID != "a/beta" {
		t.Errorf("verified must come before unverified, got %s", got[1].Info.ID)
	}
	// Rank must not mutate its input.
	if all[0].Info.ID != "z/alpha" {
		t.Error("Rank mutated the input slice")
	}
}

func TestFilterRequireVerified(t *testing.T) {
	all := Build([]provider.ModelInfo{{ID: "a/x"}, {ID: "b/y"}})
	all[1].Check = CheckOK
	got := Filter(all, Options{RequireVerified: true})
	if len(got) != 1 || got[0].Info.ID != "b/y" {
		t.Errorf("RequireVerified = %+v", got)
	}
}

func TestSummarize(t *testing.T) {
	all := Build(openRouterStyle())
	vis := Filter(all, Options{})
	c := Summarize(all, vis)
	if c.Total != len(all) || c.Visible != len(vis) {
		t.Errorf("counters = %+v", c)
	}
	if c.Priced != len(all) {
		t.Errorf("priced = %d, want %d", c.Priced, len(all))
	}
	if c.NonChat == 0 {
		t.Error("non-chat should be counted (the embedding entry)")
	}
	if c.Unverified != c.Total {
		t.Errorf("unverified = %d, total = %d", c.Unverified, c.Total)
	}
}

func TestIndexFindsAndMisses(t *testing.T) {
	all := Build(nvidiaStyle())
	if i := Index(all, "meta/llama-3.1-8b-instruct"); i < 0 {
		t.Error("existing model not found")
	}
	if i := Index(all, "nope/nope"); i != -1 {
		t.Errorf("missing model must be -1, got %d", i)
	}
}
