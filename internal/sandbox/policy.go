// Package sandbox implements execution-layer security: a command policy
// (whitelist + hard-block) and a subprocess executor with user isolation,
// timeout, output limits and workdir confinement (SPEC §5.2, camadas 2 e 4).
package sandbox

import (
	"fmt"
	"regexp"
	"strings"
)

// defaultBlocked are hard-block patterns matched on the whitespace-normalized
// form of the command.  They are inalterable — no config may override them
// (SPEC §5.2 camada 2).
var defaultBlocked = []*regexp.Regexp{
	regexp.MustCompile(`rm-rf/`),
	regexp.MustCompile(`rm-fr/`),
	regexp.MustCompile(`rm-rf\*`),
	regexp.MustCompile(`rm-r-f/`),
	regexp.MustCompile(`rm-f-r/`),
	regexp.MustCompile(`rm-.+-f.*/`),
	regexp.MustCompile(`of=/dev/`),
	regexp.MustCompile(`<>dev`),
	regexp.MustCompile(`(^|/)mkfs`),
	regexp.MustCompile(`dd.*of=/dev/`),
	regexp.MustCompile(`wipefs`),
	regexp.MustCompile(`fdisk`),
	regexp.MustCompile(`parted`),
	regexp.MustCompile(`sudo`),
	regexp.MustCompile(`chmod-?[rR]?777/?$`),
	regexp.MustCompile(`format.*/dev/`),
}

// chainingRe matches shell separators denied by default.
var chainingRe = regexp.MustCompile(`[;|&]|\n|\$\(|` + "`")

// DefaultAllowed is the baseline whitelist for the shell tool (SPEC §5.2).
var DefaultAllowed = []string{
	"ls", "cat", "head", "tail", "grep", "find", "wc", "sort", "uniq", "cut",
	"echo", "env", "pwd", "whoami", "id", "date", "seq", "yes", "sleep",
	"git", "docker", "ip", "sysctl", "journalctl", "apt", "apt-get",
	"systemctl", "df", "du", "free", "uname", "lscpu", "uptime",
	"ps", "top", "htop", "ping", "curl", "wget",
	"python3", "python", "node", "go", "cargo", "make", "cmake",
}

// Policy enforces camada 2 of the execution defense (SPEC §5.2): a whitelist
// by command prefix and an inalterable hard-block list.
type Policy struct {
	allowed []string
	blocked []*regexp.Regexp
}

// NewPolicy builds a policy from the allowed command list.
// hard-blocked patterns are always applied regardless of the allowed list.
func NewPolicy(allowed []string) *Policy {
	return &Policy{allowed: allowed, blocked: defaultBlocked}
}

// Normalize strips whitespace, tabs, newlines, single/double quotes and
// backslashes so that hard-block matches survive arbitrary quoting (SPEC §5.3).
func Normalize(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '\'', '"', '\\':
			continue
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// ChainingDenied reports whether the command uses denied shell separators.
func ChainingDenied(cmd string) bool {
	return chainingRe.MatchString(cmd)
}

// DeniedError marks a command refused by the policy layer (camada 2). It lets
// callers and the audit distinguish a security denial from a runtime error.
type DeniedError struct {
	Reason string
}

func (e *DeniedError) Error() string { return "sandbox: refused: " + e.Reason }

// AllowCommand checks a raw command string against the hard-block list and
// the whitelist.  It returns nil if the command is allowed, or a *DeniedError
// describing why it was denied.
func (p *Policy) AllowCommand(cmd string) error {
	norm := Normalize(cmd)
	for _, re := range p.blocked {
		if re.MatchString(norm) {
			return &DeniedError{Reason: fmt.Sprintf("hard-block pattern %q matched", re.String())}
		}
	}
	if ChainingDenied(cmd) {
		return &DeniedError{Reason: "command chaining is denied by default"}
	}
	return nil
}

// AllowExecutable checks whether the given executable name matches any entry
// in the allowed list (prefix match).  Returns nil if matched, error otherwise.
func (p *Policy) AllowExecutable(name string) error {
	base := strings.TrimSpace(name)
	if base == "" {
		return &DeniedError{Reason: "empty executable"}
	}
	for _, prefix := range p.allowed {
		if base == prefix || strings.HasPrefix(base, prefix+"/") {
			return nil
		}
	}
	return &DeniedError{Reason: fmt.Sprintf("executable %q is not in the allowed list", base)}
}

// IsAllowed reports whether the given executable is in the whitelist without
// returning an error (convenience for callers that need a bool).
func (p *Policy) IsAllowed(name string) bool {
	return p.AllowExecutable(name) == nil
}
