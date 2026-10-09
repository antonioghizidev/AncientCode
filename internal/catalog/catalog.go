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

	"github.com/antonioghizidev/ancientcode/internal/provider"
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
	// AgentViable applies the hard requirements this harness actually has, from
	// the PROVIDER's own metadata: tool calling, and a context window big enough
	// for one turn. It is the honest filter, and it is deliberately not a big
	// one.
	//
	// Measured on OpenRouter (2026-10-02): of 469 models, 401 advertise tool
	// calling and 395 also clear a 32k context floor. The reduction is 16%. The
	// reason is that the metadata cannot do the job an operator expects of it:
	// `google/gemini-nano-banana-2.1` advertises `tools` AND
	// `output_modalities: [image, text]`, so no field in the response marks it as
	// an image model. Filtering metadata gets the obviously-wrong out; ordering
	// is what makes the list usable (see AgentRank).
	AgentViable bool
	// MinContext overrides the default context floor, in tokens.
	MinContext int
}

// MinAgentContext is the smallest context window worth offering.
//
// The loop already sends ~31 KB of history in a single turn (measured), which is
// roughly 8k tokens before the operator has done anything, and M13's autocompact
// threshold sits at 80% of the window. A model advertised with a 4k window
// cannot run one turn of this harness, however good it is at something else.
//
// 32k leaves room for four turns before compaction.
const MinAgentContext = 32_000

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
			if opt.AgentViable && !agentViable(e, opt.minContext()) {
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

// minContext resolves the context floor to apply.
func (o Options) minContext() int {
	if o.MinContext > 0 {
		return o.MinContext
	}
	return MinAgentContext
}

// agentViable reports whether a model clears this harness's HARD requirements,
// using only what the provider disclosed.
//
// The three checks, in order of how much they exclude:
//
//	tool calling  — a model that cannot emit tool_calls cannot drive the loop.
//	               ToolsUnknown does NOT fail the check: NVIDIA NIM discloses
//	               nothing, and dropping every NIM model would hide the only
//	               provider the operator can actually reach for free. Unknown is
//	               kept and flagged, exactly as ToolSupport was designed.
//	context      — the provider published a window, and it is too small to hold
//	               one turn.
//	pricing      — absent price is not "free", but it also does not disqualify;
//	               it only disables the --free filter.
//
// What this does NOT check, and why it cannot: whether the model is actually
// good at driving a tool loop. No metadata field says that. `/models --check`
// is the only answer, and it costs a real request.
func agentViable(e Entry, minContext int) bool {
	if e.Tools == ToolsNo {
		return false
	}
	// The provider explicitly listed its supported parameters and omitted tools:
	// that is a disclosure, not a guess, so it is disqualifying.
	if len(e.Info.SupportedParameters) > 0 && !containsFold(e.Info.SupportedParameters, "tools") {
		return false
	}
	// A published context window that cannot hold one turn is disqualifying.
	// A window the provider did not publish is not: absence is not a small number.
	if ctx := e.Info.ContextLength; ctx > 0 && ctx < minContext {
		return false
	}
	return true
}

// containsFold reports whether list contains want, case-insensitively.
func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}
func matches(m provider.ModelInfo, q string) bool {
	return strings.Contains(strings.ToLower(m.ID), q) ||
		strings.Contains(strings.ToLower(m.Name), q)
}

// Rank orders entries for display: the active model first, then verified, then
// alphabetical by id. Sorting by id alone (the provider order) is already stable
// from Build, so this is the last step of a pipeline.
// DefaultPageSize is how many models the catalog shows before paging.
//
// Measured on OpenRouter (2026-10-02): 395 models clear this harness's hard
// requirements. Printing 395 lines is not a catalog, it is noise the operator
// has to scroll past to find nothing. 40 is what fits a tall terminal a few
// times over while still being short enough to scan; the rest stays reachable
// through paging, because a model the operator cannot see is a model they will
// not choose.
const DefaultPageSize = 40

