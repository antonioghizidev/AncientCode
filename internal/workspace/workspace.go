// Package workspace owns the two directories the harness must never confuse:
// the PROJECT ROOT (where the shell runs and where the file/code tools are
// confined) and the AGENT HOME (config, sessions/, audit.jsonl, scratch) —
// which is created at install time and is never something the agent edits.
//
// The split exists because the operator's real topology was unrepresentable:
// config.Validate required code_root inside workdir, so an agent installed in
// one home could not work on a source tree anywhere else.
//
// Security shape:
//   - /cd is an OPERATOR action. No tool exposes it to the model; an agent that
//     could move its own root would leave the confine without a trace.
//   - The agent home is never a valid project root or grant target. If it were,
//     the file tools could read and rewrite auth.json, and the confine is
//     per-directory, not per-filename.
//   - Paths are RESOLVED (symlinks) before any check, because checking the
//     literal path and then chdir-ing the resolved one validates one thing
//     and confines another.
//
// What this package deliberately does NOT do: confine the shell by path. The
// shell's "path" is free text; `find / -name x` and `ls .` are both "shell".
// The shell's real controls are an unprivileged run_user and an approval gate
// that shows the whole command. Promising a path confine that does not exist
// is what made /connect accept a wrong key once already (D-70).
package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ErrNotDirectory is returned when a path exists but is not a directory.
var ErrNotDirectory = errors.New("workspace: o caminho existe e nao e um diretorio")

// Workspace holds the mutable roots for one running session.
//
// All access goes through it, so /cd in one frontend is visible to the other
// and to a turn already in flight. The mutex is the same shape as
// Loop.SetToolboxMode (loop.go:71).
type Workspace struct {
	mu          sync.RWMutex
	projectRoot string
	agentHome   string
	scratch     string
	grants      []string
}

// New builds a workspace, creating the project root if it does not exist yet.
//
// Creating it is deliberate: the operator types the path of the project they
// are about to clone, and refusing an absent directory is friction without
// security — the path is the operator's, not the model's.
func New(projectRoot, agentHome string) (*Workspace, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return nil, errors.New("workspace: raiz do projeto vazia")
	}
	if strings.TrimSpace(agentHome) == "" {
		return nil, errors.New("workspace: casa do agente vazia")
	}
	home, err := filepath.EvalSymlinks(agentHome)
	if err != nil {
		home = filepath.Clean(agentHome)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, fmt.Errorf("workspace: criar casa do agente: %w", err)
	}

	root, err := resolveDir(projectRoot)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("workspace: criar raiz do projeto: %w", err)
	}
	if err := checkNotAgentHome(root, home); err != nil {
		return nil, err
	}

	scratch := filepath.Join(home, "scratch")
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		return nil, fmt.Errorf("workspace: criar rascunho: %w", err)
	}
	return &Workspace{projectRoot: root, agentHome: home, scratch: scratch}, nil
}

// resolveDir makes an absolute, symlink-resolved directory path, creating it
// when absent. The check runs AFTER resolving so a link cannot slip past.
func resolveDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("workspace: caminho invalido: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("workspace: resolver caminho: %w", err)
	}
	abs = filepath.Clean(abs)

	st, err := os.Stat(abs)
	switch {
	case err == nil && !st.IsDir():
		return "", ErrNotDirectory
	case err == nil:
		return abs, nil
	case os.IsNotExist(err):
		return abs, nil // ainda nao existe; quem chama cria
	default:
		return "", fmt.Errorf("workspace: stat: %w", err)
	}
}

// checkNotAgentHome refuses a root that is the agent home, sits inside it, or
// CONTAINS it.
//
// Inside, not just equal: that is where auth.json actually lives. Contains
// matters too — with the agent home at ~/ancientcode and the project at ~,
// every file tool could reach ~/ancientcode/auth.json by path. The confine is
// per-directory, so a root that swallows the home swallows the secret.
//
// Neither side may contain the other; they are siblings by design.
func checkNotAgentHome(root, home string) error {
	if sameOrUnder(root, home) || sameOrUnder(home, root) {
		return fmt.Errorf("workspace: %q e a casa do agente, ou a contem (guarda auth.json, sessions/ e audit.jsonl); "+
			"a raiz do projeto e a casa do agente sao irmas, uma nao dentro da outra", root)
	}
	return nil
}

// sameOrUnder reports whether p is base or lives under it.
func sameOrUnder(p, base string) bool {
	if p == base {
		return true
	}
	rel, err := filepath.Rel(base, p)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// SetProjectRoot points the session at a new project root.
func (w *Workspace) SetProjectRoot(path string) error {
	root, err := resolveDir(path)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := checkNotAgentHome(root, w.agentHome); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("workspace: criar raiz do projeto: %w", err)
	}
	w.projectRoot = root
	return nil
}

// ProjectRoot is where the shell runs and where file/code tools are confined.
func (w *Workspace) ProjectRoot() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.projectRoot
}

// AgentHome is the harness's own directory. It never moves during a session.
func (w *Workspace) AgentHome() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.agentHome
}

// Scratch is where the agent writes throwaway output without polluting the
// repository. Writes here are ungated by design.
func (w *Workspace) Scratch() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.scratch
}

// Widens reports whether moving to next would BROADEN the blast radius.
//
// Only an ancestor counts. A sibling is neither, and the current root is not
// either — otherwise a no-op /cd would ask for consent, which is how a
// confirmation prompt trains people to type "y" without reading.
func (w *Workspace) Widens(next string) bool {
	cur := w.ProjectRoot()
	resolved, err := resolveDir(next)
	if err != nil {
		// Cannot resolve: treat as widening so the operator is asked. Failing
		// closed on consent is the right side to err on.
		return true
	}
	if resolved == cur {
		return false
	}
	return sameOrUnder(cur, resolved)
}

// Grant allows writing under path for the rest of the session.
func (w *Workspace) Grant(path string) error {
	dir, err := resolveDir(path)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if sameOrUnder(dir, w.agentHome) {
		return fmt.Errorf("workspace: nao se concede escrita dentro da casa do agente (%q)", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("workspace: criar pasta concedida: %w", err)
	}
	for _, g := range w.grants {
		if g == dir {
			return nil
		}
	}
	w.grants = append(w.grants, dir)
	return nil
}

// Granted reports whether path is the project root, the scratch, or under a
// grant — the places a write may land without asking again.
func (w *Workspace) Granted(path string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	clean := filepath.Clean(path)
	if sameOrUnder(clean, w.projectRoot) || sameOrUnder(clean, w.scratch) {
		return true
	}
	for _, g := range w.grants {
		if sameOrUnder(clean, g) {
			return true
		}
	}
	return false
}

// Grants lists the session's extra write allowances, for the UI to show.
func (w *Workspace) Grants() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]string, len(w.grants))
	copy(out, w.grants)
	return out
}
