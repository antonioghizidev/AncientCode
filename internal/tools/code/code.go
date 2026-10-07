// Package code implements the coding-agent tools (SPEC §10): code_scaffold,
// edit_file and code_review. All of them are confined to the code_root via the
// files confinement helpers. Generated code is never executed automatically.
package code

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/antonioghizidev/ancientcode/internal/redact"
	"github.com/antonioghizidev/ancientcode/internal/tools"
	"github.com/antonioghizidev/ancientcode/internal/tools/files"
)

//go:embed templates
var templatesFS embed.FS

// maxReviewBytes bounds a single reviewed file.
const maxReviewBytes = 2 << 20 // 2 MiB

// maxTemplateFiles bounds the number of files one scaffold writes.
const maxTemplateFiles = 256

// projectNameRe accepts safe project names (no path separators, no dot-dot).
var projectNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// moduleRe accepts Go module paths without traversal (SPEC §10.3).
var moduleRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9./_-]*$`)

// codeSensitive segments hard-blocked once placed under a project, on top of
// the generic sensitive list (SPEC §10.3): never touch third-party repos or
// secrets. Compared per path segment.
var codeSensitive = []string{".git", ".git-credentials", ".ssh", ".env"}

// templateFile describes one generated file of a template.
type templateFile struct {
	Path    string `yaml:"path"`
	Content string `yaml:"content"`
	Src     string `yaml:"src"`
}

// templateSpec is the template.yaml manifest.
type templateSpec struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Files       []templateFile `yaml:"files"`
}

// template is a validated, ready-to-scaffold template.
type template struct {
	spec  templateSpec
	dir   string
	files []templateFile
}

// Toolset groups the coding-agent tools around one confined code_root.
type Toolset struct {
	fs    *files.Files
	limit int
}

// NewToolset returns the code-agent tools rooted at code_root. limit bounds
// how many bytes code_review echoes back.
func NewToolset(root string, limit int) *Toolset {
	return &Toolset{fs: files.NewFiles(root, limit), limit: limit}
}

// Root returns the confined code_root.
func (t *Toolset) Root() string { return t.fs.Root() }

// templates loads and validates every embedded template once.
// templatesMap loads and validates every embedded template once. It is a raw
// var (not initialized in init) so the scaffold test flags the first execution.
var templatesMap = func() map[string]*template {
	entries, err := templatesFS.ReadDir("templates")
	if err != nil {
		panic(err)
	}
	out := make(map[string]*template, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rel := path.Join("templates", e.Name())
		raw, err := templatesFS.ReadFile(path.Join(rel, "template.yaml"))
		if err != nil {
			panic(fmt.Errorf("code: template %q missing template.yaml: %w", e.Name(), err))
		}
		var spec templateSpec
		if err := yaml.Unmarshal(raw, &spec); err != nil {
			panic(fmt.Errorf("code: template %q invalid: %w", e.Name(), err))
		}
		tpl := &template{spec: spec, dir: rel}
		tpl.files, err = loadTemplateFiles(tpl)
		if err != nil {
			panic(err)
		}
		out[e.Name()] = tpl
	}
	if len(out) == 0 {
		panic(errors.New("code: no templates found"))
	}
	return out
}

// loadTemplateFiles validates the manifest and materializes the file list,
// rejecting traversal, script files and inline scripts fail-closed (SPEC §10.1).
func loadTemplateFiles(tpl *template) ([]templateFile, error) {
	name := tpl.spec.Name
	if name == "" || len(tpl.spec.Files) == 0 {
		return nil, fmt.Errorf("code: template %q: name and files required", name)
	}
	if len(tpl.spec.Files) > maxTemplateFiles {
		return nil, fmt.Errorf("code: template %q: too many files", name)
	}
	out := make([]templateFile, 0, len(tpl.spec.Files))
	seen := map[string]bool{}
	for _, f := range tpl.spec.Files {
		if err := validateTemplatePath(f.Path); err != nil {
			return nil, fmt.Errorf("code: template %q file %q: %w", name, f.Path, err)
		}
		if seen[f.Path] {
			return nil, fmt.Errorf("code: template %q: duplicate path %q", name, f.Path)
		}
		seen[f.Path] = true
		if isScriptName(f.Path) {
			return nil, fmt.Errorf("code: template %q: file %q is a script; templates never carry executables", name, f.Path)
		}
		if f.Src != "" {
			if f.Content != "" {
				return nil, fmt.Errorf("code: template %q file %q: use 'content' or 'src', not both", name, f.Path)
			}
			if err := validateTemplatePath(f.Src); err != nil {
				return nil, fmt.Errorf("code: template %q src %q: %w", name, f.Src, err)
			}
			if isScriptName(f.Src) {
				return nil, fmt.Errorf("code: template %q: src %q is a script", name, f.Src)
			}
			raw, err := templatesFS.ReadFile(path.Join(tpl.dir, f.Src))
			if err != nil {
				return nil, fmt.Errorf("code: template %q src %q: %w", name, f.Src, err)
			}
			f.Content = string(raw)
		}
		if isShebang(f.Content) {
			return nil, fmt.Errorf("code: template %q file %q starts with a shebang; scripts are rejected", name, f.Path)
		}
		out = append(out, f)
	}
	return out, nil
}

// validateTemplatePath rejects absolute paths, drive roots, ".." escapes and
// NUL bytes. Leading separators are rejected even on Windows where "/abs" and
// "\abs" are drive-relative (SPEC §10.3: nothing escapes the code root).
var drivePrefixRe = regexp.MustCompile(`^[a-zA-Z]:`)

func validateTemplatePath(p string) error {
	if p == "" {
		return errors.New("empty path")
	}
	if strings.ContainsRune(p, 0) {
		return errors.New("path contains NUL")
	}
	if p == "." || p == ".." || filepath.IsAbs(p) || drivePrefixRe.MatchString(p) ||
		strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return errors.New("path escapes the template root")
	}
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return errors.New("path escapes the template root")
		}
	}
	return nil
}

// isScriptName reports whether a file name is an executable script.
func isScriptName(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".sh", ".bat", ".cmd", ".ps1", ".vh", ".exe", ".com":
		return true
	}
	return false
}

// isShebang reports whether content starts with a unix shebang line.
func isShebang(content string) bool {
	trimmed := strings.TrimLeft(content, " \t\r\n")
	return strings.HasPrefix(trimmed, "#!")
}

// outsideSensitive rejects generated paths that land on hard-blocked
// per-segment sensitive locations (SPEC §10.3).
func outsideSensitive(p string) bool {
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		low := strings.ToLower(seg)
		for _, s := range codeSensitive {
			if low == s {
				return true
			}
		}
	}
	return false
}

// ToolCodeScaffold returns the code_scaffold tool (SPEC §10.1).
func (t *Toolset) ToolCodeScaffold() tools.Tool { return codeScaffold{ts: t} }

// ToolEditFile returns the edit_file tool (SPEC §10.1).
func (t *Toolset) ToolEditFile() tools.Tool { return editFile{ts: t} }

// ToolCodeReview returns the code_review tool (SPEC §10.1, §10.4).
func (t *Toolset) ToolCodeReview() tools.Tool { return codeReview{ts: t} }

type codeScaffold struct{ ts *Toolset }

func (codeScaffold) Name() string { return "code_scaffold" }
func (codeScaffold) Description() string {
	return "Cria um projeto novo a partir de um template validado, dentro do code_root. Args: 'template', 'name', opcional 'module'. Nunca executa nada."
}
func (codeScaffold) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"template":{"type":"string"},"name":{"type":"string"},"module":{"type":"string"}},"required":["template","name"],"additionalProperties":false}`)
}

