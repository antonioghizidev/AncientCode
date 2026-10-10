package main

import (
	"fmt"
	"strings"

	"github.com/antonioghizidev/ancientcode/internal/workspace"
)

// cdResult is what the /cd handler needs to decide whether to ask.
//
// Apply is separate from the decision on purpose: the checks that determine
// "this widens" are read-only, and the mutation must not happen before the
// operator says yes. Splitting them makes "nothing changed until confirmed" a
// property of the shape, not of remembering to order two statements.
type cdResult struct {
	// NeedsConfirm is true only when the move BROADENS the blast radius.
	NeedsConfirm bool
	// Message is shown to the operator: the resolved path, or why it is refused.
	Message string
	// Err is set when the destination is invalid. Nothing is applied.
	Err error
	// path is the resolved destination, kept private so only Apply uses it.
	path string
}

// cdOutcome reports what an Apply actually did.
type cdOutcome struct {
	Changed bool
	Root    string
	Err     error
}

// cdCommand evaluates /cd WITHOUT touching the workspace.
//
// arg empty is a question, not a command: the operator asks where they are and
// gets an answer. A missing argument that silently reset to a default would be
// the worst reading of it.
func cdCommand(w *workspace.Workspace, arg string) cdResult {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return cdResult{
			Message: fmt.Sprintf("projeto: %s\nrascunho: %s", w.ProjectRoot(), w.Scratch()),
		}
	}
	// Validate BEFORE deciding whether to prompt. Asking the operator to
	// approve a destination that can never be accepted is worse than useless:
	// it teaches that the prompt is decorative.
	if err := w.CanUse(arg); err != nil {
		return cdResult{Err: err, Message: err.Error()}
	}
	resolved, err := workspace.ResolveDir(arg)
	if err != nil {
		return cdResult{Err: err, Message: err.Error()}
	}
	res := cdResult{path: resolved}
	res.NeedsConfirm = w.Widens(resolved)
	res.Message = fmt.Sprintf("raiz do projeto: %s -> %s (amplia o alcance%s)", w.ProjectRoot(), resolved, grantSuffix(w))
	return res
}

func grantSuffix(w *workspace.Workspace) string {
	if g := w.Grants(); len(g) > 0 {
		return fmt.Sprintf("; concessoes ativas: %s", strings.Join(g, ", "))
	}
	return ""
}

// Apply performs the switch, but only if confirmed when required.
//
// A cdResult that failed to resolve has no path, so Apply is a no-op: there is
// nothing valid to apply, and inventing a fallback would be the /connect
// mistake again.
func (r cdResult) Apply(w *workspace.Workspace, confirmed bool) cdOutcome {
	if r.Err != nil {
		return cdOutcome{Root: w.ProjectRoot(), Err: r.Err}
	}
	if r.path == "" {
		return cdOutcome{Root: w.ProjectRoot()}
	}
	if r.NeedsConfirm && !confirmed {
		return cdOutcome{Root: w.ProjectRoot()}
	}
	before := w.ProjectRoot()
	if err := w.SetProjectRoot(r.path); err != nil {
		return cdOutcome{Root: before, Err: err}
	}
	return cdOutcome{Changed: w.ProjectRoot() != before, Root: w.ProjectRoot()}
}

// rootPersister remembers the root for the next run (M15). It is an interface
// so cdCommand and its tests stay free of the state package.
type rootPersister interface {
	SetProjectRoot(root string) error
}

// persistRoot saves the new root, but never fails the switch.
//
// Order matters: the workspace is already changed when this runs, and a state
// write that errors must NOT roll the session back to the old folder — that
// would leave the operator believing a /cd that visibly worked did not happen.
// The cost of failing to persist is one lost convenience, not a lying prompt.
func persistRoot(p rootPersister, root string) {
	if p == nil || root == "" {
		return
	}
	_ = p.SetProjectRoot(root)
}

// rest of the session, recorded so the UI can show it.
//
// This exists because the shell cannot be confined by path — the honest answer
// to "the agent must ask before writing elsewhere" is a narrow, visible grant,
// not a promise the shell cannot keep.
func grantCommand(w *workspace.Workspace, arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		g := w.Grants()
		if len(g) == 0 {
			return "nenhuma concessao ativa", nil
		}
		return "escrita concedida em: " + strings.Join(g, ", "), nil
	}
	if err := w.CanUse(arg); err != nil {
		return err.Error(), err
	}
	dir, err := workspace.ResolveDir(arg)
	if err != nil {
		return err.Error(), err
	}
	if err := w.Grant(dir); err != nil {
		return err.Error(), err
	}
	return fmt.Sprintf("escrita concedida em %s (ate o fim da sessao)", dir), nil
}
