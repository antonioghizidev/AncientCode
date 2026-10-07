package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/antonioghizidev/ancientcode/internal/redact"
)

// ErrUnauthorized reports that the provider REJECTED the credential: the key is
// wrong, revoked, or not authorized. This is the one case where the operator
// pasted a bad key, so the message has to say so and /connect must not store it
// (M14 §3.5).
var ErrUnauthorized = errors.New("provider: credential rejected")

// ErrInconclusive reports that the check could NOT tell whether the key is good.
// It is deliberately a different error from ErrUnauthorized, because conflating
// the two is how an operator ends up told "your key is wrong" when the provider
// was merely rate-limiting or the probe model had retired.
//
// The distinction was not theoretical: the first implementation of this file
// verified with `GET /models`, which BOTH NVIDIA and OpenRouter serve publicly
// and WITHOUT authentication. It returned HTTP 200 byte-for-byte identically
// with a valid key, an invalid key, and no key at all, so every pasted string
// was stored as "verified". The unit tests passed because the mock returned
// 401 — a mock stricter than the real system hides exactly this class of defect.
var ErrInconclusive = errors.New("provider: credential check inconclusive")

// Outcome classifies what the verification learned. It exists so the caller can
// report the truth instead of collapsing everything into "ok".
type Outcome int

const (
	// OutcomeOK: the provider authenticated the key with a real request.
	OutcomeOK Outcome = iota
	// OutcomeInvalid: the provider rejected the key (401/403). Never stored.
	OutcomeInvalid
	// OutcomeInconclusive: the provider could not confirm or deny. Never
	// reported as verified, and never reported as a bad key either.
	OutcomeInconclusive
)

// VerifyResult reports what a credential check learned.
type VerifyResult struct {
	Outcome Outcome
	// Status is the HTTP status that decided the outcome (0 when the request
	// never completed). The UI names it so the operator sees the real reason.
	Status int
	// ModelCount is how many catalog entries the provider published. It is
	// free to fetch (the catalog is public on both measured providers) and it
	// warms the list the /models picker will need.
	ModelCount int
	// Probe is a short description of the request that decided the outcome,
	// for the error message. It never contains the key.
	Probe string
}

const (
	// maxVerifyResponseBytes bounds the body read during verification.
	maxVerifyResponseBytes = 64 << 10 // 64 KiB
	// chatProbeCandidates is how many catalog models the chat probe will try
	// before declaring itself inconclusive. Bounded on purpose: each attempt is
	// a real network call, and "keep trying until something answers" would turn
	// a retired model into an open-ended loop.
	//
	// The value is measured, not guessed. On the Inspiron, NVIDIA's catalog
	// listed 80 models and only ONE of the first 20 answered 200 for a
	// perfectly valid key — the rest return 404 ("Function ... not found for
	// account"), which passes authentication and fails model resolution. A
	// bound of 3 would have rejected a working key roughly nineteen times out of
	// twenty. Twelve keeps that failure rate low without making /connect a
	// long walk through a dead catalog.
	chatProbeCandidates = 12
)

// VerifyCredential makes one real request that actually AUTHENTICATES, before
// the key is stored (M14 §3.5).
//
// The strategy is per provider, because "the" verification endpoint does not
// exist: measured on 2026-10-02 against the real services,
//
//	nvidia/v1/models            -> 200 with a valid key, an invalid key and NO key
//	openrouter/api/v1/models     -> 200 in all three cases
//	openrouter/api/v1/key        -> 401 invalid / 200 valid   (cheap, no tokens)
//	nvidia/v1/chat/completions   -> 401 invalid / past-auth valid
//
// So a catalog listing proves liveness, never identity, and OpenRouter has an
// identity endpoint while NVIDIA does not.
func VerifyCredential(ctx context.Context, httpc *http.Client, providerID, baseURL, key, modelHint string) (VerifyResult, error) {
	if err := checkCredentialBaseURL(baseURL); err != nil {
		return VerifyResult{Outcome: OutcomeInconclusive}, err
	}
	if strings.TrimSpace(key) == "" {
		return VerifyResult{Outcome: OutcomeInconclusive},
			errors.New("provider: refusing to verify an empty credential")
	}
	if httpc == nil {
		httpc = http.DefaultClient
	}
	base := strings.TrimRight(baseURL, "/")

	var res VerifyResult
	var err error
	switch verifyStrategyFor(providerID) {
	case strategyKeyIdentity:
		res, err = verifyByKeyIdentity(ctx, httpc, base, key)
	default:
		res, err = verifyByChatProbe(ctx, httpc, base, key, modelHint)
	}
	if err != nil {
		return res, err
	}

	// Best effort: count the catalog so the operator sees "N modelos" and the
	// /models picker starts warm. Never turns a successful check into a failure.
	if count, cerr := catalogCount(ctx, httpc, base, key); cerr == nil {
		res.ModelCount = count
	}
	return res, nil
}

