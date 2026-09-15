package redact

import (
	"strings"
	"testing"
)

func TestRedactHidesKnownSecrets(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		prefix string // leading chars of the secret token that must survive masking
	}{
		{"openai", "key sk-abcdefGHIJKLMNOPQRSTUVWXyz123456 end", "sk-abc"},
		{"anthropic", "k=sk-ant-api03-abcdEFGHIJKLMNOPQRSTUVWXYZ1234567890", "sk-ant"},
		{"nvidia", "nvapi-abcDEFGHIJKLMNOPQRSTUVWXYZ1234567890", "nvapi-"},
		{"google", "AIzaSyABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", "AIzaSy"},
		{"aws", "AKIAIOSFODNN7EXAMPLE", "AKIAIO"},
		{"github", "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ012345", "ghp_AB"},
		{"github_user", "ghu_abcdefGHIJKLMNOPQRSTUVWXYZ0123456789", "ghu_ab"},
		{"bearer_jwt", `Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOnsdf.abc123XYZ$~+/=`, "Bearer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.input)
			if strings.Contains(got, tt.input) {
				t.Fatalf("Redact kept the full secret: %q", got)
			}
			if !strings.Contains(got, "***") {
				t.Fatalf("Redact output %q has no masking", got)
			}
			if !strings.Contains(got, tt.prefix) {
				t.Fatalf("Redact output %q lost the identifiable prefix %q", got, tt.prefix)
			}
		})
	}
}

func TestRedactPEMBlock(t *testing.T) {
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIB...base64...more\n-----END RSA PRIVATE KEY-----"
	got := Redact(pem)
	if strings.Contains(got, "BEGIN RSA PRIVATE KEY") {
		t.Fatalf("PEM block was not masked: %q", got)
	}
}

func TestRedactBenignTextUnchanged(t *testing.T) {
	in := "just a normal sentence with an sk-word inside and numbers 1234567890"
	if got := Redact(in); got != in {
		t.Fatalf("benign input was altered: %q", got)
	}
}

func TestRedactEmpty(t *testing.T) {
	if got := Redact(""); got != "" {
		t.Fatalf("empty input produced %q", got)
	}
}

func TestRedactIdempotent(t *testing.T) {
	in := "key sk-abcdefGHIJKLMNOPQRSTUVWXyz123456 end"
	once := Redact(in)
	twice := Redact(once)
	if once != twice {
		t.Fatalf("Redact is not idempotent: %q -> %q", once, twice)
	}
}

func TestFindSecrets(t *testing.T) {
	got := FindSecrets("use nvapi-abcDEFGHIJKLMNOPQRSTUVWXYZ1234567890 and sk-abcdefGHIJKLMNOPQRSTUVWXyz123456")
	if len(got) != 2 {
		t.Fatalf("expected 2 secrets, got %d: %v", len(got), got)
	}
}

func TestFindSecretsNone(t *testing.T) {
	if got := FindSecrets("nothing sensitive here"); len(got) != 0 {
		t.Fatalf("unexpected secrets found: %v", got)
	}
}
