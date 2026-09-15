package loop

// M4 security tests: sandbox hard-block, chaining and audit redaction
// verified end-to-end through the loop (SPEC §5.2/§5.7).

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"ancientai/internal/audit"
	"ancientai/internal/config"
	"ancientai/internal/provider"
	"ancientai/internal/tools"
	"ancientai/internal/tools/files"
	"ancientai/internal/tools/shell"
)

// registryWithShell builds a registry with the filesystem tools plus the shell
// tool (for the M3/M4 execution-layer security tests).
func registryWithShell(root string) *tools.Registry {
	fs := files.NewFiles(root, 65536)
	reg := tools.NewRegistry()
	for _, tl := range []tools.Tool{fs.ToolReadFile(), fs.ToolWriteFile(), fs.ToolListDir()} {
		if err := reg.Register(tl); err != nil {
			panic(err)
		}
	}
	if err := reg.Register(shell.NewSimple(65536)); err != nil {
		panic(err)
	}
	return reg
}

// shellCallArgs encodes a shell command as the tool's JSON argument document.
func shellCallArgs(cmd string) string {
	b, err := json.Marshal(map[string]string{"cmd": cmd})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func shellCall(id, cmd string) provider.CompletedToolCall {
	return provider.CompletedToolCall{ID: id, Name: "shell", Args: shellCallArgs(cmd)}
}

func newAuditLoop(t *testing.T, fp *scriptedProvider, cfg *config.Config, reg *tools.Registry, appr Approver) (*Loop, string, *audit.Recorder) {
	t.Helper()
	if cfg == nil {
		cfg = &config.Config{}
	}
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	rec, err := audit.NewRecorder(auditPath, 1<<20, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rec.Close() })
	l := newTestLoopA(t, fp, cfg, appr, "")
	l.reg = reg
	l.SetAudit(rec)
	return l, auditPath, rec
}

func readAudit(t *testing.T, path string) []audit.Entry {
	t.Helper()
	entries, err := audit.ReadEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// Security: "rm -rf /" is refused at the policy layer and, if it ever reaches
// the loop approved, the refusal is still recorded in the audit.
func TestM4ShellHardBlockRecordedInAudit(t *testing.T) {
	root := t.TempDir()
	cfg := runConfig()
	cfg.ShellTool = "enabled"
	fp := &scriptedProvider{script: []provider.ChatResult{
		{ToolCalls: []provider.CompletedToolCall{shellCall("c1", "rm -rf /")}},
	}}
	l, auditPath, _ := newAuditLoop(t, fp, cfg, registryWithShell(root), wrapApprover(alwaysApprove))

	if _, err := l.Turn(context.Background(), "destrua"); err == nil {
		t.Fatal("rm -rf / must be refused")
	}

	entries := readAudit(t, auditPath)
	if len(entries) == 0 {
		t.Fatal("no audit entry recorded")
	}
	last := entries[len(entries)-1]
	if last.Tool != "shell" || last.Status != "denied" {
		t.Fatalf("expected denied shell entry, got %+v", last)
	}
	if !strings.Contains(last.Args, "rm -rf /") {
		t.Fatalf("audit args missing command: %q", last.Args)
	}
}

// Security: hard-block survives arbitrary quoting — the policy matches on the
// normalized form, not on the raw text (SPEC §5.2).
func TestM4HardBlockQuotedVariants(t *testing.T) {
	root := t.TempDir()
	cfg := runConfig()
	cfg.ShellTool = "enabled"
	for _, tc := range []struct {
		label string
		cmd   string
	}{
		{"plain", `rm -rf /`},
		{"quoted", `rm -rf "/"`},
		{"space-flags", `rm -r -f /`},
		{"fr-order", `rm -fr /`},
		{"root", `rm -rf /etc`},
		{"dd-of-dev", `dd if=/dev/zero of=/dev/sda bs=4M`},
		{"mkfs", `mkfs.ext4 /dev/sdb1`},
		{"wipefs", `wipefs -a /dev/sda`},
		{"fdisk", `fdisk /dev/sda`},
		{"parted", `parted /dev/sda mklabel gpt`},
		{"sudo", `sudo apt update`},
		{"chmod-recursive", `chmod -R 777 /`},
		{"chmod-plain", `chmod 777 /`},
	} {
		t.Run(tc.label, func(t *testing.T) {
			fp := &scriptedProvider{script: []provider.ChatResult{
				{ToolCalls: []provider.CompletedToolCall{shellCall("c1", tc.cmd)}},
			}}
			l, _, _ := newAuditLoop(t, fp, cfg, registryWithShell(root), wrapApprover(alwaysApprove))
			if _, err := l.Turn(context.Background(), "destrua"); err == nil {
				t.Fatalf("expected hard-block for %q", tc.cmd)
			}
		})
	}
}

// Security: chaining (; && || | & newline $() `) is denied even when the
// individual command would be allowed (SPEC §5.2 camada 2).
func TestM4ChainingDenied(t *testing.T) {
	root := t.TempDir()
	cfg := runConfig()
	cfg.ShellTool = "enabled"
	// Security: chaining (; && || | & newline $() `) is denied even when the
	// individual command would be allowed (SPEC §5.2 camada 2).
	for _, tc := range []struct {
		label string
		cmd   string
	}{
		{"semicolon", "ls; cat /etc/passwd"},
		{"ampersand", "ls & cat /etc/passwd"},
		{"pipe", "cat /etc/passwd | grep root"},
		{"andand", "ls && cat /etc/passwd"},
		{"oror", "ls || cat /etc/passwd"},
		{"dollar-paren", "echo $(whoami)"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			fp := &scriptedProvider{script: []provider.ChatResult{
				{ToolCalls: []provider.CompletedToolCall{shellCall("c1", tc.cmd)}},
			}}
			l, _, _ := newAuditLoop(t, fp, cfg, registryWithShell(root), wrapApprover(alwaysApprove))
			if _, err := l.Turn(context.Background(), "encadeie"); err == nil {
				t.Fatalf("expected chaining denial for %q", tc.cmd)
			}
		})
	}
}

