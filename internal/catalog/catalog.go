// Package catalog decides which models from a provider catalog are usable by
// this harness, and filters/ranks them for a picker (M12, D-2026-10-01-53).
//
// It is pure stdlib and side-effect free: same input, same output, no network, no
// clock. The REPL and the TUI both render *these* results, so the notion of
// "works here" has exactly one definition (the M8 parity rule).
//
// The central judgement is deliberately conservative. What this harness needs is
// a model that emits `tool_calls`; the agent is useless without it. Providers
// expose wildly different metadata — NVIDIA NIM returns only id/owner, OpenRouter
// returns capabilities and pricing — so a missing signal is reported as
// *unknown*, never as "yes". The UI can then say "unverified" instead of quietly
// offering a model that will not do tool calls.
package catalog

import (
	"sort"
	"strings"

	"ancientai/internal/provider"
)

// Kind is the coarse family a model belongs to, derived from its name (always
// available) refined by provider metadata (only when the provider exposes it).
type Kind int

const (
	// KindUnknown is the honest default: nothing in the name or the metadata
	// identified the model. Treated as chat-shaped but flagged unverified.
	KindUnknown Kind = iota
	KindChat
	KindEmbedding
	KindRerank
	KindGuard
	KindVision
	KindAudio
	KindModeration
	KindImageGen
)

var kindLabels = map[Kind]string{
	KindUnknown:    "?",
	KindChat:       "chat",
	KindEmbedding:  "embedding",
	KindRerank:     "rerank",
	KindGuard:      "guard",
	KindVision:     "vision",
	KindAudio:      "audio",
	KindModeration: "moderation",
	KindImageGen:   "image",
}

func (k Kind) String() string {
	if s, ok := kindLabels[k]; ok {
		return s
	}
	return "?"
}

// nonChatKinds are the families this harness cannot drive as an agent: they do
// not answer /chat/completions with tool calls.
func (k Kind) nonChat() bool {
	switch k {
	case KindEmbedding, KindRerank, KindGuard, KindVision, KindAudio, KindModeration, KindImageGen:
		return true
	}
	return false
}

// Check is the result of an explicit verification (layer 3, `--check`).
type Check int

const (
	// CheckUnverified is the default: nobody spent a request proving anything.
	CheckUnverified Check = iota
	// CheckOK means a real tool-calling request succeeded.
	CheckOK
	// CheckFailed means the model answered but could not produce tool calls.
	CheckFailed
	// CheckError means the probe itself failed (network, auth, quota).
	CheckError
)

// Entry is one catalog row as this harness sees it.
type Entry struct {
	Info provider.ModelInfo
	Kind Kind
	// Tools is tri-state on purpose: false means "the provider said no" (or the
	// name says it cannot), true means "verified by a real request", and
	// Unverified means nobody knows. Treating unknown as false would hide every
	// NIM model; treating it as true would promise something we never checked.
	Tools ToolSupport
	Check Check
}

// ToolSupport is the tri-state tool-calling capability.
type ToolSupport int

const (
	// ToolsUnknown: the provider did not disclose it (e.g. NIM) and no
	// verification has been run.
	ToolsUnknown ToolSupport = iota
	// ToolsYes: verified by a real tool-calling request.
	ToolsYes
	// ToolsNo: the provider explicitly does not list tool support.
	ToolsNo
)

func (t ToolSupport) String() string {
	switch t {
	case ToolsYes:
		return "tools"
	case ToolsNo:
		return "sem tools"
	default:
		return "tools?"
	}
}

// nameHints maps substrings to a family. Order matters only for readability —
// every hint is checked, and the first match wins in the listed order.
var nameHints = []struct {
	needle string
	kind   Kind
}{
	// Order: the more specific families first, so "guard" does not read as chat.
	{"nemoguard", KindGuard},
	{"guard", KindGuard},
	{"safety", KindGuard},
	{"embed", KindEmbedding},
	{"embedding", KindEmbedding},
	{"e5-", KindEmbedding},
	{"rerank", KindRerank},
	{"nv-rerank", KindRerank},
	{"moderation", KindModeration},
	{"whisper", KindAudio},
	{"tts", KindAudio},
	{"parakeet", KindAudio},
	{"speech", KindAudio},
	{"audio", KindAudio},
	{"image-gen", KindImageGen},
	{"imagen", KindImageGen},
	{"stable-diffusion", KindImageGen},
	{"vision", KindVision},
	// Chat families. These come last on purpose: the specific non-chat hints
	// above win first (a model called "llama-…-embed" is an embedding, not
	// chat). A model matching none of these stays unknown — better an honest
	// "?" than a guess that puts a reranker in front of the operator.
	{"nemotron", KindChat},
	{"llama", KindChat},
	{"gemma", KindChat},
	{"qwen", KindChat},
	{"phi-", KindChat},
	{"mistral", KindChat},
	{"mixtral", KindChat},
	{"deepseek", KindChat},
	{"gpt-", KindChat},
	{"gpt", KindChat},
	{"claude", KindChat},
	{"sonnet", KindChat},
	{"opus", KindChat},
	{"haiku", KindChat},
	{"gemini", KindChat},
	{"command-r", KindChat},
	{"falcon", KindChat},
	{"hermes", KindChat},
	{"dbrx", KindChat},
	{"jamba", KindChat},
	{"yi-", KindChat},
	{"exaone", KindChat},
	{"devstral", KindChat},
	{"magistral", KindChat},
	{"smollm", KindChat},
	{"nomic-embed", KindEmbedding},
	{"vllm-", KindChat},
	{"instruct", KindChat},
	{"chat", KindChat},
	{"coder", KindChat},
	{"code", KindChat},
	{"reasoning", KindChat},
	{"think", KindChat},
}

