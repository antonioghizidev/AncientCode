package sandbox

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	helperEnv   = "GO_WANT_SANDBOX_HELPER"
	behaviorEnv = "GO_SANDBOX_BEHAVIOR"
)

// fakeExec returns a launcher that spawns this same test binary as a helper
// process, so executor behavior is testable without a working shell.
func fakeExec(t *testing.T, behavior string) func(ctx context.Context, name string, args ...string) *exec.Cmd {
	t.Helper()
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		helper, err := os.Executable()
		if err != nil {
			panic(err)
		}
		cmd := exec.CommandContext(ctx, helper, "-test.run=TestSandboxHelperProcess")
		cmd.Env = append(os.Environ(), helperEnv+"=1", behaviorEnv+"="+behavior)
		return cmd
	}
}

// TestSandboxHelperProcess is not a real test: it is the child executed by
// fakeExec via the helper-process pattern, picked by the env sentinel.
func TestSandboxHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		return
	}
	behavior := os.Getenv(behaviorEnv)
	switch behavior {
	case "echo":
		if len(os.Args) > 0 && false {
			_ = os.Args // keep args referenced for future behaviors
		}
		println("hello")
	case "sleep":
		time.Sleep(5 * time.Second)
	case "long":
		println(strings.Repeat("x", 1000))
	case "exit42":
		os.Exit(42)
	case "pwd":
		wd, err := os.Getwd()
		if err != nil {
			os.Exit(2)
		}
		println(wd)
	case "env":
		for _, e := range os.Environ() {
			if strings.HasPrefix(e, "SANDBOX_") {
				println(e)
			}
		}
	}
	os.Exit(0)
}

func withFake(t *testing.T, behavior string) *Executor {
	t.Helper()
	// A generous timeout gives the helper-process child (a full go-test binary,
	// slower under -race) room to boot; timeout semantics are tested separately
	// in TestExecTimeoutKillsProcess with its own short deadline.
	ex := NewExecutor(t.TempDir(), "", 15*time.Second, 1024)
	orig := execCommand
	execCommand = fakeExec(t, behavior)
	t.Cleanup(func() { execCommand = orig })
	return ex
}

