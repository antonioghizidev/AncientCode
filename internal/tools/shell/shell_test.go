package shell

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/antonioghizidev/ancientcode/internal/tools"
)

func TestHardBlockPatterns(t *testing.T) {
	blocked := []string{
		"rm -rf /",
		`rm -rf "/"`,
		"rm  - rf /etc",
		"rm -fr /",
		"dd if=/dev/zero of=/dev/sda bs=4M",
		"mkfs.ext4 /dev/sdb1",
		"wipefs -a /dev/sda",
		"fdisk /dev/sda",
		"parted /dev/sda mklabel gpt",
		"sudo apt update",
		"chmod -R 777 /",
		"chmod 777 /",
		"cat /etc/shadow ; rm -rf /tmp",
	}
	for _, cmd := range blocked {
		_, err := NewSimple(65536).Execute(context.Background(), tools.Raw(`{"cmd":`+quote(cmd)+`}`))
		if err == nil {
			t.Errorf("hard-block missed: %q", cmd)
		}
	}
}

func TestChainingDenied(t *testing.T) {
	blocked := []string{
		"echo a && echo b",
		"echo a || echo b",
		"echo a; echo b",
		"echo a | head",
		"echo a & echo b",
		"echo a\n echo b",
		"echo $(whoami)",
	}
	for _, cmd := range blocked {
		if _, err := NewSimple(65536).Execute(context.Background(), tools.Raw(`{"cmd":`+quote(cmd)+`}`)); err == nil {
			t.Errorf("chaining not denied: %q", cmd)
		}
	}
}

func TestOversizedCmdRejected(t *testing.T) {
	cmd := strings.Repeat("a", maxCmdBytes+1)
	if _, err := NewSimple(65536).Execute(context.Background(), tools.Raw(`{"cmd":`+quote(cmd)+`}`)); err == nil {
		t.Fatal("oversized cmd accepted")
	}
}

func TestStrictArgs(t *testing.T) {
	for _, args := range []string{
		`{"cmd":"echo ok","extra":1}`,
		`{"cmd":123}`,
		`{"cmd":"echo ok"} trailing`,
	} {
		if _, err := NewSimple(65536).Execute(context.Background(), tools.Raw(args)); err == nil {
			t.Errorf("strict args accepted: %s", args)
		}
	}
}

func TestEcho(t *testing.T) {
	requireBash(t)
	res, err := NewSimple(65536).Execute(t.Context(), tools.Raw(`{"cmd":"echo ok"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Output) != "ok" || res.ExitCode != 0 {
		t.Fatalf("echo = %q exit %d", res.Output, res.ExitCode)
	}
}

// Security: an executable outside the whitelist is denied before any
// execution attempt (deny-by-default), even with approval.
func TestUnknownCommandDeniedByWhitelist(t *testing.T) {
	_, err := NewSimple(65536).Execute(t.Context(), tools.Raw(`{"cmd":"definitely-not-a-real-cmd-xyz"}`))
	var de *tools.DeniedError
	if !errors.As(err, &de) {
		t.Fatalf("expected whitelist denial, got %v", err)
	}
	if !strings.Contains(de.Reason, "not in the allowed list") {
		t.Fatalf("unexpected reason: %s", de.Reason)
	}
}

// Security: a runaway command is killed by the context, not the host.
func TestTimeoutKills(t *testing.T) {
	requireBash(t)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	res, err := NewSimple(65536).Execute(ctx, tools.Raw(`{"cmd":"sleep 5"}`))
	if err == nil {
		if res.ExitCode == 0 {
			t.Fatal("sleep completed despite timeout")
		}
		return
	}
	if strings.Contains(err.Error(), "deadline") {
		return
	}
	t.Fatalf("unexpected error: %v", err)
}

func TestOutputTruncated(t *testing.T) {
	requireBash(t)
	res, err := NewSimple(16).Execute(t.Context(), tools.Raw(`{"cmd":"seq 1 100000"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated {
		t.Fatal("output should be truncated")
	}
	if len(res.Output) > 16 {
		t.Fatalf("buffer exceeded limit: %d", len(res.Output))
	}
}

// requireBash skips when bash is not usable.  On non-Linux hosts the shell
// functional tests are not meaningful (the target is Linux minimal), so we
// skip unconditionally on other platforms.  On Linux we still trial-run
// `bash -c "true"` to guard against environments where the launcher exists
// but the shell is broken (e.g. Windows Store / WSL shim).
func requireBash(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("shell functional tests only run on Linux (target host)")
	}
	path, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, path, "-c", "true").Run(); err != nil {
		t.Skipf("bash unusable on this host: %v", err)
	}
}

func quote(s string) string { return `"` + s + `"` }