// Classify derives the family from the model name, refined by provider
// metadata when present. An unknown name stays unknown rather than being forced
// into "chat".
func Classify(m provider.ModelInfo) Kind {
	id := strings.ToLower(m.ID + " " + m.Name)
	for _, h := range nameHints {
		if strings.Contains(id, h.needle) {
			return h.kind
		}
	}
	// OpenRouter tells us the input modalities; a model that only takes images
	// is not an agent brain for us.
	if len(m.InputModalities) > 0 && !m.SupportsTextInput() {
		return KindVision
	}
	return KindUnknown
}

// toolSupport reads the provider's declared capabilities. Absent metadata stays
// unknown.
func toolSupport(m provider.ModelInfo) ToolSupport {
	if len(m.SupportedParameters) == 0 {
		return ToolsUnknown
	}
	for _, p := range m.SupportedParameters {
		if strings.EqualFold(strings.TrimSpace(p), "tools") ||
			strings.EqualFold(strings.TrimSpace(p), "tool_choice") {
			return ToolsYes
		}
	}
	return ToolsNo
}

// Build turns raw provider entries into catalog entries, dropping duplicates
// and deprecated aliases (OpenRouter's alias_target points at the replacement).
func Build(models []provider.ModelInfo) []Entry {
	out := make([]Entry, 0, len(models))
	seen := make(map[string]bool, len(models))
	byID := make(map[string]int, len(models))
	for _, m := range models {
		if strings.TrimSpace(m.ID) == "" || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		byID[m.ID] = len(out)
		out = append(out, Entry{Info: m, Kind: Classify(m), Tools: toolSupport(m)})
	}
	// An alias whose target is also present is noise: drop it.
	filtered := out[:0]
	for _, e := range out {
		if e.Info.AliasTarget != "" && seen[e.Info.AliasTarget] {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

// Options are the picker filters. All zero values mean "no extra filtering".
type Options struct {
	// Query is a case-insensitive substring matched against id and name.
	Query string
	// FreeOnly keeps only models the provider priced at zero. It is a no-op
	// filter when the catalog has no pricing at all (NIM) — the caller reports
	// that instead of pretending the list is empty on purpose.
	FreeOnly bool
	// IncludeNonChat keeps embedding/guard/vision/... families, which are
	// hidden by default because this harness cannot drive them.
	IncludeNonChat bool
	// RequireVerified drops entries with Check != CheckOK.
	RequireVerified bool
	// RequireTools drops entries known NOT to support tool calls.
	RequireTools bool
}

// Filter applies Options. Matching is stable and the order is preserved (call
// Rank first if you want a specific order).
//
// One deliberate rule: typing a family name (e.g. "embed") reveals that family
// even though it is hidden by default. The default hides non-chat models because
// this harness cannot drive them, but an explicit search is a question, not a
// policy violation — and the entry still shows its own family label, so nobody is
// misled about what they picked.
func Filter(entries []Entry, opt Options) []Entry {
	q := strings.ToLower(strings.TrimSpace(opt.Query))
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		familyHit := q != "" && strings.Contains(e.Kind.String(), q)
		if !familyHit {
			if !opt.IncludeNonChat && e.Kind.nonChat() {
				continue
			}
			if opt.RequireTools && e.Tools == ToolsNo {
				continue
			}
			if opt.RequireVerified && e.Check != CheckOK {
				continue
			}
			if opt.FreeOnly && !e.Info.Free() {
				continue
			}
			if q != "" && !matches(e.Info, q) {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

// matches reports whether q appears in the id or the display name.
func matches(m provider.ModelInfo, q string) bool {
	return strings.Contains(strings.ToLower(m.ID), q) ||
		strings.Contains(strings.ToLower(m.Name), q)
}

// Rank orders entries for display: the active model first, then verified, then
// alphabetical by id. Sorting by id alone (the provider order) is already stable
// from Build, so this is the last step of a pipeline.
func Rank(entries []Entry, active string) []Entry {
	out := make([]Entry, len(entries))
	copy(out, entries)
	sort.SliceStable(out, func(i, j int) bool {
		ai := out[i].Info.ID == active
		aj := out[j].Info.ID == active
		if ai != aj {
			return ai // active model always on top
		}
		if out[i].Check != out[j].Check {
			return out[i].Check == CheckOK // verified first
		}
		return out[i].Info.ID < out[j].Info.ID
	})
	return out
}

// FreeFilterAvailable reports whether a "free only" filter can mean anything
// for this catalog. NVIDIA NIM publishes no pricing, so the answer is false and
// the UI must say so rather than showing an empty list.
func FreeFilterAvailable(entries []Entry) bool {
	for _, e := range entries {
		if e.Info.PriceKnown() {
			return true
		}
	}
	return false
}

// Index returns the position of id in entries, or -1. The REPL uses it to turn
// "/model 3" into a model id without letting the number escape the last listing.
func Index(entries []Entry, id string) int {
	for i, e := range entries {
		if e.Info.ID == id {
			return i
		}
	}
	return -1
}

// Count summarizes a catalog for the picker footer.
type Count struct {
	Total      int
	Visible    int
	Verified   int
	Free       int
	Priced     int
	NonChat    int
	Unverified int
}

// Summarize computes the counters shown next to the list.
func Summarize(all, visible []Entry) Count {
	c := Count{Total: len(all), Visible: len(visible)}
	for _, e := range all {
		if e.Check == CheckOK {
			c.Verified++
		} else {
			c.Unverified++
		}
		if e.Info.Free() {
			c.Free++
		}
		if e.Info.PriceKnown() {
			c.Priced++
		}
		if e.Kind.nonChat() {
			c.NonChat++
		}
	}
	return c
}