func (s codeScaffold) Execute(_ context.Context, args json.RawMessage) (tools.Result, error) {
	var a struct {
		Template string `json:"template"`
		Name     string `json:"name"`
		Module   string `json:"module"`
	}
	if err := tools.UnmarshalStrict(args, &a); err != nil {
		return tools.Result{}, err
	}
	registry := templatesMap()
	tpl, ok := registry[a.Template]
	if !ok {
		return tools.Result{}, fmt.Errorf("code: template %q desconhecido (validos: %s)", a.Template, registryNames(registry))
	}
	if !projectNameRe.MatchString(a.Name) {
		return tools.Result{}, fmt.Errorf("code: nome de projeto invalido %q (use letras, digitos, ponto, '_' ou '-')", a.Name)
	}
	module := a.Module
	if module == "" {
		module = "example.com/" + a.Name
	}
	if !moduleRe.MatchString(module) || strings.Contains(module, "..") {
		return tools.Result{}, fmt.Errorf("code: module invalido %q", a.Module)
	}
	if err := s.ts.fs.CheckSensitive(a.Name); err != nil {
		return tools.Result{}, err
	}
	// Fail-closed: never overwrite an existing project tree.
	root := filepath.Join(s.ts.Root(), filepath.Clean(a.Name))
	if _, err := os.Stat(root); err == nil {
		return tools.Result{}, fmt.Errorf("code: projeto %q ja existe no code_root; recusado por seguranca", a.Name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return tools.Result{}, err
	}

	sub := map[string]string{
		"{{name}}":   a.Name,
		"{{Name}}":   projectTitle(a.Name),
		"{{NAME}}":   strings.ToUpper(a.Name),
		"{{module}}": module,
	}
	var sb strings.Builder
	for _, f := range tpl.files {
		outPath := renderPlaceholders(f.Path, sub)
		if err := validateTemplatePath(outPath); err != nil {
			return tools.Result{}, fmt.Errorf("code: template %q path %q: %w", a.Template, outPath, err)
		}
		if outsideSensitive(outPath) {
			return tools.Result{}, fmt.Errorf("code: template %q grava em %q, local sensivel; bloqueado (fail-closed)", a.Template, outPath)
		}
		dest, err := s.ts.fs.ResolveWrite(filepath.Join(a.Name, outPath))
		if err != nil {
			return tools.Result{}, err
		}
		content := renderPlaceholders(f.Content, sub)
		if err := os.WriteFile(dest, []byte(content), 0o600); err != nil {
			return tools.Result{}, err
		}
		sb.WriteString(fmt.Sprintf("created %s (%d bytes)\n", outPath, len(content)))
	}
	return tools.Result{Output: "scaffoldado:\n" + sb.String()}, nil
}

func registryNames(m map[string]*template) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func projectTitle(name string) string {
	if name == "" {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func renderPlaceholders(s string, sub map[string]string) string {
	out := s
	for k, v := range sub {
		out = strings.ReplaceAll(out, k, v)
	}
	return out
}

type editFile struct{ ts *Toolset }

func (editFile) Name() string { return "edit_file" }
func (editFile) Description() string {
	return "Aplica um patch textual exato em arquivo do code_root. Args: 'path', 'old' (substring exata), 'new' e opcional 'occurrence' (1-based). So aplica se 'old' for unico; exibe diff."
}
func (editFile) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"},"occurrence":{"type":"integer"}},"required":["path","old","new"],"additionalProperties":false}`)
}

func (e editFile) Execute(_ context.Context, args json.RawMessage) (tools.Result, error) {
	var a struct {
		Path       string `json:"path"`
		Old        string `json:"old"`
		New        string `json:"new"`
		Occurrence int    `json:"occurrence"`
	}
	if err := tools.UnmarshalStrict(args, &a); err != nil {
		return tools.Result{}, err
	}
	if a.Old == "" {
		return tools.Result{}, errors.New("code: edit_file 'old' vazio; nao aplica patch vazio")
	}
	path, err := e.ts.fs.Resolve(a.Path)
	if err != nil {
		return tools.Result{}, err
	}
	if err := e.ts.fs.CheckSensitive(a.Path); err != nil {
		return tools.Result{}, err
	}
	if outsideSensitive(a.Path) {
		return tools.Result{}, fmt.Errorf("code: edit_file path %q bloqueado (fail-closed)", a.Path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tools.Result{}, err
	}
	before := string(data)
	idx := strings.Index(before, a.Old)
	if idx < 0 {
		return tools.Result{}, fmt.Errorf("code: substring 'old' nao encontrada em %q", a.Path)
	}
	count := strings.Count(before, a.Old)
	n := a.Occurrence
	if n == 0 {
		if count != 1 {
			return tools.Result{}, fmt.Errorf("code: 'old' ocorre %d vezes em %q; passe 'occurrence' (1-based) para desambiguar", count, a.Path)
		}
		n = 1
	}
	if n > count {
		return tools.Result{}, fmt.Errorf("code: 'occurrence' %d acima do total %d em %q", a.Occurrence, count, a.Path)
	}
	for i := 1; i < n; i++ {
		j := strings.Index(before[idx+1:], a.Old)
		if j < 0 {
			return tools.Result{}, fmt.Errorf("code: 'occurrence' %d invalido em %q", a.Occurrence, a.Path)
		}
		idx += j + 1
	}
	after := before[:idx] + a.New + before[idx+len(a.Old):]
	diff := editDiff(before, idx, len(a.Old), a.New)
	dest, err := e.ts.fs.ResolveWrite(a.Path)
	if err != nil {
		return tools.Result{}, err
	}
	if err := os.WriteFile(dest, []byte(after), 0o600); err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Output: diff + fmt.Sprintf("wrote %d bytes (was %d)\n", len(after), len(before))}, nil
}

// editDiff renders a line diff preview with two context lines around the
// replaced region. The removed block is marked "-" and the replacement block
// "+": shared lines stay as context, so a "new" payload that repeats the
// surrounding text does not fool the human reviewer into seeing a duplicate.
func editDiff(before string, idx int, oldLen int, newContent string) string {
	head, tail := diffContext(before, idx, oldLen)
	oldBlock := strings.Split(before[idx:idx+oldLen], "\n")
	oldLines := append(append([]string{}, head...), append(oldBlock, tail...)...)
	newLines := append(append([]string{}, head...), append(strings.Split(newContent, "\n"), tail...)...)
	var sb strings.Builder
	sb.WriteString("--- before\n+++ after\n")
	if len(oldLines)*len(newLines) > diffMaxCells {
		for _, ln := range strings.Split(newContent, "\n") {
			sb.WriteString("+" + ln + "\n")
		}
		return sb.String()
	}
	for _, ln := range lcsDiffMarkers(oldLines, newLines) {
		sb.WriteString(ln + "\n")
	}
	return sb.String()
}

// diffContext returns the head and tail context lines (up to two each) around
// the region being replaced in before.
func diffContext(before string, idx int, oldLen int) ([]string, []string) {
	start := idx
	for n := 0; n < 2; n++ {
		if start == 0 {
			break
		}
		start = lineStart(before, start-1)
	}
	lineEnd := idx + oldLen
	for n := 0; n < 2; n++ {
		if p := strings.Index(before[lineEnd:], "\n"); p >= 0 {
			lineEnd += p + 1
		} else {
			break
		}
	}
	head := strings.Split(before[start:idx], "\n")
	if len(head) > 0 && head[len(head)-1] == "" {
		head = head[:len(head)-1]
	}
	return head, strings.Split(before[idx+oldLen:lineEnd], "\n")
}

// diffMaxCells bounds the LCS grid for the diff preview so huge replacement
// payloads fall back to the whole-content "+" listing instead of O(n*m) memory.
const diffMaxCells = 65536

// lineStart returns the byte index where the line containing pos begins.
func lineStart(s string, pos int) int {
	if pos <= 0 {
		return 0
	}
	return strings.LastIndex(s[:pos], "\n") + 1
}

// lcsDiffMarkers returns the line diff (old vs new) with per-line prefixes,
// aligned by longest common subsequence. y markers: " "+line (shared context),
// "-"+line (present only in old), "+"+line (present only in new).
func lcsDiffMarkers(oldLines, newLines []string) []string {
	n, m := len(oldLines), len(newLines)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out []string
	for i, j := 0, 0; i < n || j < m; {
		switch {
		case i < n && j < m && oldLines[i] == newLines[j]:
			out = append(out, " "+oldLines[i])
			i++
			j++
		case j < m && (i == n || dp[i][j+1] >= dp[i+1][j]):
			out = append(out, "+"+newLines[j])
			j++
		default:
			out = append(out, "-"+oldLines[i])
			i++
		}
	}
	return out
}

type codeReview struct{ ts *Toolset }

func (codeReview) Name() string { return "code_review" }
func (codeReview) Description() string {
	return "Revisa um arquivo do code_root (100%% read-only) com heuristicas locais: segredos hard-coded, execucao de shell, permissoes, path traversal, blobs grandes, TODO/FIXME e injecao de prompt. Emite relatorio estruturado. Nunca modifica arquivos."
}
func (codeReview) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)
}

