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

// ErrUnauthorized reports that the provider rejected the credential. It is a
// DISTINCT error because it is the one case where the operator pasted a wrong
// key: the message has to say so, and /connect must not store it (M14 §3.5).
var ErrUnauthorized = errors.New("provider: credential rejected")

// VerifyResult reports what a credential check learned beyond "it works".
type VerifyResult struct {
	// ModelCount is how many catalog entries the provider returned. It is a
	// liveness signal: a key that authenticates but lists nothing is almost
	// always wrong for this project.
	ModelCount int
}

// maxVerifyResponseBytes bounds the body read during verification. Verification
// only needs to know that the catalog is non-empty, so this is far smaller than
// the full catalog limit.
const maxVerifyResponseBytes = 64 << 10 // 64 KiB

// VerifyCredential makes one real request with a candidate key before it is
// stored (M14 §3.5: "verificar antes de gravar").
//
// Why this exists: storing an unverified key moves the failure from the moment
// the operator pastes it — where the message can be precise — to the first real
// turn, where the failure looks like "the model is broken". Verifying here also
// warms the catalog the /models picker will need.
//
// Security: the key is used only as an Authorization header, never logged, and
// never included in an error; the base URL is subject to the same https-or-
// loopback rule as the configuration (SPEC §5.4); the body is read under a hard
// limit; and every provider string is sanitized before it can reach a screen.
func VerifyCredential(ctx context.Context, httpc *http.Client, baseURL, key string) (VerifyResult, error) {
	if err := checkCredentialBaseURL(baseURL); err != nil {
		return VerifyResult{}, err
	}
	if strings.TrimSpace(key) == "" {
		return VerifyResult{}, errors.New("provider: refusing to verify an empty credential")
	}
	if httpc == nil {
		httpc = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("provider: create verify request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ancientcode/0.1")

	resp, err := httpc.Do(req)
	if err != nil {
		// The URL may carry userinfo; redact the whole message.
		return VerifyResult{}, fmt.Errorf("provider: verify request failed: %s", redact.Redact(err.Error()))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		// The body of a 401 can echo the submitted key back; read it only to
		// bound the connection and never surface it.
		_, _ = io.ReadAll(io.LimitReader(resp.Body, maxErrorResponseBytes))
		return VerifyResult{}, fmt.Errorf("%w: HTTP %d for the configured provider", ErrUnauthorized, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorResponseBytes))
		return VerifyResult{}, fmt.Errorf("provider: verify HTTP %d: %s",
			resp.StatusCode, redact.Redact(sanitizeCatalog(string(raw))))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxVerifyResponseBytes))
	if err != nil {
		return VerifyResult{}, fmt.Errorf("provider: read verify response: %s", redact.Redact(err.Error()))
	}
	var payload modelsPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return VerifyResult{}, fmt.Errorf("provider: decode verify response: %s", redact.Redact(err.Error()))
	}

	count := 0
	for _, e := range payload.Data {
		if cleanCatalogString(e.ID) != "" {
			count++
		}
	}
	if count == 0 {
		// Authenticates but offers nothing: storing this key would produce a
		// /models that is permanently empty, with no explanation.
		return VerifyResult{}, fmt.Errorf(
			"provider: credential authenticou mas o provider nao devolveu nenhum modelo usavel")
	}
	return VerifyResult{ModelCount: count}, nil
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
