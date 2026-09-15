package docker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"ancientai/internal/tools"
)

func TestDockerPSVector(t *testing.T) {
	var gotName string
	var gotArgs []string
	capture := func(_ context.Context, name string, args ...string) *exec.Cmd {
		gotName, gotArgs = name, args
		return helperCmd(t)
	}
	orig := execCommand
	execCommand = capture
	t.Cleanup(func() { execCommand = orig })

	if _, err := New(0, 0).Execute(context.Background(), tools.Raw(`{"subcommand":"ps","args":["-a","--format","json"]}`)); err != nil {
		t.Fatal(err)
	}
	if gotName != "docker" {
		t.Fatalf("binary name = %q, want docker", gotName)
	}
	want := []string{"ps", "-a", "--format", "json"}
	if strings.Join(gotArgs, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", gotArgs, want)
	}
}

// helperCmd builds a self-calling test binary that exits 0 (helper-process
// pattern): the injected launcher records argv but the actual process lives and
// dies inside the test executable, so the test runs on any platform.
func helperCmd(t *testing.T) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test binary: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=TestDockerHelperProcess")
	cmd.Env = append(os.Environ(), "GO_WANT_DOCKER_HELPER=1")
	return cmd
}

// TestDockerHelperProcess is the re-executed helper: it does not run as a test,
// it just exits 0 so the tool observes a successful (empty) run.
func TestDockerHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_DOCKER_HELPER") != "1" {
		t.Skip("helper process")
	}
}

func TestDockerSubcommandWhitelist(t *testing.T) {
	for _, sub := range []string{"run", "exec", "push", "pull", "build", "commit", "rm", "rmi", "network", "volume", "compose", "attach", "cp", "create", "tag", "save", "load", "login"} {
		_, err := New(0, 0).Execute(context.Background(), tools.Raw(`{"subcommand":"`+sub+`","args":["x"]}`))
		var de *tools.DeniedError
		if !errors.As(err, &de) {
			t.Errorf("subcommand %q not denied (err=%v)", sub, err)
		}
	}
}

func TestDockerAllowedSubcommandsReachCLI(t *testing.T) {
	for _, sub := range []string{"ps", "logs", "inspect", "restart", "stop", "start", "stats", "top", "images", "version", "info"} {
		if !allowedSubcommands[sub] {
			t.Errorf("subcommand %q should be allowed", sub)
		}
	}
}

func TestDockerStrictArgs(t *testing.T) {
	for _, args := range []string{
		`{"subcommand":"ps","extra":1}`,
		`{"subcommand":"ps","args":"-a"}`,
		`{"subcommand":123}`,
		`{"subcommand":"ps"} trailing`,
	} {
		if _, err := New(0, 0).Execute(context.Background(), tools.Raw(args)); err == nil {
			t.Errorf("strict args accepted: %s", args)
		}
	}
}

func TestDockerArgLimits(t *testing.T) {
	if _, err := New(0, 0).Execute(context.Background(), tools.Raw(`{"subcommand":"ps","args":[`+strings.Repeat(`"a",`, maxArgs+1)+`"b"]}`)); err == nil {
		t.Fatal("too many args accepted")
	}
	big := strings.Repeat("x", maxArgBytes+1)
	if _, err := New(0, 0).Execute(context.Background(), tools.Raw(`{"subcommand":"ps","args":["`+big+`"]}`)); err == nil {
		t.Fatal("oversized arg accepted")
	}
}

// PATH-injected fake docker binary (SPEC §5 test): create a real executable
// named "docker" and assert the tool resolves it from PATH (Linux/CI).
func TestDockerResolvesFromPATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executable via PATH is not portable on windows; covered by helper tests")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "docker")
	script := "#!/bin/sh\nif [ \"$1\" = \"ps\" ] && [ \"$2\" = \"-a\" ]; then echo CONTAINER1; exit 0; fi\necho unexpected >&2; exit 42\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	dt := New(5*time.Second, 1024)
	res, err := dt.Execute(context.Background(), tools.Raw(`{"subcommand":"ps","args":["-a"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "CONTAINER1") {
		t.Fatalf("output = %q, want CONTAINER1", res.Output)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d", res.ExitCode)
	}
}

// Denied subcommands must not touch the CLI at all (fail fast).
func TestDockerDeniedDoesNotExec(t *testing.T) {
	hit := false
	orig := execCommand
	execCommand = func(_ context.Context, _ string, _ ...string) *exec.Cmd { hit = true; return nil }
	t.Cleanup(func() { execCommand = orig })

	if _, err := New(0, 0).Execute(context.Background(), tools.Raw(`{"subcommand":"push","args":["img"]}`)); err == nil {
		t.Fatal("push not denied")
	}
	if hit {
		t.Fatal("CLI was executed for a denied subcommand")
	}
}