// Security: executable outside the whitelist is refused even when approved by
// the operator (deny-by-default — camada 2).
func TestM4WhitelistDeniesUnknownExecutable(t *testing.T) {
	root := t.TempDir()
	cfg := runConfig()
	cfg.ShellTool = "enabled"
	fp := &scriptedProvider{script: []provider.ChatResult{
		{ToolCalls: []provider.CompletedToolCall{shellCall("c1", "nc -l -p 4444")}},
	}}
	l, _, _ := newAuditLoop(t, fp, cfg, registryWithShell(root), wrapApprover(alwaysApprove))
	if _, err := l.Turn(context.Background(), "abra porta"); err == nil {
		t.Fatal("nc must be refused by the whitelist")
	}
}

// Security: an audit entry never stores a secret in clear — args that trigger
// the exfiltration scan are recorded already redacted (SPEC §5.7).
func TestM4AuditRedactsSecretInArgs(t *testing.T) {
	root := t.TempDir()
	cfg := runConfig()
	secretPath := testSecret + "/x.txt"
	fp := &scriptedProvider{script: []provider.ChatResult{
		{ToolCalls: []provider.CompletedToolCall{{ID: "c1", Name: "read_file",
			Args: `{"path":"` + secretPath + `"}`}}},
	}}
	l, auditPath, _ := newAuditLoop(t, fp, cfg, registryWithShell(root), wrapApprover(alwaysApprove))
	if _, err := l.Turn(context.Background(), "leia"); err == nil {
		t.Fatal("secret in args must be refused by the exfiltration scan")
	}

	entries := readAudit(t, auditPath)
	if len(entries) == 0 {
		t.Fatal("no audit entry recorded")
	}
	last := entries[len(entries)-1]
	if last.Status != "denied" {
		t.Fatalf("expected denied entry, got %+v", last)
	}
	if strings.Contains(last.Args, testSecret) {
		t.Fatalf("audit leaked secret in args: %q", last.Args)
	}
	raw, _ := json.Marshal(last)
	if strings.Contains(string(raw), testSecret) {
		t.Fatal("audit entry contains the raw secret")
	}
}
