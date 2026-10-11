package workspace

// Ref is the live view of a Workspace that tools hold.
//
// Why an interface and not a struct copy: files.Files, code.Toolset and
// sandbox.Executor capture their root at CONSTRUCTION and none of them has a
// setter, while tools.Registry.Register refuses duplicates — so re-registering
// a tool with the new root is not an option. The root therefore has to be read
// from somewhere alive at the moment of use.
//
// A pointer is not enough on its own: passing *Workspace and calling ProjectRoot
// per tool call is fine, but the shell executor resolves proc.Dir once per
// command and the file tools resolve per call — two different cadences. Ref
// gives both the same accessor names, so a swap cannot be half-applied.
type Ref interface {
	// CodeRoot is where the file and code tools are confined. It FOLLOWS the
	// project root: there is one root, and two would let the agent read in one
	// place and write in another.
	CodeRoot() string
	// Workdir is the directory the shell process runs in.
	Workdir() string
	// Scratch is where throwaway output goes, ungated.
	Scratch() string
	// Writable reports whether a write may land on path without asking again.
	Writable(path string) bool
}

// ref is the concrete Ref backed by a Workspace.
type ref struct{ w *Workspace }

// NewRef returns the live view of w.
func NewRef(w *Workspace) Ref { return ref{w: w} }

func (r ref) CodeRoot() string { return r.w.ProjectRoot() }

func (r ref) Workdir() string { return r.w.ProjectRoot() }

func (r ref) Scratch() string { return r.w.Scratch() }

func (r ref) Writable(path string) bool { return r.w.Granted(path) }
