package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"sync"
)

const (
	// stateVersion is the on-disk schema version. A file claiming a higher
	// version is refused rather than guessed at.
	stateVersion = 1

	// maxModelIDBytes bounds a stored model id. The id comes from a provider
	// catalog, so it is remote input even when the operator picked it.
	maxModelIDBytes = 256
	// maxFileBytes bounds the whole file, enforced on READ.
	maxFileBytes = 64 << 10 // 64 KiB
)

// modelIDRe imposes a strict model-id grammar. A model id is sent back to the
// provider in the request path and printed in the UI, so it must not carry
// control characters, newlines, spaces or shell metacharacters that would let
// a provider-authored id break a line of output or a command line.
var modelIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]*$`)

// ValidateModelID enforces the model-id grammar (M14 spec §3.7). This is the
// guard that keeps a catalog-supplied id from becoming a UI/transport
// injection: without it, a provider could return a model id containing ANSI
// escapes or a newline and the picker would render it verbatim.
func ValidateModelID(id string) error {
	if id == "" {
		return errors.New("state: empty model id")
	}
	if len(id) > maxModelIDBytes {
		return fmt.Errorf("state: model id exceeds %d bytes", maxModelIDBytes)
	}
	if !modelIDRe.MatchString(id) {
		return fmt.Errorf("state: invalid model id (must match %s)", modelIDRe)
	}
	return nil
}

// file is the on-disk shape. Unknown fields are refused (fail-closed).
type file struct {
	Version int               `json:"version"`
	Models  map[string]string `json:"models"`
}

// Store persists the last model chosen per provider.
//
// It is deliberately NOT the credential store (internal/creds): a preference
// and a secret have different blast radii, and mixing them in one file is the
// short path to a permission bug that leaks a key (D-2026-10-02-68). Nothing
// here is sensitive; the file is still 0600 for consistency with the rest of
// the harness's state.
type Store struct {
	path string
	mu   sync.Mutex
}

// DefaultPath returns ~/.local/share/ancientcode/state.json. It sits next to
// auth.json, outside the project tree, so the model cannot read it as a
// project file (M14 spec S7).
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("state: resolve home: %w", err)
	}
	return filepath.Join(home, ".local", "share", "ancientcode", "state.json"), nil
}

// Open ensures the parent directory exists with 0700 and returns a Store.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("state: empty path")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("state: create state dir: %w", err)
	}
	if err := tightenDir(dir); err != nil {
		return nil, err
	}
	return &Store{path: path}, nil
}

// tightenDir forces the state directory to 0700 on POSIX. MkdirAll only applies
// the mode when it creates the directory; one that already existed keeps its
// old mode — the same gap found (and fixed) in internal/creds.
func tightenDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("state: stat state dir: %w", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("state: tighten state dir: %w", err)
		}
		fi, err = os.Stat(dir)
		if err != nil {
			return fmt.Errorf("state: re-stat state dir: %w", err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("state: state dir %s is mode %o, refusing to keep group/other access",
				dir, fi.Mode().Perm())
		}
	}
	return nil
}

// Path returns the backing file path.
func (s *Store) Path() string { return s.path }

// GetModel returns the last model chosen for a provider. The bool is existence:
// false means "never used", which is what makes SwitchProvider ask for /models
// instead of guessing (D-2026-10-02-68).
func (s *Store) GetModel(provider string) (string, bool, error) {
	f, err := s.load()
	if err != nil {
		return "", false, err
	}
	id, ok := f.Models[provider]
	if !ok {
		return "", false, nil
	}
	return id, true, nil
}

// SetModel records the model chosen for a provider. An invalid id is refused
// BEFORE it reaches disk, so a poisoned catalog value cannot be persisted.
func (s *Store) SetModel(provider, modelID string) error {
	if err := validateProviderKey(provider); err != nil {
		return err
	}
	if err := ValidateModelID(modelID); err != nil {
		return fmt.Errorf("state: refusing to store model for provider %q: %w", provider, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.load()
	if err != nil {
		return err
	}
	f.Models[provider] = modelID
	return s.write(f)
}

// Providers returns the sorted providers that have a remembered model.
func (s *Store) Providers() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(f.Models))
	for p := range f.Models {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// Delete forgets the remembered model for a provider. It is not an error to
// forget one that was never remembered.
func (s *Store) Delete(provider string) error {
	if err := validateProviderKey(provider); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.load()
	if err != nil {
		return err
	}
	delete(f.Models, provider)
	return s.write(f)
}

// providerKeyRe mirrors the credential store's provider grammar. The state file
// is not a path-traversal risk the way credentials are, but one grammar for
// provider ids across the codebase is worth more than the second chance to
// disagree later.
var providerKeyRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func validateProviderKey(p string) error {
	if !providerKeyRe.MatchString(p) {
		return fmt.Errorf("state: invalid provider id %q", p)
	}
	return nil
}

// load reads and strictly parses the store. A missing file is empty state.
func (s *Store) load() (*file, error) {
	data, err := os.ReadFile(s.path)
	switch {
	case err == nil:
	case os.IsNotExist(err):
		return &file{Version: stateVersion, Models: map[string]string{}}, nil
	default:
		return nil, fmt.Errorf("state: cannot read %s: %w", s.path, err)
	}
	if len(data) > maxFileBytes {
		return nil, fmt.Errorf("state: %s exceeds %d bytes", s.path, maxFileBytes)
	}
	if len(data) == 0 {
		return &file{Version: stateVersion, Models: map[string]string{}}, nil
	}

	f := &file{}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(f); err != nil {
		return nil, fmt.Errorf("state: invalid state %s: %w", s.path, err)
	}
	if f.Version > stateVersion {
		return nil, fmt.Errorf("state: %s has version %d, this build understands up to %d",
			s.path, f.Version, stateVersion)
	}
	if f.Models == nil {
		f.Models = map[string]string{}
	}
	for p, id := range f.Models {
		if err := validateProviderKey(p); err != nil {
			return nil, fmt.Errorf("state: invalid state %s: %w", s.path, err)
		}
		if err := ValidateModelID(id); err != nil {
			return nil, fmt.Errorf("state: invalid state %s: provider %q: %w", s.path, p, err)
		}
	}
	f.Version = stateVersion
	return f, nil
}

// write persists atomically (tmp + fsync + rename) and verifies the mode, for
// the same reason as the credential store: consistency of posture, so a future
// reader does not assume one file is hardened and the other is not.
func (s *Store) write(f *file) error {
	blob, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("state: encode state: %w", err)
	}
	blob = append(blob, '\n')

	dir := filepath.Dir(s.path)
	tmp, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	_ = dir
	if err != nil {
		return fmt.Errorf("state: create temp file: %w", err)
	}
	tmpName := s.path
	_ = tmpName
	defer func() {
		tmp.Close()
	}()

	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("state: restrict temp file: %w", err)
	}
	if _, err := tmp.Write(blob); err != nil {
		return fmt.Errorf("state: write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("state: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("state: close temp file: %w", err)
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		return fmt.Errorf("state: rename into place: %w", err)
	}
	return verifyPerms(s.path)
}

func verifyPerms(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("state: stat after write: %w", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("state: tighten state perms: %w", err)
		}
	}
	return nil
}
