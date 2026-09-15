package audit

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRecordAppendsEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	rec, err := NewRecorder(path, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	if err := rec.Record(Entry{Tool: "shell", Args: `{"cmd":"ls"}`, Status: "executed", Policy: "ask", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
	if err := rec.Record(Entry{Tool: "shell", Args: `{"cmd":"rm"}`, Status: "denied", Policy: "ask", Decision: "denied"}); err != nil {
		t.Fatal(err)
	}
	rec.Close()

	entries, err := ReadEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Tool != "shell" || entries[0].Status != "executed" {
		t.Fatalf("entry 0 wrong: %+v", entries[0])
	}
	if entries[1].Status != "denied" {
		t.Fatalf("entry 1 wrong: %+v", entries[1])
	}
}

func TestRecordTimestampAutoFilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	rec, err := NewRecorder(path, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	if err := rec.Record(Entry{Tool: "read_file"}); err != nil {
		t.Fatal(err)
	}
	rec.Close()
	entries, _ := ReadEntries(path)
	if entries[0].Timestamp == "" {
		t.Fatal("timestamp not auto-filled")
	}
}

func TestHashOutputDeterministic(t *testing.T) {
	a := HashOutput("hello")
	b := HashOutput("hello")
	if a != b || a == "" {
		t.Fatalf("HashOutput not deterministic: %q != %q", a, b)
	}
	c := HashOutput("world")
	if a == c {
		t.Fatal("different inputs produced same hash")
	}
}

func TestRotationBySize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	rec, err := NewRecorder(path, 100, 3) // rotate at 100 bytes, keep 3
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	// Write enough entries to trigger rotation.
	for i := 0; i < 50; i++ {
		rec.Record(Entry{Tool: "shell", Args: `{"cmd":"echo "}` + string(rune('A'+i%26)), Status: "executed"})
	}
	rec.Close()

	// Archive .1 should exist.
	if _, err := os.Stat(path + ".1"); os.IsNotExist(err) {
		t.Fatal("rotation did not create archive .1")
	}
}

func TestFilePerms0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforced on Windows")
	}
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	rec, err := NewRecorder(path, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	rec.Close()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	perm := fi.Mode().Perm()
	if perm != 0o600 {
		t.Fatalf("audit file perms = %o, want 0600", perm)
	}
}

func TestAuditRecordsDenials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	rec, err := NewRecorder(path, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	rec.Record(Entry{Tool: "shell", Args: `{"cmd":"rm -rf /"}`, Status: "denied", Policy: "deny", Decision: "denied"})
	rec.Record(Entry{Tool: "write_file", Args: `{"path":"a.txt","content":"x"}`, Status: "denied", Policy: "ask", Decision: "denied"})
	rec.Close()

	entries, _ := ReadEntries(path)
	if len(entries) != 2 {
		t.Fatalf("expected 2 denial entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Status != "denied" {
			t.Fatalf("expected denied status, got %q", e.Status)
		}
	}
}
