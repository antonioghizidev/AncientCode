package creds

import (
	"errors"
	"fmt"
	"os"
)

// Source identifies WHERE the key in use came from. It exists so the UI can
// answer "which key am I using?" instead of the operator guessing — the
// original defect of a precedence rule that silently ignored the key the
// operator had just pasted (D-2026-10-02-66).
type Source string

const (
	// SourceNone means no credential is available at all.
	SourceNone Source = "nenhuma"
	// SourceEnv means the key came from api_key_env in the environment.
	SourceEnv Source = "env"
	// SourceStoreLoose means the key came from the store but the provider is
	// NOT pinned, so the environment would win if it had a value.
	SourceStoreLoose Source = "store solta"
	// SourceStoreFixed means the store won because /connect pinned the
	// provider. Only this state overrides the environment.
	SourceStoreFixed Source = "store fixada"
)

// String renders the source for the UI.
func (s Source) String() string { return string(s) }

// Pinned reports whether this source overrides the environment.
func (s Source) Pinned() bool { return s == SourceStoreFixed }

// Resolver is the seam config uses to ask "which key will be used?", without
// config having to know that a credential store exists. nil means "no store:
// the environment is the only source", which is exactly the pre-M14 behaviour
// and the CI path.
type Resolver interface {
	ResolveCredential(provider, envName string) (key string, src Source, err error)
}

// Resolve applies the precedence rule of D-2026-10-02-66, per provider:
//
//  1. store, when the provider is PINNED by /connect;
//  2. the environment variable named by api_key_env;
//  3. a stored but unpinned key, only if the environment has nothing;
//  4. otherwise an error naming BOTH sources tried.
//
// lookup is injected so the rule is testable without mutating the process
// environment; callers pass os.Getenv.
func (s *Store) Resolve(provider, envName string, lookup func(string) string) (string, Source, error) {
	if lookup == nil {
		lookup = os.Getenv
	}
	if err := ValidateProvider(provider); err != nil {
		return "", SourceNone, err
	}

	entry, fixed, err := s.Get(provider)
	stored := err == nil
	if err != nil && !errors.Is(err, ErrNoCredential) {
		// A corrupt or unreadable store is fail-closed: we do NOT silently
		// fall through to the environment, because that would hide a real
		// problem behind a working credential.
		return "", SourceNone, err
	}

	envKey := ""
	if envName != "" {
		envKey = lookup(envName)
	}

	if stored && fixed {
		return entry.Key, SourceStoreFixed, nil
	}
	if envKey != "" {
		return envKey, SourceEnv, nil
	}
	if stored {
		return entry.Key, SourceStoreLoose, nil
	}

	return "", SourceNone, fmt.Errorf(
		"creds: no credential for provider %q: tried store %s and environment variable %q "+
			"(run /connect to store a key, or export %s)",
		provider, s.path, envName, envName)
}

// ResolveCredential implements Resolver with the real environment.
//
// The source is returned as the underlying string, not as Source, so that
// config.CredentialResolver (which must not import this package) is satisfied
// without a wrapper: Source IS a string.
func (s *Store) ResolveCredential(provider, envName string) (string, string, error) {
	key, src, err := s.Resolve(provider, envName, os.Getenv)
	return key, string(src), err
}
