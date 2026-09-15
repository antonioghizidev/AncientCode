package sandbox

import (
	"strings"
	"testing"
)

func TestHardBlockRmRfSlash(t *testing.T) {
	p := NewPolicy(DefaultAllowed)
	if err := p.AllowCommand("rm -rf /"); err == nil {
		t.Fatal("rm -rf / not blocked")
	}
}

func TestHardBlockRmRfSlashQuoted(t *testing.T) {
	p := NewPolicy(DefaultAllowed)
	cases := []string{
		`rm -rf "/"`,
		`rm -rf '/'`,
		`rm  -rf  /`,
		`rm -r -f /`,
		`rm -rf /etc`,
		`rm -rf /home`,
	}
	for _, cmd := range cases {
		if err := p.AllowCommand(cmd); err == nil {
			t.Fatalf("hard-block bypass via quoting/chaining: %q", cmd)
		}
	}
}

func TestHardBlockDdOfDev(t *testing.T) {
	p := NewPolicy(DefaultAllowed)
	if err := p.AllowCommand("dd if=foo of=/dev/sda bs=512"); err == nil {
		t.Fatal("dd of=/dev/sda not blocked")
	}
}

func TestHardBlockMkfs(t *testing.T) {
	p := NewPolicy(DefaultAllowed)
	if err := p.AllowCommand("mkfs.ext4 /dev/sda1"); err == nil {
		t.Fatal("mkfs not blocked")
	}
}

func TestHardBlockChmod777(t *testing.T) {
	p := NewPolicy(DefaultAllowed)
	if err := p.AllowCommand("chmod -R 777 /"); err == nil {
		t.Fatal("chmod -R 777 / not blocked")
	}
}

func TestHardBlockSudo(t *testing.T) {
	p := NewPolicy(DefaultAllowed)
	if err := p.AllowCommand("sudo apt install foo"); err == nil {
		t.Fatal("sudo not blocked")
	}
}

func TestHardBlockWipefs(t *testing.T) {
	p := NewPolicy(DefaultAllowed)
	if err := p.AllowCommand("wipefs -a /dev/sda"); err == nil {
		t.Fatal("wipefs not blocked")
	}
}

func TestChainingDenied(t *testing.T) {
	p := NewPolicy(DefaultAllowed)
	cases := []string{
		"ls; rm -rf /",
		"ls && rm -rf /",
		"ls | cat",
		"ls\nrm -rf /",
		"echo $(whoami)",
		"echo `whoami`",
	}
	for _, cmd := range cases {
		if err := p.AllowCommand(cmd); err == nil {
			t.Fatalf("chaining not denied: %q", cmd)
		}
	}
}

func TestWhitelistAllowsPrefix(t *testing.T) {
	p := NewPolicy(DefaultAllowed)
	if err := p.AllowExecutable("ls"); err != nil {
		t.Fatalf("ls should be allowed: %v", err)
	}
	if err := p.AllowExecutable("git"); err != nil {
		t.Fatalf("git should be allowed: %v", err)
	}
	if err := p.AllowExecutable("docker"); err != nil {
		t.Fatalf("docker should be allowed: %v", err)
	}
}

func TestWhitelistDeniesUnknown(t *testing.T) {
	p := NewPolicy(DefaultAllowed)
	// AllowExecutable is camada 2 whitelist; unknown names are denied by default.
	if err := p.AllowExecutable("nc"); err == nil {
		t.Fatal("unknown executable should be denied by default")
	}
}

func TestIsAllowedConvenience(t *testing.T) {
	p := NewPolicy(DefaultAllowed)
	if !p.IsAllowed("curl") {
		t.Fatal("curl should be allowed")
	}
	if p.IsAllowed("nc") {
		t.Fatal("nc should not be allowed")
	}
}

// Security: if the hard-block protection is removed, these tests must fail.
func TestHardBlockSecurityFailsIfProtectionRemoved(t *testing.T) {
	empty := &Policy{allowed: DefaultAllowed, blocked: nil}
	if err := empty.AllowCommand("rm -rf /"); err != nil {
		t.Fatalf("expected no error without hard-block, got: %v", err)
	}
	// This test documents that removing the protection opens the door.
	// In real code, defaultBlocked is always applied.
}

func TestNormalizeStripsQuotes(t *testing.T) {
	raw := `rm -rf "/"`
	norm := Normalize(raw)
	if strings.Contains(norm, `"`) {
		t.Fatalf("normalize did not strip quotes: %q", norm)
	}
	if !strings.Contains(norm, "rm-rf/") {
		t.Fatalf("normalized form wrong: %q", norm)
	}
}
