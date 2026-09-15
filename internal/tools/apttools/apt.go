// Package apttools implements the apt package-manager tool (SPEC §5.2, M5).
//
// Wraps apt-get as an argument vector. Allowed actions update/install; any
// removal/upgrade/downgrade action is refused outright (DeniedError) — the
// mutating surface stays minimal and every install is gated by a human ask at
// the loop level. Package names are validated against a strict charset so the
// model can never inject apt options or paths.
package apttools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"time"

	"ancientai/internal/tools"
)

// execCommand is injectable in tests (helper-process pattern).
var execCommand = exec.CommandContext

var packageNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9.+_-]*$`)

const (
	maxPackages = 64         // per-call package count
	maxPkgLen   = 128        // per-name cap
	aptBinary   = "apt-get"  // use the scriptable frontend
	maxOutput   = 128 * 1024 // keep a diagnostic tail
)

// allowedActions is the closed set of accepted package-manager operations.
const (
	actionUpdate  = "update"
	actionInstall = "install"
)

var allowedActions = map[string]bool{actionUpdate: true, actionInstall: true}

// Tool runs apt-get actions with a strict vector (never shell text).
type Tool struct {
	timeout time.Duration
	maxOut  int
}

// New builds an apt tool. timeout<=0 defaults to 180s, maxOut<=0 to 128KiB.
func New(timeout time.Duration, maxOut int) *Tool {
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	if maxOut <= 0 {
		maxOut = maxOutput
	}
	return &Tool{timeout: timeout, maxOut: maxOut}
}

func (Tool) Name() string { return "apt" }

func (Tool) Description() string {
	return "Gerencia pacotes (apt). Acao update ou install; remocoes sao recusadas."
}

func (Tool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["update","install"]},"packages":{"type":"array","items":{"type":"string"}}},"required":["action"],"additionalProperties":false}`)
}

// Execute turns a validated action into an apt-get vector. Removal actions are
// refused before touching apt-get.
func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
	var a struct {
		Action   string   `json:"action"`
		Packages []string `json:"packages"`
	}
	if err := tools.UnmarshalStrict(raw, &a); err != nil {
		return tools.Result{}, err
	}
	if !allowedActions[a.Action] {
		return tools.Result{}, &tools.DeniedError{Reason: fmt.Sprintf("apt action %q is not allowed (only update/install)", a.Action)}
	}
	if a.Action == actionInstall {
		for _, p := range a.Packages {
			if len(p) > maxPkgLen || !packageNameRE.MatchString(p) {
				return tools.Result{}, &tools.DeniedError{Reason: fmt.Sprintf("apt: invalid package name %q", p)}
			}
		}
		if len(a.Packages) == 0 {
			return tools.Result{}, errors.New("apt: install requires at least one package")
		}
		if len(a.Packages) > maxPackages {
			return tools.Result{}, fmt.Errorf("apt: at most %d packages per call", maxPackages)
		}
	}

	tctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	// argv is a pure vector, e.g. apt-get install -y <pkg...> or apt-get update.
	var argv []string
	if a.Action == actionInstall {
		argv = append([]string{"install", "-y"}, a.Packages...)
	} else {
		argv = []string{"update"}
	}

	cmd := execCommand(tctx, aptBinary, argv...)
	buf := &limitedBuffer{limit: t.maxOut}
	cmd.Stdout = buf
	cmd.Stderr = buf

	start := time.Now()
	runErr := cmd.Run()
	dur := time.Since(start)

	res := tools.Result{
		Output:    buf.String(),
		Truncated: buf.truncated,
		Duration:  dur,
	}
	if runErr != nil {
		if code, ok := tools.ExitCodeOf(runErr); ok {
			res.ExitCode = code
			return res, nil
		}
		return tools.Result{}, fmt.Errorf("apt: exec: %w", runErr)
	}
	return res, nil
}

// limitedBuffer caps output while keeping the truncated flag.
type limitedBuffer struct {
	buf       []byte
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(b.buf) >= b.limit {
		b.truncated = true
		return len(p), nil
	}
	room := b.limit - len(b.buf)
	hasRoom := len(p) <= room
	if !hasRoom {
		b.buf = append(b.buf, p[:room]...)
		b.truncated = true
		return len(p), nil
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *limitedBuffer) String() string { return string(b.buf) }
