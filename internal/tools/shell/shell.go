// Package shell implements the shell tool — the single, most sensitive path
// to arbitrary command execution (SPEC §5.3).
//
// It is never enabled by default and, when enabled, every execution requires
// the ask policy (validated at the configuration level). Before anything runs,
// the raw command is scanned against a hard-block list and against command
// chaining (; && || | backticks $()), which are denied by default.
package shell

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"ancientai/internal/tools"
)

// maxCmdBytes bounds the size of the command string.
const maxCmdBytes = 16 << 10 // 16 KiB

// Tool is the shell tool.
type Tool struct {
	limit int
}

// New returns the shell tool. limit bounds output bytes (SPEC §5.2).
func New(limit int) tools.Tool {
	if limit <= 0 {
		limit = 65536
	}
	return Tool{limit: limit}
}

func (Tool) Name() string { return "shell" }
func (Tool) Description() string {
	return "Executa um unico comando shell apos aprovacao explicita. Arg 'cmd' e a linha de comando."
}
func (Tool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"],"additionalProperties":false}`)
}

// isWhitespaceNormalized strips every whitespace character so pattern matches
// survive arbitrary quoting and line breaks (SPEC §5.3: hard-block must be
// independent of quoting/chaining syntax).
func normalize(cmd string) string {
	var sb strings.Builder
	for _, r := range cmd {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\'' || r == '"' || r == '\\' {
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// chainingRe matches the command separators that are denied by default.
var chainingRe = regexp.MustCompile(`[;|&]|\n|\$\(|` + "`")

// hardBlockPatterns are destructive commands that are never allowed, even if
// the rest of the command looks harmless. They are matched on the
// whitespace-stripped form of the command.
var hardBlockPatterns = []*regexp.Regexp{
	regexp.MustCompile(`rm-rf/`),
	regexp.MustCompile(`rm-fr/`),
	regexp.MustCompile(`rm-rf\*`),
	regexp.MustCompile(`of=/dev/`),
	regexp.MustCompile(`<>(?i)dev`),
	regexp.MustCompile(`(^|/)mkfs`),
	regexp.MustCompile(`dd.*of=/dev/`),
	regexp.MustCompile(`wipefs`),
	regexp.MustCompile(`fdisk`),
	regexp.MustCompile(`parted`),
	regexp.MustCompile(`sudo`),
	regexp.MustCompile(`chmod-?[rR]?777/?$`),
	regexp.MustCompile(`format.*/dev/`),
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

	norm := normalize(cmd)
	for _, re := range hardBlockPatterns {
		if re.MatchString(norm) {
			return tools.Result{}, fmt.Errorf("shell: command matches a hard-block pattern (%s) and is refused", re.String())
		}
	}
	if chainingRe.MatchString(cmd) {
		return tools.Result{}, errors.New("shell: multi-command chaining is denied by default")
	}

	proc := exec.CommandContext(ctx, "bash", "-c", cmd)
	buf := &limitedBuffer{limit: t.limit}
	proc.Stdout = buf
	proc.Stderr = buf
	start := time.Now()
	err := proc.Run()
	dur := time.Since(start)
	res := tools.Result{Duration: dur, ExitCode: 0, Output: buf.String(), Truncated: buf.truncated}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		return tools.Result{}, err
	}
	return res, nil
}

// limitedBuffer caps the captured output while keeping the truncated flag.
type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		b.limit = 65536
	}
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
