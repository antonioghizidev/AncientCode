// Package skills implements reusable agent skills (SPEC §11.1, M7).
//
// A skill is a directory <skills_dir>/<skill>/SKILL.md with a strict YAML
// frontmatter (name, description, allowed_tools, origin, origin_hash) and an
// optional scripts/ folder. Skills are UNTRUSTED third-party content:
//   - the body is only injected into the system prompt wrapped in the untrusted
//     markers (D-2026-09-16-41), never executed at load;
//   - allowed_tools only RESTRICTS the base toolbox, never grants (D-38);
//   - scripts only run through the skill_run gate+ sandbox (D-39);
//   - downloaded skills (origin set) require an explicit operator verification
//     stamp; any content change after it fails closed until re-verification
//     (D-2026-09-16-40).
package skills

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/antonioghizidev/ancientcode/internal/tools"
)

const (
	// skillFileName is the canonical mainfest name inside a skill directory.
	skillFileName = "SKILL.md"
	// StampName is the operator-verification stamp written by --verify-skill.
	StampName = ".ancientcode-verified"
	// maxSkillBytes bounds SKILL.md (the model controls its size indirectly).
	maxSkillBytes = 256 << 10 // 256 KiB
	// maxScriptBytes bounds a single skill script.
	maxScriptBytes = 64 << 10 // 64 KiB
	stampVersion   = 1
)

// skillNameRe imposes the strict skill-name grammar. It excludes path
// separators, dots, spaces and anything that could traverse or hide, on every
// platform (both '/' and '\\' fail the class). Leading/trailing/double dashes
// are rejected so a name can never be just a separator artifact.
var skillNameRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ValidateName enforces the skill-name grammar (SPEC §11.1, M7 contract).
func ValidateName(name string) error {
	if name == "" {
		return errors.New("skills: empty skill name")
	}
	if !skillNameRe.MatchString(name) {
		return fmt.Errorf("skills: invalid skill name %q (must match %s)", name, skillNameRe)
	}
	return nil
}

// Meta is the parsed frontmatter of SKILL.md. Fields are explicit: unknown
// keys are rejected fail-closed (D-2026-09-16-41).
type Meta struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	AllowedTools []string `yaml:"allowed_tools"`
	Origin       string   `yaml:"origin"`      // set for downloaded skills
	OriginHash   string   `yaml:"origin_hash"` // published sha256, if any
}

// Skill is one loaded skill, fully validated and confined to its directory.
type Skill struct {
	Name         string
	Description  string
	AllowedTools []string
	Instructions string // body of SKILL.md (untrusted data)
	Origin       string
	OriginHash   string
	Dir          string   // absolute resolved skill directory
	ScriptsDir   string   // absolute path, empty when the skill has no scripts
	ScriptNames  []string // plain script file names, sorted
}

// stamp records the operator's verification hashes (D-2026-09-16-40).
type stamp struct {
	Version      int               `json:"version"`
	SkillSHA256  string            `json:"skill_sha256"`
	ScriptSHA256 map[string]string `json:"script_sha256,omitempty"`
	VerifiedAt   time.Time         `json:"verified_at"`
}

// Load reads and validates a skill, and refuses it when a downloaded skill has
// no valid verification stamp (fail-closed, SPEC §11.1).
func Load(skillsDir, name string) (*Skill, error) {
	s, err := readSkill(skillsDir, name)
	if err != nil {
		return nil, err
	}
	if s.needsVerification() {
		ok, err := s.Verified()
		if err != nil {
			return nil, fmt.Errorf("skills: verificacao de %q: %w", name, err)
		}
		if !ok {
			return nil, fmt.Errorf("skills: skill %q foi baixada (origin presente) e não foi verificada "+
				"ou foi alterada apos a verificacao; rode --verify-skill %s e so carregue apos revisao do operador", name, name)
		}
	}
	return s, nil
}

