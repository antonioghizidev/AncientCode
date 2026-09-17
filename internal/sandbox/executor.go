package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// execCommand is the process launcher — injectable for tests (helper-process
// pattern) and replaced by production code with exec.CommandContext.
var execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// Result captures the outcome of an executed command (SPEC §5.2 camada 4).
// The SignedArgs field holds the exact vector that was approved and executed —
// it must not differ from what the operator saw (M3 frozen-vector guarantee).
type Result struct {
	ExitCode   int
	Output     string
	Truncated  bool
	Approved   bool
	SignedArgs string
	Duration   time.Duration
}

// Executor runs commands in a sandboxed context with timeout, output limit
// and workdir confinement (SPEC §5.2 camada 4).
type Executor struct {
	workdir string
	runUser string
	timeout time.Duration
	maxOut  int
}

// NewExecutor builds an executor.  If runUser is empty, commands run as the
// current user.  If timeout is zero, a default of 30s is applied.
func NewExecutor(workdir, runUser string, timeout time.Duration, maxOut int) *Executor {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if maxOut <= 0 {
		maxOut = 65536
	}
	return &Executor{workdir: workdir, runUser: runUser, timeout: timeout, maxOut: maxOut}
}

// Exec runs cmd via bash -c, potentially dropping privileges to runUser.
// approvedArgs is the exact argument string that was shown to the operator
// and approved — it is stored verbatim in the Result.
func (e *Executor) Exec(ctx context.Context, cmd string, approvedArgs string) (*Result, error) {
	// Deny-by-default: an explicit root target is a refused downgrade, never a
	// silent fallback to the current user (SPEC §5.2 camada 4).
	if runtime.GOOS == "linux" && e.runUser == "root" {
		return nil, &DeniedError{Reason: "run_user=root is not allowed"}
	}
	if e.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.timeout)
		defer cancel()
	}

	argv := e.buildCommand(cmd)
	proc := execCommand(ctx, argv[0], argv[1:]...)
	if e.workdir != "" {
		proc.Dir = e.workdir
	}
	return e.run(proc, approvedArgs)
}

// ExecVector runs an executable directly as an argument vector, WITHOUT any
// shell. This is the deliberate, non-injectable path used by skill_run
// (D-2026-09-16-39): the model-supplied argv never becomes a shell line. A
// run_user privilege drop is refused here (a vector cannot compose `su -c`
// without rejoining the argv into text); skill scripts run as the harness
// user — a run_user mismatch is a hard denial, never a silent downgrade.
func (e *Executor) ExecVector(ctx context.Context, prog string, argv []string, approvedArgs string) (*Result, error) {
	if runtime.GOOS == "linux" && e.runUser == "root" {
		return nil, &DeniedError{Reason: "run_user=root is not allowed"}
	}
	if runtime.GOOS == "linux" && e.runUser != "" && !e.isCurrentUser(e.runUser) {
		return nil, &DeniedError{
			Reason: "script de skill exige troca de run_user; execucao por vetor nao compoe 'su'; recusado (sem downgrade silencioso)",
		}
	}
	if e.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.timeout)
		defer cancel()
	}
	proc := execCommand(ctx, prog, argv...)
	if e.workdir != "" {
		proc.Dir = e.workdir
	}
	return e.run(proc, approvedArgs)
}

// run executes a prepared process with the output limit and result plumbing.
func (e *Executor) run(proc *exec.Cmd, approvedArgs string) (*Result, error) {
	buf := &limitedBuffer{limit: e.maxOut}
	proc.Stdout = buf
	proc.Stderr = buf

	start := time.Now()
	runErr := proc.Run()
	dur := time.Since(start)

	res := &Result{
		ExitCode:   0,
		Output:     buf.String(),
		Truncated:  buf.truncated,
		Approved:   true,
		SignedArgs: approvedArgs,
		Duration:   dur,
	}

	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			if st, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				res.ExitCode = st.ExitStatus()
			}
			return res, nil
		}
		return nil, fmt.Errorf("sandbox: exec failed: %w", runErr)
	}
	return res, nil
}

// buildCommand constructs the exec argv based on the platform and runUser.
// On Linux, if runUser differs from the current user, su is used to drop
// privileges (SPEC §5.2: the main process never runs as root). On Windows su
// is unavailable — commands run directly (tests use the injectable launcher).
func (e *Executor) buildCommand(cmd string) []string {
	if runtime.GOOS != "linux" || e.runUser == "" || e.isCurrentUser(e.runUser) || e.runUser == "root" {
		return []string{"bash", "-c", cmd}
	}
	return []string{"su", "-", e.runUser, "-c", cmd}
}

// isCurrentUser reports whether the given user is the current process user.
// On error it returns true so we avoid trying to switch unnecessarily.
func (e *Executor) isCurrentUser(user string) bool {
	out, err := execCommand(context.Background(), "id", "-un").Output()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) == user
}

// limitedBuffer caps output while keeping the truncated flag.
type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len() >= b.limit {
		b.truncated = true
		return len(p), nil
	}
	room := b.limit - b.buf.Len()
	if len(p) > room {
		b.buf.Write(p[:room])
		b.truncated = true
		return len(p), nil
	}
	b.buf.Write(p)
	return len(p), nil
}

func (b *limitedBuffer) String() string { return b.buf.String() }
