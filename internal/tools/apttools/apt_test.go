package apttools

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

	"github.com/antonioghizidev/ancientcode/internal/tools"
)

func TestAptInstallVector(t *testing.T) {
	var gotName string
	var gotArgs []string
	capture := func(_ context.Context, name string, args ...string) *exec.Cmd {
		gotName, gotArgs = name, args
		return helperCmd(t)
	}
	orig := execCommand
	execCommand = capture
	t.Cleanup(func() { execCommand = orig })

	if _, err := New(0, 0).Execute(context.Background(), tools.Raw(`{"action":"install","packages":["htop","git"]}`)); err != nil {
		t.Fatal(err)
	}
	if gotName != "apt-get" {
		t.Fatalf("binary = %q, want apt-get", gotName)
	}
	want := "install -y htop git"
	if strings.Join(gotArgs, " ") != want {
		t.Fatalf("argv = %q, want %q", strings.Join(gotArgs, " "), want)
	}
}

func TestAptUpdateVector(t *testing.T) {
	var gotArgs []string
	capture := func(_ context.Context, _ string, args ...string) *exec.Cmd {
		gotArgs = args
		return helperCmd(t)
	}
	orig := execCommand
	execCommand = capture
	t.Cleanup(func() { execCommand = orig })

	if _, err := New(0, 0).Execute(context.Background(), tools.Raw(`{"action":"update"}`)); err != nil {
		t.Fatal(err)
	}
	if strings.Join(gotArgs, " ") != "update" {
		t.Fatalf("argv = %q, want update", strings.Join(gotArgs, " "))
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
	cmd := exec.Command(exe, "-test.run=TestAptHelperProcess")
	cmd.Env = append(os.Environ(), "GO_WANT_APT_HELPER=1")
	return cmd
}

// TestAptHelperProcess is the re-executed helper: it does not run as a test, it
// just exits 0 so the tool observes a successful (empty) run.
func TestAptHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_APT_HELPER") != "1" {
		t.Skip("helper process")
	}
}

func TestAptRemovalDenied(t *testing.T) {
	for _, action := range []string{"remove", "purge", "autoremove", "upgrade", "full-upgrade", "dist-upgrade"} {
		_, err := New(0, 0).Execute(context.Background(), tools.Raw(`{"action":"`+action+`","packages":["x"]}`))
		var de *tools.DeniedError
		if !errors.As(err, &de) {
			t.Errorf("action %q not denied (err=%v)", action, err)
		}
	}
}

func TestAptInvalidPackageNames(t *testing.T) {
	for _, pkg := range []string{"-y", "--install-suggests", "../evil", "pkg;rm", "pkg$", "Pkg", "with space", "-o"} {
		_, err := New(0, 0).Execute(context.Background(), tools.Raw(`{"action":"install","packages":["`+pkg+`"]}`))
		var de *tools.DeniedError
		if !errors.As(err, &de) {
			t.Errorf("package %q not denied (err=%v)", pkg, err)
		}
	}
}

func TestAptValidPackageNames(t *testing.T) {
	for _, pkg := range []string{"htop", "libssl-dev", "python3.12", "g++", "my-pkg_1.0", "pkg++"} {
		if !packageNameRE.MatchString(pkg) {
			t.Errorf("valid package %q rejected by regex", pkg)
		}
	}
}

func TestAptStrictArgs(t *testing.T) {
	for _, args := range []string{
		`{"action":"update","packages":{}}`,
		`{"action":"install"}`, // missing packages
		`{"action":"update","x":1}`,
		`{"attempt":"evil"}`,
	} {
		if _, err := New(0, 0).Execute(context.Background(), tools.Raw(args)); err == nil {
			t.Errorf("strict args accepted: %s", args)
		}
	}
}

func TestAptInstallWithoutPackagesDenied(t *testing.T) {
	if _, err := New(0, 0).Execute(context.Background(), tools.Raw(`{"action":"install","packages":[]}`)); err == nil {
		t.Fatal("install with no packages accepted")
	}
}

// PATH-injected fake apt-get binary (Linux/CI): assert the tool drives the real
// apt-get frontend without a shell.
func TestAptResolvesFromPATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executable via PATH is not portable on windows; covered by helper tests")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "apt-get")
	script := "#!/bin/sh\nif [ \"$1\" = \"update\" ]; then echo UPDATED; exit 0; fi\necho unexpected >&2; exit 43\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	at := New(5*time.Second, 1024)
	res, err := at.Execute(context.Background(), tools.Raw(`{"action":"update"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "UPDATED") {
		t.Fatalf("output = %q, want UPDATED", res.Output)
	}
}

func TestAptDeniedDoesNotExec(t *testing.T) {
	hit := false
	orig := execCommand
	execCommand = func(_ context.Context, _ string, _ ...string) *exec.Cmd { hit = true; return nil }
	t.Cleanup(func() { execCommand = orig })

	if _, err := New(0, 0).Execute(context.Background(), tools.Raw(`{"action":"purge","packages":["x"]}`)); err == nil {
		t.Fatal("purge not denied")
	}
	if hit {
		t.Fatal("CLI was executed for a denied action")
	}
}
