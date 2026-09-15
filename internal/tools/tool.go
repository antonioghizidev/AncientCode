// Package tools defines the tool contract and the registry (SPEC §3.3, §5.6).
//
// A Tool receives already parsed, validated arguments and returns a Result
// that the loop treats as untrusted data: it is always redacted, truncated and
// wrapped in [untrusted-tool-output] markers before it reaches the model.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"syscall"
	"time"

	"ancientai/internal/provider"
)

// MaxArgsBytes bounds the size of a single tool-call argument document. The
// model controls the arguments, so the size is clamped before any parsing.
const MaxArgsBytes = 256 << 10 // 256 KiB

// Result is the outcome of one tool execution.
type Result struct {
	Output    string
	Truncated bool
	ExitCode  int
	Duration  time.Duration
}

// DeniedError marks a tool-call refused by a security policy (hard-block,
// whitelist or chaining denial). The loop classifies it as status="denied"
// in the audit (SPEC §5.7) — a refusal, not a runtime error.
type DeniedError struct {
	Reason string
}

func (e *DeniedError) Error() string { return "denied: " + e.Reason }

// ExitCodeOf extracts the child process exit code from an *exec.ExitError.
// Returns ok=false when the error does not carry an exit status.
func ExitCodeOf(runErr error) (int, bool) {
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		if st, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return st.ExitStatus(), true
		}
	}
	return 0, false
}

// Tool is one callable instrument exposed to the model.
type Tool interface {
	// Name is the stable identifier sent to the model (lowercase snake_case).
	Name() string
	// Description is shown to the model.
	Description() string
	// Parameters is the JSON Schema document for the arguments.
	Parameters() json.RawMessage
	// Execute runs the tool for the given raw JSON argument document. It MUST
	// validate the arguments strictly before performing any action.
	Execute(ctx context.Context, args json.RawMessage) (Result, error)
}

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ValidateName enforces the strict tool-name grammar. A tool name coming from
// the model is never trusted until it passes here (SPEC §5.6.5).
func ValidateName(name string) error {
	if name == "" {
		return errors.New("tools: empty tool name")
	}
	if !nameRe.MatchString(name) {
		return fmt.Errorf("tools: invalid tool name %q (must match %s)", name, nameRe)
	}
	return nil
}

// Registry is a deterministic, append-only map of registered tools.
type Registry struct {
	tools map[string]Tool
	order []string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool. It fails closed on invalid or duplicate names.
func (r *Registry) Register(t Tool) error {
	if t == nil {
		return errors.New("tools: cannot register nil tool")
	}
	if err := ValidateName(t.Name()); err != nil {
		return err
	}
	if _, dup := r.tools[t.Name()]; dup {
		return fmt.Errorf("tools: tool %q already registered", t.Name())
	}
	r.tools[t.Name()] = t
	r.order = append(r.order, t.Name())
	return nil
}

// Has reports whether a tool name is registered.
func (r *Registry) Has(name string) bool {
	_, ok := r.tools[name]
	return ok
}

// Lookup returns a registered tool by name.
func (r *Registry) Lookup(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Names returns the registered tool names in registration order.
func (r *Registry) Names() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Defs renders the registered tools as provider.ToolDef payloads, ready to be
// sent to the model.
func (r *Registry) Defs() []provider.ToolDef {
	out := make([]provider.ToolDef, 0, len(r.order))
	for _, name := range r.order {
		t := r.tools[name]
		out = append(out, provider.NewToolDef(t.Name(), t.Description(), t.Parameters()))
	}
	return out
}

// Raw converts a JSON literal to json.RawMessage (mostly for tests and
// small literals); callers must ensure the string is valid JSON.
func Raw(s string) json.RawMessage { return json.RawMessage(s) }

// UnmarshalStrict decodes JSON into v, rejecting unknown fields and using
// native number precision. It is the mandatory entry point for tool-call
// arguments (SPEC §5.6.5): a document that does not match the schema exactly
// is an error, never a guess.
func UnmarshalStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("tools: invalid arguments: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("tools: invalid arguments: trailing data after JSON document")
	}
	return nil
}