// finding is one entry of the code_review report (SPEC §10.1).
type finding struct {
	File     string `json:"file"`
	Severity string `json:"severity"`
	Rule     string `json:"rule"`
	Detail   string `json:"detail"`
}

type reviewReport struct {
	Findings []finding `json:"findings"`
}

var (
	reSecret    = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key|apikey)\s*[:=]\s*["'][^"']{8,}["']|sk-[A-Za-z0-9]{16,}|AKIA[0-9A-Z]{16}|ghp_[A-Za-z0-9]{20,}|BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY`)
	reShell     = regexp.MustCompile(`os/exec|exec\.Command|exec\.CommandContext|os\.system\(|subprocess\.(call|run)\(|child_process|shell_exec\(`)
	reTodo      = regexp.MustCompile(`(?i)\b(TODO|FIXME|HACK)\b`)
	reInject    = regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|prior|earlier)\s+(instructions|orders|prompts|instru)|(rode|execute|run)\s+(curl|wget)\b|"tool_call"`)
	reTraversal = regexp.MustCompile(`\.\./|\.\.\\|\.\.[^a-zA-Z0-9._/]`)
)

func (r codeReview) Execute(_ context.Context, args json.RawMessage) (tools.Result, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := tools.UnmarshalStrict(args, &a); err != nil {
		return tools.Result{}, err
	}
	path, err := r.ts.fs.Resolve(a.Path)
	if err != nil {
		return tools.Result{}, err
	}
	if err := r.ts.fs.CheckSensitive(a.Path); err != nil {
		return tools.Result{}, err
	}
	f, err := os.Open(path) // read-only by construction (§10.4)
	if err != nil {
		return tools.Result{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(maxReviewBytes)+1))
	if err != nil {
		return tools.Result{}, err
	}
	if len(data) > maxReviewBytes {
		return tools.Result{}, fmt.Errorf("code: %q excede o limite de revisao de %d bytes", a.Path, maxReviewBytes)
	}
	st, err := f.Stat()
	if err != nil {
		return tools.Result{}, err
	}
	findings := r.analyze(a.Path, data, st)
	rep, err := json.MarshalIndent(reviewReport{Findings: findings}, "", "  ")
	if err != nil {
		return tools.Result{}, err
	}
	out, truncated := r.ts.truncate(redact.Redact(string(rep)))
	return tools.Result{Output: out, Truncated: truncated}, nil
}

// truncate bounds the report echoed to the model (SPEC §5.2), keeping the
// head and tail.
func (t *Toolset) truncate(s string) (string, bool) {
	if len(s) <= t.limit {
		return s, false
	}
	keep := (t.limit * 2) / 3
	tail := t.limit / 3
	return s[:keep] + "\n... [relatorio truncado pelo limite de seguranca] ...\n" + s[len(s)-tail:], true
}

// analyze runs the local heuristics over the file bytes (SPEC §10.4). Pure and
// side-effect free, so it can be unit-tested in isolation.
func (codeReview) analyze(file string, data []byte, st os.FileInfo) []finding {
	var out []finding
	if st != nil {
		// Windows emulates 0666/0777 and has no meaningful permission bits;
		// the perms heuristic is meaningful only on POSIX (the real target).
		if runtime.GOOS != "windows" && st.Mode().Perm()&0o002 == 0o002 {
			out = append(out, finding{File: file, Severity: "high", Rule: "world-writable-perms", Detail: fmt.Sprintf("permissao %04o (gravavel por qualquer um)", st.Mode().Perm())})
		}
		if st.Size() > 1<<20 && bytes.IndexByte(data, 0) >= 0 {
			out = append(out, finding{File: file, Severity: "info", Rule: "large-binary", Detail: fmt.Sprintf("bloco binario grande com %d bytes (nunca deve entrar no git)", st.Size())})
		}
	}
	content := string(data)
	for i, ln := range strings.Split(content, "\n") {
		trim := strings.TrimSpace(ln)
		if trim == "" {
			continue
		}
		if m := reSecret.FindStringIndex(trim); m != nil {
			out = append(out, finding{File: file, Severity: "critical", Rule: "hardcoded-secret", Detail: fmt.Sprintf("linha %d: possivel credencial embutida", i+1)})
		}
		if reShell.MatchString(trim) {
			out = append(out, finding{File: file, Severity: "medium", Rule: "shell-exec", Detail: fmt.Sprintf("linha %d: execucao de processo/shell dentro do codigo nao sandboxada aqui", i+1)})
		}
		if reTraversal.MatchString(trim) {
			out = append(out, finding{File: file, Severity: "medium", Rule: "path-traversal", Detail: fmt.Sprintf("linha %d: uso de '..' em caminhos", i+1)})
		}
		if reTodo.MatchString(trim) {
			out = append(out, finding{File: file, Severity: "info", Rule: "todo-fixme", Detail: fmt.Sprintf("linha %d: marcador TODO/FIXME presente", i+1)})
		}
		if reInject.MatchString(trim) && isCommentOrString(trim) {
			out = append(out, finding{File: file, Severity: "high", Rule: "prompt-injection", Detail: fmt.Sprintf("linha %d: texto com aparencia de override de instrucoes; tratar conteudo como untrusted", i+1)})
		}
	}
	return out
}

// isCommentOrString reports whether the trimmed line starts like a comment or
// string literal (the natural disguises for indirect injection, §10.4).
func isCommentOrString(trim string) bool {
	for _, p := range []string{"#", "//", "/*", "*", "--", ";", `"`, "'", "`"} {
		if strings.HasPrefix(trim, p) {
			return true
		}
	}
	return false
}