// Verify performs the explicit operator verification of a downloaded skill:
// it computes the sha256 of SKILL.md and of every script, cross-checks the
// published origin_hash when present, and writes the stamp. Idempotent for
// unchanged content.
func Verify(skillsDir, name string) error {
	s, err := readSkill(skillsDir, name)
	if err != nil {
		return err
	}
	skillHash, err := hashFile(filepath.Join(s.Dir, skillFileName))
	if err != nil {
		return err
	}
	if s.OriginHash != "" && s.OriginHash != skillHash {
		return fmt.Errorf("skills: sha256 de %q (%s) nao confere com o origin_hash publicado (%s); recusado",
			name, skillHash, redactHash(s.OriginHash))
	}
	scripts := map[string]string{}
	for _, n := range s.ScriptNames {
		h, err := hashFile(filepath.Join(s.ScriptsDir, n))
		if err != nil {
			return err
		}
		scripts[n] = h
	}
	st := stamp{Version: stampVersion, SkillSHA256: skillHash, ScriptSHA256: scripts, VerifiedAt: time.Now().UTC()}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.Dir, StampName), data, 0o600); err != nil {
		return err
	}
	return nil
}

// readSkill parses and validates the skill WITHOUT the verification
// enforcement — the shared body of Load and Verify.
func readSkill(skillsDir, name string) (*Skill, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(skillsDir)
	if err != nil {
		return nil, fmt.Errorf("skills: skills_dir: %w", err)
	}
	if _, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("skills: skills dir %q: %w", skillsDir, err)
	}
	dir, err := filepath.EvalSymlinks(filepath.Join(root, name))
	if err != nil {
		return nil, fmt.Errorf("skills: skill %q not found under %q: %v", name, skillsDir, err)
	}
	if !within(root, dir) {
		return nil, fmt.Errorf("skills: skill dir %q escapes the skills root (symlink)", dir)
	}
	raw, err := readLimited(filepath.Join(dir, skillFileName), maxSkillBytes)
	if err != nil {
		return nil, fmt.Errorf("skills: %s: %w", skillFileName, err)
	}
	meta, body, err := splitFrontmatter(raw)
	if err != nil {
		return nil, fmt.Errorf("skills: %s de %q: %w", skillFileName, name, err)
	}
	if meta.Name != name {
		return nil, fmt.Errorf("skills: frontmatter name %q difere do nome do diretorio %q (identidade obrigatoria)",
			meta.Name, name)
	}
	allowed, err := cleanAllowed(meta.AllowedTools)
	if err != nil {
		return nil, fmt.Errorf("skills: allowed_tools de %q: %w", name, err)
	}
	scriptsDir, scripts, err := discoverScripts(dir)
	if err != nil {
		return nil, err
	}
	return &Skill{
		Name:         name,
		Description:  meta.Description,
		AllowedTools: allowed,
		Instructions: strings.TrimSpace(body),
		Origin:       strings.TrimSpace(meta.Origin),
		OriginHash:   strings.TrimSpace(meta.OriginHash),
		Dir:          dir,
		ScriptsDir:   scriptsDir,
		ScriptNames:  scripts,
	}, nil
}

// needsVerification reports whether the skill declares an external origin
// (i.e. it was downloaded and must carry an operator stamp).
func (s *Skill) needsVerification() bool { return s.Origin != "" }

// Verified reports whether the stamp matches the current SKILL.md bytes.
func (s *Skill) Verified() (bool, error) {
	st, err := readStamp(s.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	cur, err := hashFile(filepath.Join(s.Dir, skillFileName))
	if err != nil {
		return false, err
	}
	return cur == st.SkillSHA256, nil
}

// ScriptPath resolves a script name strictly inside the skill's scripts dir.
func (s *Skill) ScriptPath(name string) (string, error) {
	if s.ScriptsDir == "" {
		return "", fmt.Errorf("skills: skill %q nao tem diretorio scripts", s.Name)
	}
	ok := false
	for _, n := range s.ScriptNames {
		if n == name {
			ok = true
			break
		}
	}
	if !ok {
		return "", fmt.Errorf("skills: script %q nao existe em scripts/ de %q", name, s.Name)
	}
	clean := filepath.Join(s.ScriptsDir, name)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", fmt.Errorf("skills: script %q: %w", name, err)
	}
	if !within(s.ScriptsDir, resolved) {
		return "", fmt.Errorf("skills: script %q resolves fora de scripts/ (symlink)", name)
	}
	return resolved, nil
}

