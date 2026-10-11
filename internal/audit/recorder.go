// Package audit provides an append-only JSONL audit logger (SPEC §5.7).
// Every tool call — executed, denied or timed out — produces one immutable
// entry with the exact vector, status, output hash and the operator decision.
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry is a single audit event (SPEC §5.7). The output itself is never
// stored — only its SHA-256 hash, preserving confidentiality.
type Entry struct {
	Timestamp  string `json:"timestamp"`
	Tool       string `json:"tool"`
	Args       string `json:"args"`
	Status     string `json:"status"`
	ExitCode   int    `json:"exit_code,omitempty"`
	OutputHash string `json:"output_hash"`
	Policy     string `json:"policy"`
	Decision   string `json:"decision"`
	Duration   string `json:"duration,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

// HashOutput returns the hex-encoded SHA-256 of data.
func HashOutput(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

// Recorder writes append-only JSONL audit entries.  Files are created with
// permission 0600 and rotated by size (SPEC §5.7).
type Recorder struct {
	mu        sync.Mutex
	f         *os.File
	path      string
	maxBytes  int64
	keepCount int
}

// NewRecorder opens (or creates) the audit file at path with 0600 perms.
// maxBytes triggers rotation when exceeded; keepCount controls how many
// rotated files are retained (0 means no rotation).
func NewRecorder(path string, maxBytes int64, keepCount int) (*Recorder, error) {
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024 // 10 MiB default
	}
	if keepCount < 0 {
		keepCount = 0
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("audit: mkdir %s: %w", dir, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit: open %s: %w", path, err)
	}
	return &Recorder{f: f, path: path, maxBytes: maxBytes, keepCount: keepCount}, nil
}

// Record appends one entry.  It is safe for concurrent use.
// Path returns the audit file this recorder writes to.
//
// It exists so a test can prove the path does NOT follow the project root when
// /cd changes it (M15). An audit log that moved with the working directory
// would open a fresh trail exactly when the operator widened the blast radius —
// which is the one moment the log matters most. Reading the path is safe: it is
// a filename, never content, and never a secret.
func (r *Recorder) Path() string {
	if r == nil {
		return ""
	}
	return r.path
}

func (r *Recorder) Record(entry Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("audit: marshal: %w", err)
	}
	line = append(line, '\n')

	if err := r.rotateLocked(); err != nil {
		return err
	}
	_, err = r.f.Write(line)
	return err
}

// rotateLocked rotates the file if it exceeds maxBytes and keepCount > 0.
func (r *Recorder) rotateLocked() error {
	if r.keepCount <= 0 {
		return nil
	}
	fi, err := r.f.Stat()
	if err != nil || fi.Size() < r.maxBytes {
		return nil
	}
	r.f.Close()
	for i := r.keepCount - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", r.path, i)
		dst := fmt.Sprintf("%s.%d", r.path, i+1)
		os.Rename(src, dst)
	}
	os.Rename(r.path, r.path+".1")
	r.f, err = os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	return err
}

// Close closes the underlying file.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f != nil {
		return r.f.Close()
	}
	return nil
}

// ReadEntries reads all audit entries from path (for testing/inspection).
func ReadEntries(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var entries []Entry
	dec := json.NewDecoder(f)
	for {
		var e Entry
		if err := dec.Decode(&e); err == io.EOF {
			break
		} else if err != nil {
			return entries, fmt.Errorf("audit: decode: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}
