// Package creds stores provider credentials in a single 0600 JSON file, written
// only by explicit operator command (M14 spec §4, D-2026-10-02-65).
//
// Security contract (SPEC §5.5, M14 spec §4):
//   - The YAML configuration NEVER holds a key value — only api_key_env. What
//     this package adds is exactly one extra location: a 0600 file, written only
//     by an explicit operator command.
//   - 0600 does NOT protect the key from the agent. shell runs as run_user,
//     an unprivileged user: whoever can read the key is already the operator.
//     The real defense is the human gate (§5.6). This is stated on purpose so
//     nobody mistakes this package for a security control.
//   - A key never appears in an error, a log, a path, or a String().
//   - Writes are atomic (tmp + rename): a failure mid-write never leaves a
//     truncated credential readable.
//   - Parsing is fail-closed: an unknown field or a future version is an error,
//     never a silent ignore.
package creds

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// StoreVersion is the on-disk schema version. A file claiming a higher
	// version is refused rather than guessed at.
	storeVersion = 1

	// maxEntryBytes bounds one credential entry, maxFileBytes the whole file.
	// Both are enforced on READ: the file is operator-adjacent, so a huge one
	// is either a mistake or an attempt to exhaust memory.
	maxEntryBytes = 256 << 10 // 256 KiB
	maxFileBytes  = 1 << 20   // 1 MiB
)

// providerRe imposes the strict provider-id grammar. Reusing the skill-name
// grammar (M7) keeps ids lowercase, dash-separated and free of separators and
// dots, so an id can never traverse a path or hide in a filename.
var providerRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ValidateProvider enforces the provider-id grammar.
func ValidateProvider(p string) error {
	if p == "" {
		return errors.New("creds: empty provider id")
	}
	if !providerRe.MatchString(p) {
		return fmt.Errorf("creds: invalid provider id %q (must match %s)", p, providerRe)
	}
	return nil
}

// Entry is one stored credential. The Fixed flag is the pin of D-2026-10-02-66:
// while true, this store wins over api_key_env for this provider.
type Entry struct {
	Key     string `json:"key"`
	Fixed   bool   `json:"fixed"`
	AddedAt string `json:"added_at,omitempty"`

	// provider is unexported so it never reaches JSON, and is filled by the
	// store before the Entry is handed out, so String can name the provider.
	provider string
}

// String is deliberately safe: it identifies the provider and the pin state,
// never the key. This is what a debug print or a %v of a struct would use.
func (e Entry) String() string {
	pin := "solta"
	if e.Fixed {
		pin = "fixada"
	}
	return fmt.Sprintf("creds<%s pin=%s sem chave>", e.provider, pin)
}

// provider is filled by the store before String is reachable; entries returned
// to callers keep it so the String above never prints an empty id.
func (e Entry) withProvider(p string) Entry {
	e.provider = p
	return e
}

// file is the on-disk shape. Fields are explicit: an unknown key anywhere is an
// error (fail-closed, M14 spec S8).
type file struct {
	Version   int              `json:"version"`
	Providers map[string]Entry `json:"providers"`
}

// Store is a mutex-guarded handle on one auth.json.
type Store struct {
	path string
	mu   sync.Mutex
}

// DefaultPath returns ~/.local/share/ancientcode/auth.json, the XDG-style data
// location. It is a path under the user's home, never inside the project, so
// the model cannot read it as a project file (M14 spec S7).
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("creds: resolve home: %w", err)
	}
	return filepath.Join(home, ".local", "share", "ancientcode", "auth.json"), nil
}

// Open ensures the parent directory exists with 0700 and returns a Store. It
// does not read the file: a missing store is not an error until Get.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("creds: empty path")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creds: create auth dir: %w", err)
	}
	// MkdirAll only applies the mode when it CREATES the directory. One that
	// already existed keeps its old mode, which is how the auth dir ended up
	// 0755: readable by other users' listings. Tighten it rather than assume.
	if err := tightenDir(dir); err != nil {
		return nil, err
	}
	return &Store{path: path}, nil
}

// tightenDir forces the auth directory to 0700 on POSIX. Skipped on Windows,
// where the bits do not exist and Chmod is a no-op.
func tightenDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("creds: stat auth dir: %w", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("creds: tighten auth dir: %w", err)
		}
		fi, err = os.Stat(dir)
		if err != nil {
			return fmt.Errorf("creds: re-stat auth dir: %w", err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("creds: auth dir %s is mode %o, refusing to keep group/other access",
				dir, fi.Mode().Perm())
		}
	}
	return nil
}

// Path returns the backing file path. It is a path, never a value.
func (s *Store) Path() string { return s.path }

