// M12: model catalog over the OpenAI-compatible GET /models endpoint
// (D-2026-10-01-53). This file is transport + parsing only; the *decision* about
// which models are usable lives in internal/catalog, so both frontends (REPL and
// TUI) filter with identical semantics.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/antonioghizidev/ancientcode/internal/redact"
)

// maxModelsResponseBytes bounds the catalog body. Measured: NIM returns 81
// entries, OpenRouter 464 — a few hundred KiB. The limit exists so a hostile or
// broken endpoint cannot exhaust memory or wedge the UI.
const maxModelsResponseBytes = 4 << 20 // 4 MiB

// maxModelEntries caps how many entries we keep, so the picker stays usable even
// if a provider returns tens of thousands of models.
const maxModelEntries = 2000

// ModelInfo is one catalog entry. Fields differ per provider: NIM returns only
// ID/owner, OpenRouter adds name, context, pricing and capabilities. Absent data
// is left zero and the catalog treats it as *unknown* — never as a capability.
type ModelInfo struct {
	ID            string
	Name          string
	OwnedBy       string
	ContextLength int
	// Pricing is per-token cost as decimal strings ("0" means free). Empty means
	// the provider did not say — the catalog must not guess "free".
	PricingPrompt     string
	PricingCompletion string
	// InputModalities is OpenRouter's `architecture.input_modalities`.
	InputModalities []string
	// SupportedParameters is OpenRouter's `supported_parameters` (e.g. "tools").
	SupportedParameters []string
	// AliasTarget is OpenRouter's redirect for deprecated ids ("" when absent).
	AliasTarget string
}

// Free reports whether the provider explicitly priced this model at zero.
// A missing price is NOT free: "we do not know" must never read as "yes".
func (m ModelInfo) Free() bool {
	return m.PricingPrompt == "0" && m.PricingCompletion == "0"
}

// PriceKnown reports whether the provider disclosed pricing at all. Only then
// can the UI offer a meaningful "free only" filter.
func (m ModelInfo) PriceKnown() bool {
	return m.PricingPrompt != "" || m.PricingCompletion != ""
}

// SupportsTextInput reports whether the provider said the model accepts text.
// Unknown (no metadata) returns false so callers can tell "no" from "not said".
func (m ModelInfo) SupportsTextInput() bool {
	if len(m.InputModalities) == 0 {
		return false
	}
	for _, mod := range m.InputModalities {
		if strings.EqualFold(strings.TrimSpace(mod), "text") {
			return true
		}
	}
	return false
}

// modelsPayload is the OpenAI-compatible list shape. The extra fields are
// optional and provider-specific; unknown fields are ignored by encoding/json.
type modelsPayload struct {
	Data []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		OwnedBy    string `json:"owned_by"`
		Object     string `json:"object"`
		ContextLen int    `json:"context_length"`
		Pricing    struct {
			Prompt     string `json:"prompt"`
			Completion string `json:"completion"`
			Request    string `json:"request"`
		} `json:"pricing"`
		Architecture struct {
			InputModalities []string `json:"input_modalities"`
		} `json:"architecture"`
		SupportedParameters []string `json:"supported_parameters"`
		AliasTarget         string   `json:"alias_target"`
		CanonicalSlug       string   `json:"canonical_slug"`
	} `json:"data"`
}

// ListModels fetches the provider catalog.
//
// Security: the env holding the key is read per request (never cached), the key
// never appears in errors, the body is read under a hard limit, and every
// provider-supplied string is redacted and stripped of control characters
// before it can reach a screen — the catalog is untrusted remote data.
func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	var baseURL, apiKeyEnv string
	c.mu.RLock()
	baseURL, apiKeyEnv = c.baseURL, c.apiKeyEnv
	c.mu.RUnlock()

	apiKeyEnv = strings.TrimSpace(apiKeyEnv)
	key := c.resolveKey(apiKeyEnv)
	if key == "" {
		// Report the variable NAME, never anything derived from its value.
		return nil, fmt.Errorf("provider: API key env %q is not set", apiKeyEnv)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("provider: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ancientcode/0.1")

	resp, err := c.http.Do(req)
	if err != nil {
		// %w keeps the transport cause (timeout, DNS, TLS) but the URL may carry
		// userinfo; redact the whole message for safety.
		return nil, fmt.Errorf("provider: list models request failed: %s", redact.Redact(err.Error()))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorResponseBytes))
		return nil, fmt.Errorf("provider: list models HTTP %d: %s",
			resp.StatusCode, redact.Redact(sanitizeCatalog(string(raw))))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxModelsResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("provider: read models list: %s", redact.Redact(err.Error()))
	}

	var payload modelsPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		// A truncated body (limit hit) fails to parse — say so instead of
		// silently returning a partial catalog.
		return nil, fmt.Errorf("provider: decode models list: %s", redact.Redact(err.Error()))
	}
	if len(payload.Data) == 0 {
		return nil, fmt.Errorf("provider: models list vazio (resposta sem campo data)")
	}

	out := make([]ModelInfo, 0, len(payload.Data))
	seen := make(map[string]bool, len(payload.Data))
	for _, e := range payload.Data {
		id := cleanCatalogString(e.ID)
		if id == "" || seen[id] {
			continue // empty or duplicate id: nothing to select
		}
		seen[id] = true
		out = append(out, ModelInfo{
			ID:                  id,
			Name:                cleanCatalogString(e.Name),
			OwnedBy:             cleanCatalogString(e.OwnedBy),
			ContextLength:       e.ContextLen,
			PricingPrompt:       cleanCatalogString(e.Pricing.Prompt),
			PricingCompletion:   cleanCatalogString(e.Pricing.Completion),
			InputModalities:     cleanCatalogStrings(e.Architecture.InputModalities),
			SupportedParameters: cleanCatalogStrings(e.SupportedParameters),
			AliasTarget:         cleanCatalogString(e.AliasTarget),
		})
		if len(out) >= maxModelEntries {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("provider: models list sem entradas utilizaveis")
	}
	// Stable order so the REPL numbering and the picker agree across calls.
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// cleanCatalogString makes a provider-supplied string safe to keep and to paint:
// secrets masked, control characters dropped (an id is data, never a terminal
// instruction), and whitespace collapsed.
func cleanCatalogString(s string) string {
	return sanitizeCatalog(redact.Redact(s))
}

// cleanCatalogStrings applies cleanCatalogString element-wise, dropping empties.
func cleanCatalogStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if c := cleanCatalogString(s); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// sanitizeCatalog strips C0/DEL/C1 control characters (ESC included) and
// collapses whitespace. Mirrors the TUI's paint hardening (M11): a model id
// arriving from the network must not be able to emit terminal sequences.
func sanitizeCatalog(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		switch {
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			space = true // drop; remember to separate tokens
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			space = true
		default:
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		}
	}
	return b.String()
}