// verifyStrategy picks how a provider is asked "is this key you?".
type verifyStrategy int

const (
	// strategyKeyIdentity: the provider publishes an identity endpoint that
	// answers 401 for a bad key. Free, and definitive.
	strategyKeyIdentity verifyStrategy = iota
	// strategyChatProbe: no identity endpoint, so send the smallest possible
	// real request (one token) and read the status. Costs a fraction of a cent.
	strategyChatProbe
)

// verifyStrategyFor maps a provider id to its strategy.
//
// openrouter has /key (measured). nvidia has no identity endpoint and
// authenticates only on the chat path (measured). litellm is a self-hosted proxy
// with no published contract and NO instance to measure against, so it takes the
// chat probe — the conservative choice, because the probe cannot be fooled into
// succeeding by a public endpoint.
func verifyStrategyFor(providerID string) verifyStrategy {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "openrouter":
		return strategyKeyIdentity
	case "nvidia":
		return strategyChatProbe
	default:
		return strategyChatProbe
	}
}

// verifyByKeyIdentity calls GET <base>/key, which reports the identity of the
// credential and answers 401 when it does not authenticate.
func verifyByKeyIdentity(ctx context.Context, httpc *http.Client, base, key string) (VerifyResult, error) {
	const probe = "GET /key"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/key", nil)
	if err != nil {
		return inconclusive(0, probe, fmt.Errorf("provider: create verify request: %w", err))
	}
	setVerifyHeaders(req, key)

	resp, err := httpc.Do(req)
	if err != nil {
		// The URL may carry userinfo; redact the whole message.
		return inconclusive(0, probe, fmt.Errorf("provider: %s request failed: %s", probe, redact.Redact(err.Error())))
	}
	defer resp.Body.Close()
	drainBody(resp.Body)

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return VerifyResult{Outcome: OutcomeInvalid, Status: resp.StatusCode, Probe: probe},
			fmt.Errorf("%w: HTTP %d em %s", ErrUnauthorized, resp.StatusCode, probe)
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return VerifyResult{Outcome: OutcomeOK, Status: resp.StatusCode, Probe: probe}, nil
	default:
		return inconclusive(resp.StatusCode, probe, nil)
	}
}

// verifyByChatProbe sends the smallest real completion request that could exist
// and reads the status.
//
// Status reading, which is the whole point:
//
//	401/403 -> the key is wrong. Definitive.
//	2xx    -> the key authenticated. Definitive.
//	404    -> auth PASSED (NVIDIA checks the key before resolving the model) but
//	          that model is retired or unknown for this account. Try another.
//	429/5xx/network -> we learned nothing about the key.
//
// A 404 is never treated as "valid" either: authenticating is not the same as
// being able to answer, and the operator would be told "verified" for a key
// whose only proof was that it got past a check.
func verifyByChatProbe(ctx context.Context, httpc *http.Client, base, key, modelHint string) (VerifyResult, error) {
	const probe = "POST /chat/completions"

	// Candidate models: what the provider publishes first. The catalog is
	// public, so this costs nothing and avoids hardcoding a model that may
	// retire out from under the milestone.
	candidates := probeModels(ctx, httpc, base, key, modelHint)

	attempted := 0
	var lastStatus int
	for _, model := range candidates {
		if attempted >= chatProbeCandidates {
			break
		}
		attempted++
		status, err := chatProbeOnce(ctx, httpc, base, key, model)
		if err != nil {
			return inconclusive(0, probe, fmt.Errorf("provider: %s falhou: %s", probe, redact.Redact(err.Error())))
		}
		lastStatus = status
		switch {
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			return VerifyResult{Outcome: OutcomeInvalid, Status: status, Probe: probe},
				fmt.Errorf("%w: HTTP %d em %s", ErrUnauthorized, status, probe)
		case status >= 200 && status < 300:
			return VerifyResult{Outcome: OutcomeOK, Status: status, Probe: probe}, nil
		case status == http.StatusNotFound:
			// Auth passed; this model cannot serve. Try the next one.
			continue
		default:
			// 429, 5xx, anything else: we do not know.
			return inconclusive(status, probe, nil)
		}
	}
	return inconclusive(lastStatus, probe, nil)
}