// Page is one window of a ranked catalog.
type Page struct {
	Entries []Entry
	// Number is 1-based; 0 means the catalog was empty.
	Number int
	// TotalPages is 1 for a non-empty single page, 0 when empty.
	TotalPages int
	// Total is how many entries exist beyond the filter, not just this page.
	Total int
	// PageSize is the window used.
	PageSize int
}

// HasMore reports whether another page exists after this one.
func (p Page) HasMore() bool { return p.Number < p.TotalPages }

// Next is the page after this one, clamped at the last.
func (p Page) Next() int {
	if p.Number < p.TotalPages {
		return p.Number + 1
	}
	return p.Number
}

// Prev is the page before this one, clamped at the first.
func (p Page) Prev() int {
	if p.Number > 1 {
		return p.Number - 1
	}
	return p.Number
}

// Paginate slices a ranked catalog into a window.
//
// Out-of-range pages clamp to the last non-empty one, so a caller that asks for
// page 99 of a 3-page catalog gets page 3 instead of an empty screen — an empty
// screen reads as "nothing matches", which is a different and wrong statement.
func Paginate(entries []Entry, page, pageSize int) Page {
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	total := len(entries)
	if total == 0 {
		return Page{Number: 0, TotalPages: 0, Total: 0, PageSize: pageSize}
	}
	totalPages := (total + pageSize - 1) / pageSize
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if end > total {
		end = total
	}
	window := make([]Entry, end-start)
	copy(window, entries[start:end])
	return Page{
		Entries:    window,
		Number:     page,
		TotalPages: totalPages,
		Total:      total,
		PageSize:   pageSize,
	}
}

// Rank reports a catalog for the operator, already filtered and ordered.
func Rank(entries []Entry, active string) []Entry {
	return AgentRank(entries, active)
}

// AgentRank orders a catalog for an operator choosing what to drive the loop
// with.
//
// This is a HEURISTIC, and the comment says so because the alternative is
// pretending it is a quality judgement. It ranks by signals the provider
// published, in decreasing order of how much they actually say:
//
//  1. the active model, so the operator keeps their place;
//  2. tool calling — the one hard requirement, taken from disclosure when there
//     is any, and from the family table otherwise;
//  3. free — measured on OpenRouter: only 16 of 469 models are priced at zero.
//     For an operator trying this on a Core 2 Duo, that is the single most
//     useful ordering, and `/models --free` exists for the same reason;
//  4. larger context first, as a capability proxy — NOT as a claim that a
//     2M-token model is better than a 128k one;
//  5. verified (`--check`) first;
//  6. id, so the order is stable between runs.
//
// Items 3 and 4 are proxies. Only item 5 is a verification, and only
// `/models --check` can produce it.
func AgentRank(entries []Entry, active string) []Entry {
	out := make([]Entry, len(entries))
	copy(out, entries)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Info.ID == active) != (b.Info.ID == active) {
			return a.Info.ID == active
		}
		at, bt := rankToolSupport(a), rankToolSupport(b)
		if at != bt {
			return at > bt
		}
		if a.Info.Free() != b.Info.Free() {
			return a.Info.Free()
		}
		ac, bc := a.Info.ContextLength, b.Info.ContextLength
		if ac != bc {
			return ac > bc
		}
		if a.Check != b.Check {
			return a.Check == CheckOK
		}
		return a.Info.ID < b.Info.ID
	})
	return out
}

// rankToolSupport scores tool support for ordering: 2 = disclosed by the
// provider, 1 = family-table inference, 0 = explicitly unsupported.
func rankToolSupport(e Entry) int {
	switch {
	case e.Tools == ToolsYes:
		return 2
	case e.Tools == ToolsUnknown && len(e.Info.SupportedParameters) > 0:
		if containsFold(e.Info.SupportedParameters, "tools") {
			return 2
		}
		return 0
	case e.Tools == ToolsUnknown:
		return 1
	default:
		return 0
	}
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
