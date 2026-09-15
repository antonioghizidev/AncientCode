package loop

// M5 security tests: docker subcommand whitelist, apt removal/injection
// refusal, secret redaction in docker logs and context-budget trimming, all
// verified end-to-end through the loop (SPEC §5.2/M5).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"ancientai/internal/config"
	"ancientai/internal/provider"
	"ancientai/internal/tools"
	"ancientai/internal/tools/apttools"
	"ancientai/internal/tools/docker"
	"ancientai/internal/tools/files"
)

func registryWithDockerApt(root string) *tools.Registry {
	reg := registryWithShell(root)
	if err := reg.Register(docker.New(0, 0)); err != nil {
		panic(err)
	}
	if err := reg.Register(apttools.New(0, 0)); err != nil {
		panic(err)
	}
	return reg
}

func m5RunConfig() *config.Config {
	cfg := runConfig()
	cfg.DockerTool = "enabled"
	cfg.AptTool = "enabled"
	cfg.Approval.Default = "ask"
	return cfg
}

func dockerCall(id string, sub string, args ...string) provider.CompletedToolCall {
	b, err := json.Marshal(map[string]any{"subcommand": sub, "args": args})
	if err != nil {
		panic(err)
	}
	return provider.CompletedToolCall{ID: id, Name: "docker", Args: string(b)}
}

func aptCall(id string, action string, pkgs ...string) provider.CompletedToolCall {
	b, err := json.Marshal(map[string]any{"action": action, "packages": pkgs})
	if err != nil {
		panic(err)
	}
	return provider.CompletedToolCall{ID: id, Name: "apt", Args: string(b)}
}

// Security: a docker subcommand outside the whitelist is refused at the tool
// layer even after the operator approves the call, and the refusal is audited.
func TestM5DockerDeniedSubcommandRecordedInAudit(t *testing.T) {
	for _, sub := range []string{"run", "exec", "push", "pull", "build", "compose", "rm"} {
		t.Run(sub, func(t *testing.T) {
			fp := &scriptedProvider{script: []provider.ChatResult{
				{ToolCalls: []provider.CompletedToolCall{dockerCall("c1", sub, "x")}},
			}}
			root := t.TempDir()
			l, auditPath, _ := newAuditLoop(t, fp, m5RunConfig(), registryWithDockerApt(root), wrapApprover(alwaysApprove))

			if _, err := l.Turn(context.Background(), "rode"); err == nil {
				t.Fatalf("subcommand %q must be refused", sub)
			}
			entries := readAudit(t, auditPath)
			if len(entries) == 0 {
				t.Fatal("no audit entry recorded")
			}
			last := entries[len(entries)-1]
			if last.Tool != "docker" || last.Status != "denied" {
				t.Fatalf("expected denied docker entry, got %+v", last)
			}
			if !strings.Contains(last.Args, sub) {
				t.Fatalf("audit args missing subcommand: %q", last.Args)
			}
		})
	}
}

// Security: apt removal is refused outright even when approved — only
// update/install are possible through the tool (SPEC §5.2).
func TestM5AptRemovalRefused(t *testing.T) {
	for _, action := range []string{"remove", "purge", "upgrade"} {
		t.Run(action, func(t *testing.T) {
			fp := &scriptedProvider{script: []provider.ChatResult{
				{ToolCalls: []provider.CompletedToolCall{aptCall("c1", action, "openssh-server")}},
			}}
			root := t.TempDir()
			l, auditPath, _ := newAuditLoop(t, fp, m5RunConfig(), registryWithDockerApt(root), wrapApprover(alwaysApprove))

			if _, err := l.Turn(context.Background(), "desinstale"); err == nil {
				t.Fatalf("action %q must be refused", action)
			}
			entries := readAudit(t, auditPath)
			last := entries[len(entries)-1]
			if last.Tool != "apt" || last.Status != "denied" {
				t.Fatalf("expected denied apt entry, got %+v", last)
			}
		})
	}
}

// Security: package names are validated so the model cannot smuggle apt
// options or paths into the install vector.
func TestM5AptNameInjectionRefused(t *testing.T) {
	for _, pkg := range []string{"-y", "../evil", "pkg;rm", "--assume-yes"} {
		t.Run(pkg, func(t *testing.T) {
			fp := &scriptedProvider{script: []provider.ChatResult{
				{ToolCalls: []provider.CompletedToolCall{aptCall("c1", "install", pkg)}},
			}}
			root := t.TempDir()
			l, auditPath, _ := newAuditLoop(t, fp, m5RunConfig(), registryWithDockerApt(root), wrapApprover(alwaysApprove))

			if _, err := l.Turn(context.Background(), "instale"); err == nil {
				t.Fatalf("package %q must be refused", pkg)
			}
			entries := readAudit(t, auditPath)
			if entries[len(entries)-1].Status != "denied" {
				t.Fatalf("expected denied entry, got %+v", entries[len(entries)-1])
			}
		})
	}
}

