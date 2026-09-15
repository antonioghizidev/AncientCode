// Package redact implements deterministic masking of secrets in strings.
//
// It is a security-critical package: every string that leaves the process
// (logs, session history, content sent to the model, error messages) must be
// passed through Redact before persistence or transmission, per SPEC §5.5 and
// §5.6.3. Redact only masks; it does not prevent a secret from being used for
// its intended purpose (e.g. authentication). Over-masking is safe, under-
// masking is a vulnerability.
package redact

import (
	"regexp"
)

var secretPatterns = []*regexp.Regexp{
	// Anthropic keys: sk-ant-...
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{20,}`),
	// OpenAI and generic sk- keys.
	regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),
	// NVIDIA NIM / build.nvidia.com keys.
	regexp.MustCompile(`nvapi-[A-Za-z0-9\-_]{16,}`),
	// Google API keys.
	regexp.MustCompile(`AIza[0-9A-Za-z\-_]{30,}`),
	// AWS access key id.
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	// GitHub tokens (classic, fine-grained, OAuth, refresh, user-to-server).
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),
	// Bearer tokens in "Authorization"-style payloads. Matched verbatim
	// (including whitespace) so the header line cannot come back unaltered.
	regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9\-._~+/=]{10,}`),
	// PEM private key blocks, possibly spanning multiple lines.
	regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`),
}

// mask keeps a recognizable prefix and a 4-character tail for identification
// while hiding the body. Shorter tokens are fully masked.
func mask(full string) string {
	switch {
	case len(full) <= 8:
		return "****"
	case len(full) <= 12:
		return full[:4] + "***"
	default:
		return full[:6] + "***" + full[len(full)-4:]
	}
}

// Redact returns s with every known secret pattern replaced by a masked
// version. Output of Redact is safe to log, persist or send to a model.
func Redact(s string) string {
	out := s
	for _, re := range secretPatterns {
		out = re.ReplaceAllStringFunc(out, mask)
	}
	return out
}

// FindSecrets returns the (unmasked) secret-like substrings found in s.
// It is the building block for the exfiltration scan of SPEC §5.6.4: before a
// tool is executed, its arguments should be scanned and, if any secret-like
// token appears, the call must be rejected.
func FindSecrets(s string) []string {
	var found []string
	seen := make(map[string]struct{})
	for _, re := range secretPatterns {
		for _, m := range re.FindAllString(s, -1) {
			if _, ok := seen[m]; ok {
				continue
			}
			seen[m] = struct{}{}
			found = append(found, m)
		}
	}
	return found
}
