// Package connect implements the /connect flow shared by the TUI, the REPL and
// the CLI (M14 spec §3.5). It holds no UI and no terminal handling on purpose:
// the masked prompt, the picker and the termios live in cmd/ancientcode, while
// the rules that matter — verify before store, never store a rejected key,
// never accept a key from argv, always report the real source — live here where
// they can be tested.
package connect

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/antonioghizidev/ancientcode/internal/creds"
	"github.com/antonioghizidev/ancientcode/internal/provider"
	"github.com/antonioghizidev/ancientcode/internal/redact"
)

// ErrRejected reports that the provider refused the credential. The UI shows
// this as "chave recusada" and does NOT store anything.
var ErrRejected = errors.New("connect: credential rejected by provider")

// Verifier checks a candidate key against the provider before it is stored.
type Verifier interface {
	Verify(ctx context.Context, baseURL, key string) (modelCount int, err error)
}

// HTTPVerifier is the real Verifier, backed by the provider package. It is a
// value, not a pointer: the flow is stateless and the http.Client is safe for
// concurrent use.
type HTTPVerifier struct {
	Client *http.Client
}

// Verify performs one real request with the candidate key and reports how many
// models the provider listed.
func (v HTTPVerifier) Verify(ctx context.Context, baseURL, key string) (int, error) {
	res, err := provider.VerifyCredential(ctx, v.Client, baseURL, key)
	if err != nil {
		return 0, err
	}
	return res.ModelCount, nil
}

// Result reports what a connect did, so every surface can tell the operator
// the truth about which key is now in use (D-2026-10-02-66).
type Result struct {
	Provider   string
	Stored     bool
	Verified   bool // a real request confirmed the key before storing
	Replaced   bool // there was already a key for this provider
	ModelCount int  // how many models the provider listed
	Source     creds.Source
}

// String renders the outcome for logs and tests. It carries no key: Result has
// no key field, and that is deliberate — a result is a report, never a carrier.
func (r Result) String() string {
	what := "nao guardada"
	if r.Stored {
		what = "guardada"
		if r.Replaced {
			what = "substituida"
		}
	}
	verified := "nao verificada"
	if r.Verified {
		verified = "verificada"
	}
	return fmt.Sprintf("connect<%s %s %s fonte=%s modelos=%d>",
		r.Provider, what, verified, r.Source, r.ModelCount)
}

// Service performs connect operations against the credential store.
type Service struct {
	store    *creds.Store
	verifier Verifier
}

// New returns a Service. verifier may be nil, in which case Store is used
// without verification — only acceptable for tests and for the explicit
// --no-verify path, never for the interactive default.
func New(store *creds.Store, verifier Verifier) *Service {
	return &Service{store: store, verifier: verifier}
}

// Connect verifies a candidate key and, only if the provider accepts it, stores
// it pinned for the provider.
//
// The order is the whole point: store AFTER verify. Storing first would leave
// the operator with a "connected" UI and a broken provider.
func (s *Service) Connect(ctx context.Context, providerID, baseURL, key string) (Result, error) {
	if err := creds.ValidateProvider(providerID); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(key) == "" {
		return Result{}, errors.New("connect: empty credential; nothing was stored")
	}

	replaced := false
	if _, _, err := s.store.Get(providerID); err == nil {
		replaced = true
	} else if !errors.Is(err, creds.ErrNoCredential) {
		return Result{}, err
	}

	if s.verifier != nil {
		count, err := s.verifier.Verify(ctx, baseURL, key)
		if err != nil {
			if errors.Is(err, provider.ErrUnauthorized) {
				return Result{}, fmt.Errorf("%w: %s", ErrRejected, redact.Redact(err.Error()))
			}
			// The verifier's message is redacted here and not merely trusted to
			// be safe upstream: a proxy can echo the Authorization header back
			// inside a transport error, and this key is the fresh one the
			// operator just pasted. Cost of redacting: a masked string in a
			// rare error message. Cost of not doing it: the key on screen.
			return Result{}, fmt.Errorf("connect: verification failed, nothing was stored: %s",
				redact.Redact(err.Error()))
		}
		if err := s.store.Save(providerID, key); err != nil {
			return Result{}, err
		}
		return Result{
			Provider:   providerID,
			Stored:     true,
			Replaced:   replaced,
			Verified:   true,
			ModelCount: count,
			Source:     creds.SourceStoreFixed,
		}, nil
	}

	if err := s.store.Save(providerID, key); err != nil {
		return Result{}, err
	}
	return Result{
		Provider: providerID,
		Stored:   true,
		Replaced: replaced,
		Source:   creds.SourceStoreFixed,
	}, nil
}

// UseEnv releases the pin, keeping the stored value. It is the `--use-env`
// escape hatch: the operator goes back to resolving from the environment
// without losing the key they typed.
func (s *Service) UseEnv(providerID string) error {
	if err := s.store.SetFixed(providerID, false); err != nil {
		return err
	}
	return nil
}

// Logout removes the credential AND the pin.
func (s *Service) Logout(providerID string) error {
	return s.store.Delete(providerID)
}

// ProviderCandidate is a (provider id, api_key_env) pair the onboarding must
// consider before deciding the operator needs to configure anything.
type ProviderCandidate struct {
	ID      string
	EnvName string
}

// List returns the providers that have a stored credential. It is what
// `auth list` prints, so it returns names only — never a value.
func (s *Service) List() ([]string, error) {
	return s.store.List()
}

// HasAny reports whether ANY provider already has a usable credential, from the
// store OR from the environment.
//
// The onboarding of D-2026-10-02-67 uses it, and the environment half is the
// point: an operator who already exported NVIDIA_API_KEY must NOT be asked to
// connect. Asking anyway would be the harness insisting on its own storage over
// a working configuration.
func (s *Service) HasAny(cands []ProviderCandidate) (bool, error) {
	names, err := s.store.List()
	if err != nil {
		return false, err
	}
	if len(names) > 0 {
		return true, nil
	}
	for _, c := range cands {
		if _, _, err := s.store.Resolve(c.ID, c.EnvName, os.Getenv); err == nil {
			return true, nil
		}
	}
	return false, nil
}

// Status describes what a provider would use right now, including the source.
// This is what `/provider` shows so the operator never has to guess which key
// is in play.
type Status struct {
	Provider   string
	Configured bool
	Source     creds.Source
}

// StatusFor reports the credential state of a provider.
func (s *Service) StatusFor(providerID, envName string) Status {
	key, src, err := s.store.Resolve(providerID, envName, os.Getenv)
	st := Status{Provider: providerID, Source: creds.SourceNone}
	if err != nil {
		st.Configured = false
		return st
	}
	_ = key // the value is deliberately dropped: status is not a data channel
	st.Configured = true
	st.Source = src
	return st
}

// Describe renders a Status for the UI, naming the source so the operator can
// tell a pinned store from an environment variable at a glance.
func (st Status) Describe() string {
	if !st.Configured {
		return fmt.Sprintf("sem credencial (rode /connect %s)", st.Provider)
	}
	return fmt.Sprintf("chave: %s", st.Source)
}