// VerifyScript checks the recorded hash of a script against the current bytes.
// Local skills (no stamp) pass: the confinement (path + gate) is the authority;
// downloaded skills must match the operator-verified hash before running.
func (s *Skill) VerifyScript(name string) error {
	if _, err := s.ScriptPath(name); err != nil {
		return err
	}
	st, err := readStamp(s.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	want, ok := st.ScriptSHA256[name]
	if !ok {
		return nil // script was not part of the verified set
	}
	got, err := hashFile(filepath.Join(s.ScriptsDir, name))
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("skills: script %q de %q foi alterado desde a verificacao do operador; rode --verify-skill %s",
			name, s.Name, s.Name)
	}
	return nil
}

// splitFrontmatter extracts a leading "---\n...\n---\n" YAML block and the
// remaining body. A missing or malformed block is an error (fail-closed).
func splitFrontmatter(data []byte) (Meta, string, error) {
	var meta Meta
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return meta, "", errors.New("frontmatter ausente (SKILL.md deve comecar com ---)")
	}
	rest := data[4:]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		return meta, "", errors.New("frontmatter sem fechamento '---'")
	}
	fm := rest[:end]
	if len(bytes.TrimSpace(fm)) == 0 {
		return meta, "", errors.New("frontmatter vazio")
	}
	dec := yaml.NewDecoder(bytes.NewReader(fm))
	dec.KnownFields(true)
	if err := dec.Decode(&meta); err != nil {
		return meta, "", fmt.Errorf("frontmatter YAML invalido: %w", err)
	}
	if strings.TrimSpace(meta.Name) == "" || strings.TrimSpace(meta.Description) == "" {
		return meta, "", errors.New("frontmatter exige 'name' e 'description' nao vazios")
	}
	body := rest[end+len("\n---\n"):]
	return meta, string(body), nil
}

// cleanAllowed trims and validates every allowed_tools entry using the tool
// name grammar (D-38: list is only enforced later against the registry).
func cleanAllowed(in []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, raw := range in {
		n := strings.TrimSpace(raw)
		if n == "" {
			continue
		}
		if err := tools.ValidateName(n); err != nil {
			return nil, err
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, nil
}

// discoverScripts lists the regular files directly inside scripts/. Invalid
// entries (directories, oversized or unreadable files) reject the whole skill
// (fail-closed). The skill loads with ScriptsDir empty when scripts/ is absent.
func discoverScripts(skillDir string) (scriptsDir string, names []string, err error) {
	sd := filepath.Join(skillDir, "scripts")
	entries, err := os.ReadDir(sd)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, derr := os.Stat(skillDir); derr == nil {
				return "", nil, nil
			}
		}
		return "", nil, fmt.Errorf("skills: scripts/: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			info, ierr := e.Info()
			if ierr != nil {
				return "", nil, fmt.Errorf("skills: script %q: %w", e.Name(), ierr)
			}
			if !info.Mode().IsRegular() {
				return "", nil, fmt.Errorf("skills: script %q nao e um arquivo regular; recusado", e.Name())
			}
			if info.Size() > maxScriptBytes {
				return "", nil, fmt.Errorf("skills: script %q excede %d bytes; recusado", e.Name(), maxScriptBytes)
			}
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return sd, names, nil
}

// within reports whether p stays at or below root (resolved paths).
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// readLimited reads a file with a hard byte ceiling (fail-closed on runaway
// content — SPEC §7.1).
func readLimited(path string, max int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > max {
		return nil, fmt.Errorf("excede o limite de %d bytes", max)
	}
	return data, nil
}

// hashFile returns the lowercase hex sha256 of a file.
func hashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// redactHash keeps the checksum mainly redacted in errors (never full public
// value, though hashes are not secrets, it reduces fingerprint noise).
func redactHash(h string) string {
	if len(h) <= 8 {
		return "***"
	}
	return h[:6] + "***" + h[len(h)-4:]
}

func readStamp(dir string) (*stamp, error) {
	data, err := os.ReadFile(filepath.Join(dir, StampName))
	if err != nil {
		return nil, err
	}
	var st stamp
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("skills: stamp invalido: %w", err)
	}
	return &st, nil
}
