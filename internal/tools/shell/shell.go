// Package shell implements the shell tool — the single, most sensitive path
// to arbitrary command execution (SPEC §5.3).
//
// It is never enabled by default and, when enabled, every execution requires
// the ask policy (validated at the configuration level).  Hard-block and
// chaining checks are delegated to sandbox.Policy; execution is delegated to
// sandbox.Executor, which enforces timeout, output limits, workdir confinement
// and run_user privilege drop (SPEC §5.2 camadas 2 e 4).
package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"ancientai/internal/sandbox"
	"ancientai/internal/tools"
)

// maxCmdBytes bounds the size of the command string.
const maxCmdBytes = 16 << 10 // 16 KiB

// Tool is the shell tool.
type Tool struct {
	policy     *sandbox.Policy
	exec       *sandbox.Executor
	timeoutSec int // per-tool timeout in seconds (from config)
}

// New returns the shell tool using the given policy and executor.
func New(policy *sandbox.Policy, exec *sandbox.Executor, outputMaxBytes int) tools.Tool {
	if policy == nil {
		policy = sandbox.NewPolicy(sandbox.DefaultAllowed)
	}
	if exec == nil {
		exec = sandbox.NewExecutor("", "", 30, outputMaxBytes)
	}
	return Tool{policy: policy, exec: exec}
}

// NewSimple returns a shell tool with the default sandbox policy and a direct
// executor (no user isolation).  Useful for tests and quick wiring.
func NewSimple(outputMaxBytes int) tools.Tool {
	return New(nil, nil, outputMaxBytes)
}

func (Tool) Name() string { return "shell" }
func (Tool) Description() string {
	return "Executa um unico comando shell apos aprovacao explicita. Arg 'cmd' e a linha de comando."
}
func (Tool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"],"additionalProperties":false}`)
}

func (t Tool) Execute(ctx context.Context, args json.RawMessage) (tools.Result, error) {
	var a struct {
		Cmd string `json:"cmd"`
	}
	if err := tools.UnmarshalStrict(args, &a); err != nil {
		return tools.Result{}, err
	}
	cmd := a.Cmd
	if strings.TrimSpace(cmd) == "" {
		return tools.Result{}, errors.New("shell: cmd is required")
	}
	if len(cmd) > maxCmdBytes {
		return tools.Result{}, fmt.Errorf("shell: cmd exceeds the %d byte limit", maxCmdBytes)
	}

	// Camada 2: hard-block + chaining (sandbox.Policy).
	if err := t.policy.AllowCommand(cmd); err != nil {
		return tools.Result{}, t.deny(err)
	}

	// Camada 2: whitelist — resolve o executável real (primeiro token) e exige
	// que esteja na lista; comandos desconhecidos são negados (deny-by-default).
	// O texto vem do modelo, por isso o token é apenas o ponto de partida para
	// o check — o vetor executado é fixado antes do bash -c (M3).
	exe, _, _ := strings.Cut(strings.TrimSpace(cmd), " ")
	if err := t.policy.AllowExecutable(exe); err != nil {
		return tools.Result{}, t.deny(err)
	}

	// Camada 4: execute with timeout, output limit, workdir and run_user.
	sr, err := t.exec.Exec(ctx, cmd, cmd)
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{
		ExitCode:  sr.ExitCode,
		Output:    sr.Output,
		Truncated: sr.Truncated,
		Duration:  sr.Duration,
	}, nil
}

// deny converts a sandbox policy denial into a tools.DeniedError so the loop
// audits it as a refuse (status=denied), never as a runtime error.
func (t Tool) deny(err error) error {
	var de *sandbox.DeniedError
	if errors.As(err, &de) {
		return &tools.DeniedError{Reason: de.Reason}
	}
	return err
}
