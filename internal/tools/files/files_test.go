package files

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"ancientai/internal/redact"
	"ancientai/internal/tools"
)

func setup(t *testing.T) (*Files, string) {
	t.Helper()
	root := t.TempDir()
	fs := NewFiles(root, 65536)
	if err := os.MkdirAll(filepath.Join(root, "sub", "deep"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("ola mundo"), 0o600); err != nil {
		t.Fatal(err)
	}
	return fs, root
}

func TestWriteThenReadRoundTrip(t *testing.T) {
	fs, _ := setup(t)
	if _, err := fs.ToolWriteFile().Execute(context.Background(), tools.Raw(`{"path":"sub/novo.txt","content":"x"}`)); err != nil {
		t.Fatal(err)
	}
	res, err := fs.ToolReadFile().Execute(context.Background(), tools.Raw(`{"path":"sub/novo.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "x" {
		t.Fatalf("round trip = %q", res.Output)
	}
}

func TestListDir(t *testing.T) {
	fs, _ := setup(t)
	res, err := fs.ToolListDir().Execute(context.Background(), tools.Raw(`{"path":"."}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "hello.txt\tfile") || !strings.Contains(res.Output, "sub\tdir") {
		t.Fatalf("list_dir output = %q", res.Output)
	}
}

// Security: path traversal via .. must be confined to the root.
func TestRejectPathTraversal(t *testing.T) {
	fs, root := setup(t)
	outside := filepath.Join(filepath.Dir(root), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, targ := range []string{`{"path":"../outside.txt"}`, `{"path":"../../etc/passwd"}`, `{"path":".."}`} {
		if _, err := fs.ToolReadFile().Execute(context.Background(), tools.Raw(targ)); err == nil {
			t.Errorf("read traversal allowed: %s", targ)
		}
	}
	if _, err := fs.ToolWriteFile().Execute(context.Background(), tools.Raw(`{"path":"../pwned.txt","content":"x"}`)); err == nil {
		t.Fatal("write traversal allowed")
	}
	if _, err := fs.ToolListDir().Execute(context.Background(), tools.Raw(`{"path":"../.."}`)); err == nil {
		t.Fatal("list_dir traversal allowed")
	}
}

// Security: a write into a symlinked directory that escapes the root.
func TestRejectSymlinkEscape(t *testing.T) {
	fs, root := setup(t)
	outside := filepath.Join(filepath.Dir(root), "outdir")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "sensitive.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not supported here: %v", err)
	}
	if _, err := fs.ToolReadFile().Execute(context.Background(), tools.Raw(`{"path":"link/sensitive.txt"}`)); err == nil {
		t.Fatal("read through escaping symlink allowed")
	}
	if _, err := fs.ToolWriteFile().Execute(context.Background(), tools.Raw(`{"path":"link/pwned.txt","content":"x"}`)); err == nil {
		t.Fatal("write through escaping symlink allowed")
	}
}

// Security: sensitive paths are blocked fail-closed even inside the root.
func TestRejectSensitivePaths(t *testing.T) {
	fs, root := setup(t)
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=sk-abcdefGHIJKLMNOPQRSTUVWXyz123456"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "backup", ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, targ := range []string{
		`{"path":".env"}`,
		`{"path":"backup/.ssh/id_rsa"}`,
		`{"path":"backup/.aws/credentials"}`,
	} {
		if _, err := fs.ToolReadFile().Execute(context.Background(), tools.Raw(targ)); err == nil {
			t.Errorf("sensitive read allowed: %s", targ)
		}
	}
	if _, err := fs.ToolWriteFile().Execute(context.Background(), tools.Raw(`{"path":"backup/.ssh/evil","content":"x"}`)); err == nil {
		t.Fatal("sensitive write allowed")
	}
}

// Security: read output is redacted before returning.
func TestReadRedactsSecrets(t *testing.T) {
	fs, root := setup(t)
	f := filepath.Join(root, "conf.log")
	if err := os.WriteFile(f, []byte("key=sk-abcdefGHIJKLMNOPQRSTUVWXyz123456"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := fs.ToolReadFile().Execute(context.Background(), tools.Raw(`{"path":"conf.log"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Output, "sk-abcdefGHIJKLMNOPQRSTUVWXyz123456") {
		t.Fatal("secret leaked verbatim in read output")
	}
	if len(redact.FindSecrets(res.Output)) != 0 {
		t.Fatalf("read output still contains secret pattern: %q", res.Output)
	}
}

// Security: big writes are rejected (bounds on model-controlled content).
func TestRejectOversizedWrite(t *testing.T) {
	fs, _ := setup(t)
	content := strings.Repeat("a", maxContentBytes+1)
	_, err := fs.ToolWriteFile().Execute(context.Background(),
		tools.Raw(`{"path":"big.txt","content":`+strconv.Quote(content)+`}`))
	if err == nil {
		t.Fatal("oversized write accepted")
	}
}

// Security: strict args reject unknown fields, double-encoded tricks.
func TestStrictArgsRead(t *testing.T) {
	fs, _ := setup(t)
	for _, targ := range []string{
		`{"path":"hello.txt","extra":1}`,
		`{"path":123}`,
		`{"path":"hello.txt"} trailing`,
	} {
		if _, err := fs.ToolReadFile().Execute(context.Background(), tools.Raw(targ)); err == nil {
			t.Errorf("strict args accepted: %s", targ)
		}
	}
}

// Security: reading a real sensitive system path fails closed.
func TestSystemSensitivePathsBlocked(t *testing.T) {
	fs, _ := setup(t)
	if _, err := fs.ToolReadFile().Execute(context.Background(), tools.Raw(`{"path":"/etc/shadow"}`)); err == nil {
		t.Fatal("system sensitive path read allowed")
	}
}

func TestIsSensitivePath(t *testing.T) {
	for _, p := range []string{".env", ".env.prod", ".ssh/id_rsa", ".aws/credentials", "x/.git-credentials", ".envary"} {
		if !IsSensitivePath(p) {
			t.Errorf("%q should be sensitive", p)
		}
	}
	for _, p := range []string{"env.example", "notes.txt", "sub/file.go"} {
		if IsSensitivePath(p) {
			t.Errorf("%q should not be sensitive", p)
		}
	}
}
