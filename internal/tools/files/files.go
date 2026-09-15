// Package files implements the read_file, write_file and list_dir tools
// (SPEC §3.2, §5.6.3, §7.1). Every path is resolved against a fixed root;
// escapes via "..", absolute paths outside the root, symlinks and sensitive
// paths are rejected fail-closed.
package files

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ancientai/internal/redact"
	"ancientai/internal/tools"
)

// maxContentBytes bounds a single write payload (the model controls content).
const maxContentBytes = 8 << 20 // 8 MiB

// sensitiveSegments are path components whose read/write is blocked by
// default, per SPEC §5.6.3 (fail-closed on sensitive paths).
var sensitiveSegments = []string{
	".ssh", ".aws", ".gnupg", ".git-credentials", ".kube", ".config",
	"shadow", "passwd", "opaque-oracle",
}

// Files groups the three filesystem tools around one confined root.
type Files struct {
	root  string
	limit int
}

// NewFiles returns the filesystem tools rooton. limit is the maximum number
// of output bytes returned to the model (SPEC §5.2).
func NewFiles(root string, limit int) *Files {
	if limit <= 0 {
		limit = 65536
	}
	return &Files{root: root, limit: limit}
}

// Root returns the confined root directory.
func (f *Files) Root() string { return f.root }

// ToolReadFile returns the read_file tool.
func (f *Files) ToolReadFile() tools.Tool { return readFile{fs: f} }

// ToolWriteFile returns the write_file tool.
func (f *Files) ToolWriteFile() tools.Tool { return writeFile{fs: f} }

// ToolListDir returns the list_dir tool.
func (f *Files) ToolListDir() tools.Tool { return listDir{fs: f} }

func (f *Files) within(p string) bool {
	rel, err := filepath.Rel(f.root, p)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// IsSensitivePath reports whether any component of p is a sensitive location
// (SPEC §5.6.3). It is exported so the loop can reuse the same judgement.
func IsSensitivePath(p string) bool {
	norm := filepath.ToSlash(p)
	for _, seg := range nilSafe(norm) {
		low := strings.ToLower(seg)
		if strings.HasPrefix(low, ".env") {
			return true
		}
		for _, s := range sensitiveSegments {
			if low == s {
				return true
			}
		}
	}
	return false
}

func nilSafe(norm string) []string {
	if norm == "" {
		return nil
	}
	return strings.Split(norm, "/")
}

// resolve validates and resolves a user-supplied path against the root for
// read-only operations. The final resolved path (after symlinks) must stay
// inside the root.
func (f *Files) resolve(rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", errors.New("files: path is required")
	}
	clean := filepath.Clean(filepath.Join(f.root, rel))
	if !f.within(clean) {
		return "", fmt.Errorf("files: path %q escapes the root %q", rel, f.root)
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", err
	}
	if !f.within(resolved) {
		return "", fmt.Errorf("files: path %q resolves outside the root %q (symlink)", rel, f.root)
	}
	return resolved, nil
}

// resolveWrite is resolve for write operations: the parent directory is
// created (inside the root) and then re-checked against symlink escapes.
func (f *Files) resolveWrite(rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", errors.New("files: path is required")
	}
	clean := filepath.Clean(filepath.Join(f.root, rel))
	if !f.within(clean) {
		return "", fmt.Errorf("files: path %q escapes the root %q", rel, f.root)
	}
	dir := filepath.Dir(clean)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	rdir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	if !f.within(rdir) {
		return "", fmt.Errorf("files: path %q resolves outside the root %q (symlink)", rel, f.root)
	}
	return filepath.Join(rdir, filepath.Base(clean)), nil
}

func (f *Files) checkSensitive(rel string) error {
	if IsSensitivePath(rel) {
		return fmt.Errorf("files: path %q is a sensitive location and is blocked (fail-closed)", rel)
	}
	if IsSensitivePath(filepath.Join(f.root, rel)) {
		return fmt.Errorf("files: path %q resolves into a sensitive location and is blocked (fail-closed)", rel)
	}
	return nil
}

func (f *Files) truncate(s string) (string, bool) {
	if len(s) <= f.limit {
		return s, false
	}
	keep := (f.limit * 2) / 3
	tail := f.limit / 3
	return s[:keep] + "\n... [saida truncada pelo limite de seguranca] ...\n" + s[len(s)-tail:], true
}

type readFile struct{ fs *Files }

func (readFile) Name() string { return "read_file" }
func (readFile) Description() string {
	return "Le um arquivo dentro do diretorio de trabalho. Arg 'path' e relativo a raiz."
}
func (readFile) Parameters() json.RawMessage { return pathSchema }
func (r readFile) Execute(_ context.Context, args json.RawMessage) (tools.Result, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := tools.UnmarshalStrict(args, &a); err != nil {
		return tools.Result{}, err
	}
	path, err := r.fs.resolve(a.Path)
	if err != nil {
		return tools.Result{}, err
	}
	if err := r.fs.checkSensitive(a.Path); err != nil {
		return tools.Result{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return tools.Result{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(r.fs.limit)+1))
	if err != nil {
		return tools.Result{}, err
	}
	// Redaction before the content can be echoed to the model (§5.6.3).
	out, truncated := r.fs.truncate(redact.Redact(string(data)))
	return tools.Result{Output: out, Truncated: truncated}, nil
}

type writeFile struct{ fs *Files }

func (writeFile) Name() string { return "write_file" }
func (writeFile) Description() string {
	return "Escreve um arquivo dentro do diretorio de trabalho. Args 'path' (relativo) e 'content'."
}
func (writeFile) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`)
}

func (w writeFile) Execute(_ context.Context, args json.RawMessage) (tools.Result, error) {
	var a struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := tools.UnmarshalStrict(args, &a); err != nil {
		return tools.Result{}, err
	}
	if len(a.Content) > maxContentBytes {
		return tools.Result{}, fmt.Errorf("files: write_file content exceeds the %d byte limit", maxContentBytes)
	}
	if err := w.fs.checkSensitive(a.Path); err != nil {
		return tools.Result{}, err
	}
	path, err := w.fs.resolveWrite(a.Path)
	if err != nil {
		return tools.Result{}, err
	}
	if err := os.WriteFile(path, []byte(a.Content), 0o600); err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Output: fmt.Sprintf("wrote %d bytes", len(a.Content))}, nil
}

type listDir struct{ fs *Files }

func (listDir) Name() string { return "list_dir" }
func (listDir) Description() string {
	return "Lista as entradas de um diretorio dentro da raiz. Arg 'path' e relativo."
}
func (listDir) Parameters() json.RawMessage { return pathSchema }
func (l listDir) Execute(_ context.Context, args json.RawMessage) (tools.Result, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := tools.UnmarshalStrict(args, &a); err != nil {
		return tools.Result{}, err
	}
	path, err := l.fs.resolve(a.Path)
	if err != nil {
		return tools.Result{}, err
	}
	if err := l.fs.checkSensitive(a.Path); err != nil {
		return tools.Result{}, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return tools.Result{}, err
	}
	var sb strings.Builder
	for _, e := range entries {
		typ := "file"
		if e.IsDir() {
			typ = "dir"
		}
		sb.WriteString(e.Name())
		sb.WriteByte('\t')
		sb.WriteString(typ)
		sb.WriteByte('\n')
	}
	return tools.Result{Output: sb.String()}, nil
}

var pathSchema = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)
