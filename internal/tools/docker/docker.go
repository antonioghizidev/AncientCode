// Package docker implements the docker operation tool (SPEC §5.2, M5).
//
// It is a thin, argument-vector wrapper around the docker CLI: the model
// supplies a subcommand from a fixed whitelist plus a JSON array of arguments.
// Model text is NEVER concatenated into a shell line — only argv positions of
// "docker <subcommand> <args...>" are filled, so quoting/expansion trickery has
// nothing to bite on. The executable "docker" is resolved from PATH.
package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"ancientai/internal/tools"
)

// execCommand is the process launcher — injectable in tests (helper-process
// pattern). Production code resolves the real docker binary from PATH.
var execCommand = exec.CommandContext

const (
	maxArgBytes = 1 << 10 // per-argument cap
	maxArgs     = 64      // max number of additional arguments
)

// allowedSubcommands is the closed whitelist (SPEC §5.2, M5). Everything else
// — run, exec, push, pull, build, commit, rm, network, volume, compose, etc. —
// is denied before reaching the CLI.
var allowedSubcommands = map[string]bool{
	"ps": true, "logs": true, "inspect": true, "stats": true, "top": true,
	"images": true, "port": true, "version": true, "info": true,
	"restart": true, "stop": true, "start": true,
}

// Tool runs docker subcommands with a fixed vector (never shell text).
type Tool struct {
	timeout time.Duration
	maxOut  int
}

// New builds a docker tool. timeout<=0 defaults to 60s, maxOut<=0 to 64KiB.
func New(timeout time.Duration, maxOut int) *Tool {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if maxOut <= 0 {
		maxOut = 65536
	}
	return &Tool{timeout: timeout, maxOut: maxOut}
}

func (Tool) Name() string { return "docker" }

func (Tool) Description() string {
	return "Opera containers (docker). Use subcomando + args como vetor de strings; nunca injete shell."
}

func (Tool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"subcommand":{"type":"string"},"args":{"type":"array","items":{"type":"string"}}},"required":["subcommand"],"additionalProperties":false}`)
}

// Execute validates subcommand/args against the whitelist and runs the docker
// binary as an argument vector. Denials of subcommand/meta are returned as
// *tools.DeniedError so the loop audits them as refusals.
func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
	var a struct {
		Subcommand string   `json:"subcommand"`
		Args       []string `json:"args"`
	}
	if err := tools.UnmarshalStrict(raw, &a); err != nil {
		return tools.Result{}, err
	}
	if a.Subcommand == "" {
		return tools.Result{}, errors.New("docker: subcommand is required")
	}
	if !allowedSubcommands[a.Subcommand] {
		return tools.Result{}, &tools.DeniedError{Reason: fmt.Sprintf("subcommand %q is outside the docker whitelist", a.Subcommand)}
	}
	if len(a.Args) > maxArgs {
		return tools.Result{}, fmt.Errorf("docker: at most %d args allowed", maxArgs)
	}
	for _, s := range a.Args {
		if len(s) > maxArgBytes {
			return tools.Result{}, errors.New("docker: argument exceeds the size limit")
		}
	}

	tctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	argv := append([]string{a.Subcommand}, a.Args...)
	cmd := execCommand(tctx, "docker", argv...)
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
		return tools.Result{}, fmt.Errorf("docker: exec: %w", runErr)
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
	if len(p) > room {
		b.buf = append(b.buf, p[:room]...)
		b.truncated = true
		return len(p), nil
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *limitedBuffer) String() string { return string(b.buf) }