// Get returns the entry for a provider and whether it is pinned. The bool is
// the pin, not existence: a missing provider returns a zero Entry and an error
// naming only the provider.
func (s *Store) Get(provider string) (Entry, bool, error) {
	if err := ValidateProvider(provider); err != nil {
		return Entry{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.load()
	if err != nil {
		return Entry{}, false, err
	}
	e, ok := f.Providers[provider]
	if !ok {
		return Entry{}, false, fmt.Errorf("creds: no credential stored for provider %q", provider)
	}
	if len(e.Key) > maxEntryBytes {
		return Entry{}, false, fmt.Errorf(
			"creds: entry for provider %q exceeds %d bytes", provider, maxEntryBytes)
	}
	return e.withProvider(provider), e.Fixed, nil
}

// Save stores a key and PINS the provider. Reconnecting overwrites: wanting a
// new key is the reason to reconnect (D-2026-10-02-66).
func (s *Store) Save(provider, key string) error {
	return s.save(provider, key, true)
}

// SetFixed releases or restores the pin while keeping the stored value, which
// is what `/connect <p> --use-env` does.
func (s *Store) SetFixed(provider string, fixed bool) error {
	if err := ValidateProvider(provider); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.load()
	if err != nil {
		return err
	}
	e, ok := f.Providers[provider]
	if !ok {
		return fmt.Errorf("creds: no credential stored for provider %q", provider)
	}
	e.Fixed = fixed
	f.Providers[provider] = e
	return s.write(f)
}

// Delete removes the credential AND the pin for a provider: logout means out.
func (s *Store) Delete(provider string) error {
	if err := ValidateProvider(provider); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := f.Providers[provider]; !ok {
		return fmt.Errorf("creds: no credential stored for provider %q", provider)
	}
	delete(f.Providers, provider)
	return s.write(f)
}

// List returns the sorted provider ids that have a credential. Names only,
// never values.
func (s *Store) List() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(f.Providers))
	for p := range f.Providers {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// Has reports whether any provider has a stored credential. The onboarding of
// D-2026-10-02-67 asks this before opening a dialog.
func (s *Store) Has() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.load()
	if err != nil {
		return false, err
	}
	return len(f.Providers) > 0, nil
}

// save stores a key with the given pin state. An empty key is refused: a store
// entry that exists but cannot authenticate is worse than no entry, because it
// makes the UI claim a credential is configured.
func (s *Store) save(provider, key string, fixed bool) error {
	if err := ValidateProvider(provider); err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("creds: refusing to store an empty key for provider %q", provider)
	}
	if len(key) > maxEntryBytes {
		return fmt.Errorf("creds: key for provider %q exceeds %d bytes", provider, maxEntryBytes)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.load()
	if err != nil {
		return err
	}
	if f.Providers == nil {
		f.Providers = map[string]Entry{}
	}
	f.Providers[provider] = Entry{Key: key, Fixed: fixed, AddedAt: time.Now().UTC().Format(time.RFC3339)}
	return s.write(f)
}

// load reads and strictly parses the store. A missing file is an empty store,
// not an error — that is the state onboarding expects to find.
func (s *Store) load() (*file, error) {
	data, err := os.ReadFile(s.path)
	switch {
	case err == nil:
	case os.IsNotExist(err):
		return &file{Version: storeVersion, Providers: map[string]Entry{}}, nil
	default:
		return nil, fmt.Errorf("creds: cannot read %s: %w", s.path, err)
	}
	if len(data) > maxFileBytes {
		return nil, fmt.Errorf("creds: %s exceeds %d bytes", s.path, maxFileBytes)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return &file{Version: storeVersion, Providers: map[string]Entry{}}, nil
	}

	f := &file{}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(f); err != nil {
		return nil, fmt.Errorf("creds: invalid store %s: %w", s.path, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("creds: invalid store %s: trailing data", s.path)
	}
	if f.Version > storeVersion {
		return nil, fmt.Errorf("creds: store %s has version %d, this build understands up to %d",
			s.path, f.Version, storeVersion)
	}
	if f.Providers == nil {
		f.Providers = map[string]Entry{}
	}
	for p, e := range f.Providers {
		if err := ValidateProvider(p); err != nil {
			return nil, fmt.Errorf("creds: invalid store %s: %w", s.path, err)
		}
		if len(e.Key) > maxEntryBytes {
			return nil, fmt.Errorf("creds: store %s: entry %q exceeds %d bytes",
				s.path, p, maxEntryBytes)
		}
	}
	f.Version = storeVersion
	return f, nil
}

// write persists the store atomically: a 0600 temp file in the SAME directory,
// fsync, chmod, rename. A failure at any step leaves the previous file intact
// and removes the temp, so there is never a truncated or world-readable
// credential on disk (M14 spec S1, S2).
func (s *Store) write(f *file) error {
	blob, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("creds: encode store: %w", err)
	}
	blob = append(blob, '\n')
	if len(blob) > maxFileBytes {
		return fmt.Errorf("creds: store would exceed %d bytes", maxFileBytes)
	}

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".auth-*.tmp")
	if err != nil {
		return fmt.Errorf("creds: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	// Any early return below must not leave the temp behind, and must not leave
	// a file other users can read.
	defer func() {
		tmp.Close()
		os.Remove(tmpName)
	}()

	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("creds: restrict temp file: %w", err)
	}
	if _, err := tmp.Write(blob); err != nil {
		return fmt.Errorf("creds: write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("creds: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("creds: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("creds: rename into place: %w", err)
	}

	// Rename preserves the temp's 0600, but verify rather than assume: the
	// whole point of this package is not to trust the surrounding system.
	return verifyPerms(s.path)
}

// verifyPerms re-checks that the store is not readable by group or other. A
// pre-existing loose file (e.g. left by a manual copy) is tightened to 0600.
//
// On Windows the POSIX bits do not exist: os.Chmod returns nil and os.Stat
// still reports 0666, so enforcing here would make every save fail on the
// development host while protecting nothing. The check is therefore skipped
// there and stays strict on POSIX — which is the deployment target, and where
// the S1 tests run.
func verifyPerms(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("creds: stat store after write: %w", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("creds: tighten store perms: %w", err)
		}
		fi, err = os.Stat(path)
		if err != nil {
			return fmt.Errorf("creds: re-stat store: %w", err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("creds: store %s is mode %o, refusing to keep a group/other-readable credential",
				path, fi.Mode().Perm())
		}
	}
	return nil
}
