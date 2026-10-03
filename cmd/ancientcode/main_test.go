package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Session cleanup is explicit-only: never automatic, only .jsonl session files
// older than the cutoff are removed (SPEC M5 "limpeza sem auto-executar nada").
func TestCleanupSessionsRemovesOnlyOldJSONL(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "work")
	sess := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sess, 0o700); err != nil {
		t.Fatal(err)
	}

	old := filepath.Join(sess, "old.jsonl")
	if err := os.WriteFile(old, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().AddDate(0, 0, -10)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	fresh := filepath.Join(sess, "fresh.jsonl")
	os.WriteFile(fresh, []byte("y"), 0o600)

	other := filepath.Join(sess, "notes.txt")
	os.WriteFile(other, []byte("keep"), 0o600)

	removed, err := cleanupSessions(dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != old {
		t.Fatalf("removed = %v, want [%s]", removed, old)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("fresh session was removed")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("non-session file was removed")
	}
}

func TestCleanupSessionsMissingDirIsNoop(t *testing.T) {
	removed, err := cleanupSessions(t.TempDir()+string(os.PathSeparator)+"nope", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Fatalf("unexpected removals: %v", removed)
	}
}

func TestCleanupSessionsZeroDaysIsNoop(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "work", "sessions")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "old.jsonl"), []byte("x"), 0o600)
	removed, err := cleanupSessions(filepath.Dir(dir), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Fatalf("cleanup with days=0 removed files: %v", removed)
	}
}
