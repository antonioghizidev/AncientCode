package shell

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"ancientai/internal/tools"
)

func TestHardBlockPatterns(t *testing.T) {
	// These must be refused BEFORE any execution exists, so they never need bash.
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
		_, err := New(65536).Execute(context.Background(), tools.Raw(`{"cmd":`+quote(cmd)+`}`))
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
		if _, err := New(65536).Execute(context.Background(), tools.Raw(`{"cmd":`+quote(cmd)+`}`)); err == nil {
			t.Errorf("chaining not denied: %q", cmd)
		}
	}
}

func TestOversizedCmdRejected(t *testing.T) {
	cmd := strings.Repeat("a", maxCmdBytes+1)
	if _, err := New(65536).Execute(context.Background(), tools.Raw(`{"cmd":`+quote(cmd)+`}`)); err == nil {
		t.Fatal("oversized cmd accepted")
	}
}

func TestStrictArgs(t *testing.T) {
	for _, args := range []string{
		`{"cmd":"echo ok","extra":1}`,
		`{"cmd":123}`,
		`{"cmd":"echo ok"} trailing`,
	} {
		if _, err := New(65536).Execute(context.Background(), tools.Raw(args)); err == nil {
			t.Errorf("strict args accepted: %s", args)
		}
	}
}

func TestEcho(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	res, err := New(65536).Execute(context.Background(), tools.Raw(`{"cmd":"echo ok"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Output) != "ok" || res.ExitCode != 0 {
		t.Fatalf("echo = %q exit %d", res.Output, res.ExitCode)
	}
}

func TestUnknownCommandExitCode(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	res, err := New(65536).Execute(context.Background(), tools.Raw(`{"cmd":"definitely-not-a-real-cmd-xyz"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode == 0 {
		t.Fatal("unknown command should exit non-zero")
	}
}

// Security: a runaway command is killed by the context, not the host.
func TestTimeoutKills(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	res, err := New(65536).Execute(ctx, tools.Raw(`{"cmd":"sleep 5"}`))
	if err == nil {
		// The child may have been killed: exit code 137, no harness hang.
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
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	res, err := New(16).Execute(context.Background(), tools.Raw(`{"cmd":"seq 1 100000"}`))
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

func quote(s string) string { return `"` + s + `"` }
