package state

import (
	"errors"
	"fmt"
	"strings"
)

// maxProjectRootBytes bounds a stored project root.
//
// The value is DISK, and a hand-edited state.json chooses the folder the agent
// will write into — so it gets the same treatment as a model id from a remote
// catalog: bounded, NUL-free, and validated on the way in AND on the way out.
// Trusting what a file on disk says about where to write is the bug, not the
// limit.
const maxProjectRootBytes = 4096

// SetProjectRoot remembers the project root for the next run (M15).
//
// Session-only writes still go through this: /cd persists so reopening the
// harness lands back in the project being worked on.
func (s *Store) SetProjectRoot(root string) error {
	if err := ValidateProjectRoot(root); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	f.ProjectRoot = root
	return s.write(f)
}

// ProjectRoot returns the remembered root, ok=false when none is stored.
//
// A value that fails validation on READ is reported as absent rather than
// returned: the caller falls back to the config, and the operator is not handed
// a path from a file someone edited.
func (s *Store) ProjectRoot() (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return "", false, err
	}
	if f.ProjectRoot == "" {
		return "", false, nil
	}
	if err := ValidateProjectRoot(f.ProjectRoot); err != nil {
		return "", false, fmt.Errorf("state: project_root no arquivo e invalido: %w", err)
	}
	return f.ProjectRoot, true, nil
}

// ValidateProjectRoot refuses a stored root that cannot be a directory.
func ValidateProjectRoot(root string) error {
	if strings.TrimSpace(root) == "" {
		return errors.New("raiz do projeto vazia")
	}
	if len(root) > maxProjectRootBytes {
		return fmt.Errorf("raiz do projeto acima de %d bytes", maxProjectRootBytes)
	}
	if strings.ContainsRune(root, 0) {
		return errors.New("raiz do projeto contem NUL")
	}
	return nil
}