// Security: a secret echoed by docker logs must never reach the history fed
// back to the model (redacted by appendToolResult, SPEC §5.6.2). Requires a
// real docker under PATH, so it runs on Linux (target platform); skipped on
// Windows where fake executables aren't portable.
func TestM5DockerLogsRedactsSecret(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires PATH-injected binary; covered on Linux/CI and on the Inspiron")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "docker")
	script := "#!/bin/sh\nif [ \"$1\" = \"logs\" ]; then echo \"token=\"$SECRET\" leaked\"; exit 0; fi\necho unexpected >&2; exit 44\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SECRET", testSecret)

	fp := &scriptedProvider{repeat: -1, script: []provider.ChatResult{
		{ToolCalls: []provider.CompletedToolCall{dockerCall("c1", "logs", "ctr")}},
	}}
	l := newTestLoop(t, fp, m5RunConfig(), alwaysApprove, "")

	// docker is enable-gated by config and must be registered.
	fs := files.NewFiles(t.TempDir(), 0)
	reg := tools.NewRegistry()
	for _, tl := range []tools.Tool{fs.ToolReadFile(), fs.ToolWriteFile(), fs.ToolListDir()} {
		_ = reg.Register(tl)
	}
	if err := reg.Register(docker.New(0, 0)); err != nil {
		t.Fatal(err)
	}
	l.reg = reg

	if _, err := l.Turn(context.Background(), "veja os logs"); err != nil {
		t.Fatal(err)
	}

	// Second chat round receives the tool message; it must be redacted.
	if len(fp.gotMsgs) < 2 {
		t.Fatal("loop did not feed the tool result back")
	}
	joined := ""
	for _, m := range fp.gotMsgs[len(fp.gotMsgs)-1] {
		joined += m.Content
	}
	if strings.Contains(joined, testSecret) {
		t.Fatal("secret leaked from docker logs into the history")
	}
	if !strings.Contains(joined, "***") {
		t.Fatalf("expected redacted form in history, got: %q", joined)
	}
}

// Toolbox mapping: docker/apt always ask; they appear only in run mode and
// only when enabled in the config.
func TestM5PolicyAndToolboxForDockerApt(t *testing.T) {
	cfg := &config.Config{}
	l := &Loop{cfg: cfg}

	for _, name := range []string{"docker", "apt"} {
		if l.policyFor(name) != policyAsk {
			t.Fatalf("policyFor(%s) = %q, want ask", name, l.policyFor(name))
		}
	}
	if err := l.SetToolboxMode("plan"); err != nil {
		t.Fatal(err)
	}
	for _, name := range l.enabledTools() {
		if name == "docker" || name == "apt" {
			t.Fatalf("plan mode exposed %s", name)
		}
	}

	if err := l.SetToolboxMode("run"); err != nil {
		t.Fatal(err)
	}
	cfg.DockerTool = "enabled"
	cfg.AptTool = "enabled"
	if !l.toolEnabled("docker") || !l.toolEnabled("apt") {
		t.Fatalf("run mode missed docker/apt with config enabled")
	}

	cfg.DockerTool = "disabled"
	cfg.AptTool = "disabled"
	if l.toolEnabled("docker") || l.toolEnabled("apt") {
		t.Fatal("disabled docker/apt still enabled in run mode")
	}
}

// Context budget (session_context_bytes): old turns are dropped from what is
// sent to the provider, newest always survives, and the stored session stays
// intact.
func TestM5ContextBudgetDropsOldestTurns(t *testing.T) {
	cfg := runConfig()
	cfg.SessionCtxBytes = 512
	big := strings.Repeat("x", 2048)

	fp := &scriptedProvider{script: []provider.ChatResult{
		{FinalContent: "a1", FinishReason: "stop"},
		{FinalContent: "a2", FinishReason: "stop"},
	}}
	l := newTestLoop(t, fp, cfg, alwaysApprove, "")

	if _, err := l.Turn(context.Background(), "turno1 "+big); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Turn(context.Background(), "turno2"); err != nil {
		t.Fatal(err)
	}

	if len(fp.gotMsgs) < 2 {
		t.Fatal("loop did not run two chat rounds")
	}
	joined := ""
	for _, m := range fp.gotMsgs[1] {
		joined += m.Content + " "
	}
	if strings.Contains(joined, big) {
		t.Fatal("oldest turn was not trimmed by the context budget")
	}
	if !strings.Contains(joined, "turno2") {
		t.Fatalf("newest turn missing from trimmed history: %q", joined)
	}
}
