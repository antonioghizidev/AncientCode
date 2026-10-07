// Package skills implements the skill_run tool (M7, D-2026-09-16-39): it runs
// a script of an active skill strictly inside its scripts/ directory, as an
// argument vector through the sandbox executor — never via a shell. Skills are
// untrusted third-party content, so skill_run is always an ask-gated, audited
// mutation that can only execute what the operator verified and approved.
package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/antonioghizidev/ancientcode/internal/sandbox"
	"github.com/antonioghizidev/ancientcode/internal/skills"
	"github.com/antonioghizidev/ancientcode/internal/tools"
)

const (
	// maxArgvCount bounds the number of model-supplied arguments.
	maxArgvCount = 64
	// maxArgvTotal bounds the total argument bytes.
	maxArgvTotal = 16 << 10 // 16 KiB
	// toolTimeout is the per-execution ceiling for skill scripts.
	toolTimeout = 60 * time.Second
)

// Tool runs skill scripts on behalf of the active skills.Set.
type Tool struct {
	set  *skills.Set
	exec *sandbox.Executor
}

// New returns the skill_run tool bound to a skill set and executor. outputMaxBytes
// bounds the captured output.
func New(set *skills.Set, exec *sandbox.Executor, outputMaxBytes int) tools.Tool {
	if set == nil {
		set = skills.NewSet()
	}
	if exec == nil {
		exec = sandbox.NewExecutor("", "", toolTimeout, outputMaxBytes)
	}
	return Tool{set: set, exec: exec}
}

func (Tool) Name() string { return "skill_run" }
func (Tool) Description() string {
	return "Executa um script de uma skill ativa apos aprovacao explicita. Arg 'skill' nomeia a skill, 'script' o arquivo em scripts/, 'argv' a lista de argumentos."
}
func (Tool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"skill":{"type":"string"},"script":{"type":"string"},"argv":{"type":"array","items":{"type":"string"}}},"required":["skill","script"],"additionalProperties":false}`)
}

// Execute validates the request against the active skill set and the script
// confinement, then runs the script as a pure argument vector. Any violation
// is a DeniedError (audited as status=denied, fail-closed).
func (t Tool) Execute(ctx context.Context, args json.RawMessage) (tools.Result, error) {
	var a struct {
		Skill  string   `json:"skill"`
		Script string   `json:"script"`
		Argv   []string `json:"argv"`
	}
	if err := tools.UnmarshalStrict(args, &a); err != nil {
		return tools.Result{}, err
	}
	if a.Skill == "" || a.Script == "" {
		return tools.Result{}, errors.New("skill_run: 'skill' e 'script' sao obrigatorios")
	}
	if len(a.Argv) > maxArgvCount {
		return tools.Result{}, t.deny(fmt.Errorf("skill_run: argv excede %d argumentos", maxArgvCount))
	}
	total := 0
	for _, arg := range a.Argv {
		total += len(arg)
		if total > maxArgvTotal {
			return tools.Result{}, t.deny(fmt.Errorf("skill_run: argv total excede %d bytes", maxArgvTotal))
		}
	}

	sk, ok := t.set.Get(a.Skill)
	if !ok {
		return tools.Result{}, t.deny(fmt.Errorf("skill_run: skill %q nao esta ativa na sessao", a.Skill))
	}
	if len(sk.ScriptNames) == 0 {
		return tools.Result{}, t.deny(fmt.Errorf("skill_run: skill %q nao possui scripts", a.Skill))
	}
	// Content that changed since operator verification must not run (D-40).
	if err := sk.VerifyScript(a.Script); err != nil {
		return tools.Result{}, t.deny(err)
	}
	path, err := sk.ScriptPath(a.Script)
	if err != nil {
		return tools.Result{}, t.deny(err)
	}

	// The approved vector is frozen verbatim for the audit trail (frozen args).
	approved := string(args)
	res, err := t.exec.ExecVector(ctx, path, a.Argv, approved)
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{
		ExitCode:  res.ExitCode,
		Output:    res.Output,
		Truncated: res.Truncated,
		Duration:  res.Duration,
	}, nil
}

// deny converts a denial into a tools.DeniedError so the loop audits it as
// status=denied, never as a runtime error.
func (t Tool) deny(err error) error {
	var de *sandbox.DeniedError
	if errors.As(err, &de) {
		return &tools.DeniedError{Reason: de.Reason}
	}
	return &tools.DeniedError{Reason: err.Error()}
}