// chatProbeOnce sends one minimal completion request and returns only the status.
// max_tokens=1 keeps the cost at the noise floor; the content is never read.
func chatProbeOnce(ctx context.Context, httpc *http.Client, base, key, model string) (int, error) {
	body := strings.NewReader(
		`{"model":` + jsonString(model) +
			`,"messages":[{"role":"user","content":"hi"}],"max_tokens":1}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", body)
	if err != nil {
		return 0, err
	}
	setVerifyHeaders(req, key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	// The body can echo the Authorization header back on a proxy error; read it
	// only to bound the connection and never surface it.
	drainBody(resp.Body)
	return resp.StatusCode, nil
}

// probeModels returns the models to try in the chat probe, most likely first.
//
// The hint — the model the operator is actually configured to use — goes first,
// and that is not a nicety. Measured on the Inspiron: NVIDIA's catalog leads with
// models that have already RETIRED for a given account (01-ai/yi-large answers
// 404 to a perfectly valid key), so probing the first few catalog entries
// rejected a working key as "inconclusive". The model the session will use is
// both the fairest test and the most likely to answer.
//
// The list is NOT truncated here; the loop bounds the attempts. An earlier
// version truncated AND counted, which made the bound untestable — removing
// either left the property standing, so a mutation stayed green and I nearly
// logged the probe as covered when a different line was defending it.
func probeModels(ctx context.Context, httpc *http.Client, base, key, hint string) []string {
	const fallback = "gpt-4o-mini"
	ids, err := catalogIDs(ctx, httpc, base, key)
	if err != nil {
		ids = nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	add(strings.TrimSpace(hint))
	for _, id := range ids {
		add(id)
	}
	if len(out) == 0 {
		add(fallback)
	}
	return out
}

// catalogIDs lists published model ids.
func catalogIDs(ctx context.Context, httpc *http.Client, base, key string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, err
	}
	setVerifyHeaders(req, key)
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		drainBody(resp.Body)
		return nil, fmt.Errorf("provider: catalog HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxVerifyResponseBytes))
	if err != nil {
		return nil, err
	}
	var payload modelsPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range payload.Data {
		if id := cleanCatalogString(e.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// catalogCount returns how many models the provider publishes, or 0. The catalog
// is public on both measured providers, so this needs no valid key.
func catalogCount(ctx context.Context, httpc *http.Client, base, key string) (int, error) {
	ids, err := catalogIDs(ctx, httpc, base, key)
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}

func setVerifyHeaders(req *http.Request, key string) {
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ancientcode/0.1")
}

// inconclusive builds the "we could not tell" outcome. It must never be
// reported to the operator as a bad key.
func inconclusive(status int, probe string, cause error) (VerifyResult, error) {
	res := VerifyResult{Outcome: OutcomeInconclusive, Status: status, Probe: probe}
	if cause != nil {
		return res, fmt.Errorf("%w: %s", ErrInconclusive, cause.Error())
	}
	return res, fmt.Errorf("%w: %s respondeu HTTP %d — isso nao diz se a chave e valida, entao nada foi guardado",
		ErrInconclusive, probe, status)
}

// drainBody reads and discards a bounded amount of the body, so the connection
// can be reused and nothing large is buffered.
func drainBody(r io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(r, maxErrorResponseBytes))
}

// jsonString quotes a string as a JSON literal. The model id comes from a
// remote catalog, so it must not be concatenated into JSON unescaped.
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

// checkCredentialBaseURL enforces the same transport rule as the configuration:
// https, or http only for loopback (self-hosted LiteLLM on the same machine).
// Without it, a base URL could downgrade verification to cleartext and leak the
// candidate key — a key that, unlike the stored one, has never been vetted.
func checkCredentialBaseURL(baseURL string) error {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return fmt.Errorf("provider: invalid base_url %q: %w", baseURL, err)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackVerifyHost(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("provider: insecure base_url %q: plain http is only allowed for loopback hosts", baseURL)
	default:
		return fmt.Errorf("provider: base_url scheme %q must be http(s)", u.Scheme)
	}
}

func isLoopbackVerifyHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