func TestExecBasic(t *testing.T) {
	ex := withFake(t, "echo")
	res, err := ex.Exec(context.Background(), "echo hello", `{"cmd":"echo hello"}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d", res.ExitCode)
	}
	if !res.Approved {
		t.Fatal("result not marked approved")
	}
	if res.SignedArgs != `{"cmd":"echo hello"}` {
		t.Fatalf("SignedArgs = %q", res.SignedArgs)
	}
}

func TestExecVectorRunsPureVector(t *testing.T) {
	ex := NewExecutor(t.TempDir(), "", 15*time.Second, 1024)
	var gotName string
	var gotArgs []string
	orig := execCommand
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotName, gotArgs = name, append([]string{}, args...)
		return fakeExec(t, "echo")(ctx, name, args...)
	}
	t.Cleanup(func() { execCommand = orig })

	res, err := ex.ExecVector(context.Background(), "/skills/x/scripts/deploy.sh",
		[]string{"--env", "prod", "task"}, `{"skill":"x","script":"deploy.sh","argv":["--env","prod","task"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if gotName != "/skills/x/scripts/deploy.sh" {
		t.Fatalf("prog = %q", gotName)
	}
	if len(gotArgs) != 3 || gotArgs[0] != "--env" || gotArgs[1] != "prod" || gotArgs[2] != "task" {
		t.Fatalf("argv = %v", gotArgs)
	}
	if res.ExitCode != 0 || !res.Approved {
		t.Fatalf("result = %+v", res)
	}
	if res.SignedArgs != `{"skill":"x","script":"deploy.sh","argv":["--env","prod","task"]}` {
		t.Fatalf("SignedArgs = %q", res.SignedArgs)
	}
	// The approved vector matches execution exactly: frozen guarantee.
	if !strings.Contains(res.SignedArgs, "deploy.sh") {
		t.Fatal("approved args missing script reference")
	}
}

func TestExecVectorRefusesRunUserSwitchOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("run_user switch semantics are Linux-only")
	}
	ex := NewExecutor(t.TempDir(), "somebody-else", time.Second, 1024)
	ex.runUser = "somebody-else"
	_, err := ex.ExecVector(context.Background(), "/s/x.sh", []string{"a"}, `{"skill":"x"}`)
	var de *DeniedError
	if !errors.As(err, &de) {
		t.Fatalf("expected DeniedError, got %v", err)
	}
	if !strings.Contains(de.Reason, "run_user") {
		t.Fatalf("denial reason = %q", de.Reason)
	}
}

func TestExecTimeoutKillsProcess(t *testing.T) {
	// This test owns its deadline: a 1s budget vs a child that sleeps 5s, so a
	// kill must fire regardless of spawn boot latency.
	ex := NewExecutor(t.TempDir(), "", time.Second, 1024)
	orig := execCommand
	execCommand = fakeExec(t, "sleep")
	t.Cleanup(func() { execCommand = orig })
	start := time.Now()
	res, err := ex.Exec(context.Background(), "sleep 5", `{"cmd":"sleep 5"}`)
	dur := time.Since(start)
	if err != nil {
		t.Fatalf("timeout should not surface as error, got %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	// On POSIX a killed process exits with signal — never 0.
	if res.ExitCode == 0 {
		t.Fatal("process claimed success after timeout")
	}
	if dur > 2*time.Second {
		t.Fatalf("timeout did not kill process: took %v", dur)
	}
}

func TestExecOutputTruncated(t *testing.T) {
	ex := NewExecutor(t.TempDir(), "", time.Second, 64)
	orig := execCommand
	execCommand = fakeExec(t, "long")
	t.Cleanup(func() { execCommand = orig })
	res, err := ex.Exec(context.Background(), "long", `{"cmd":"long"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated {
		t.Fatal("expected truncated output")
	}
	if len(res.Output) > 64 {
		t.Fatalf("output exceeds limit: %d bytes", len(res.Output))
	}
}

func TestExecSignedArgsFrozen(t *testing.T) {
	ex := withFake(t, "echo")
	approved := `{"cmd":"echo frozen"}`
	res, err := ex.Exec(context.Background(), "echo frozen", approved)
	if err != nil {
		t.Fatal(err)
	}
	if res.SignedArgs != approved {
		t.Fatalf("SignedArgs differs from approved: %q", approved)
	}
}

func TestExecWorkdirConfinement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper pwd relies on POSIX path expectations")
	}
	// workdir is a temp dir; the helper must print THAT dir, not the test cwd.
	workdir := "/tmp/sandbox-test"
	if err := os.MkdirAll(workdir, 0o700); err != nil {
		t.Skipf("cannot create /tmp dir: %v", err)
	}
	ex := NewExecutor(workdir, "", time.Second, 1024)
	orig := execCommand
	execCommand = fakeExec(t, "pwd")
	t.Cleanup(func() { execCommand = orig })
	res, err := ex.Exec(context.Background(), "pwd", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "sandbox-test") {
		t.Fatalf("pwd output %q does not match workdir", res.Output)
	}
}

func TestExecExitCodeNonZero(t *testing.T) {
	ex := withFake(t, "exit42")
	res, err := ex.Exec(context.Background(), "exit 42", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 42 {
		t.Fatalf("exit code = %d, want 42", res.ExitCode)
	}
}

// Security: a Linux sandbox configured to run as root refuses to execute —
// the user-isolation layer must never silently degrade to the current user.
func TestExecRunAsRootDenied(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("root refusal only applies on linux")
	}
	ex := NewExecutor("/tmp", "root", time.Second, 1024)
	_, err := ex.Exec(context.Background(), "echo hi", "")
	var de *DeniedError
	if !errors.As(err, &de) {
		t.Fatalf("expected *DeniedError for run_user=root, got %v", err)
	}
}

func TestExecDefaultTimeout(t *testing.T) {
	ex := NewExecutor(t.TempDir(), "", 0, 0)
	if ex.timeout != 30*time.Second {
		t.Fatalf("default timeout = %v", ex.timeout)
	}
	if ex.maxOut != 65536 {
		t.Fatalf("default maxOut = %d", ex.maxOut)
	}
}

// Security: removing the run_user privilege-drop logic (so the command runs
// as the sandbox user directly without su) must be detected on Linux. The
// buildCommand contract is verified here because a Linux host is where it runs.
func TestExecBuildCommandNonRoot(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("su invocation only applies on linux")
	}
	ex := NewExecutor("/tmp", "ancientai", time.Second, 1024)
	argv := ex.buildCommand("echo hi")
	if !strings.Contains(strings.Join(argv, " "), "su") {
		t.Fatalf("expected su for non-current run_user, got %v", argv)
	}
	if ex.buildCommand("echo hi")[0] != "su" {
		t.Fatalf("expected su binary as argv[0]")
	}
}

func TestExecBuildCommandSameUser(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("user comparison only applies on linux")
	}
	// runUser empty → direct bash, never su.
	ex := NewExecutor("/tmp", "", time.Second, 1024)
	argv := ex.buildCommand("echo hi")
	if argv[0] != "bash" {
		t.Fatalf("expected direct bash, got %v", argv)
	}
}
