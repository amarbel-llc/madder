//go:build test && unix

package mmap_blob

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/echo/env_dir"
)

// writeBlobAndDigest materializes payload through env_dir.NewWriter so the
// returned MarklId matches the on-disk file bytes (DefaultConfig is identity
// wrappers, so the file == payload byte-for-byte).
func writeBlobAndDigest(t *testing.T, path string, payload []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w, err := env_dir.NewWriter(env_dir.DefaultConfig, f)
	if err != nil {
		f.Close()
		t.Fatal(err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestVerify_Match(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	payload := []byte("verify happy path payload")
	writeBlobAndDigest(t, path, payload)

	br, err := env_dir.NewFileReaderOrErrNotExist(env_dir.DefaultConfig, path)
	if err != nil {
		t.Fatal(err)
	}
	// Drain the reader to compute the digest, then capture it.
	if _, err := br.WriteTo(io_Discard{}); err != nil {
		t.Fatal(err)
	}
	marklId := br.GetMarklId()

	rf, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, err := rf.Stat()
	if err != nil {
		t.Fatal(err)
	}
	mb, err := mmapFile(rf, 0, stat.Size(), marklId)
	if err != nil {
		t.Fatal(err)
	}
	defer mb.Close()
	defer br.Close()

	if err := mb.Verify(); err != nil {
		t.Fatalf("Verify on intact blob: %v", err)
	}
}

func TestVerify_Mismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	payload := []byte("the original blob content")
	writeBlobAndDigest(t, path, payload)

	br, err := env_dir.NewFileReaderOrErrNotExist(env_dir.DefaultConfig, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := br.WriteTo(io_Discard{}); err != nil {
		t.Fatal(err)
	}
	marklId := br.GetMarklId()
	br.Close()

	// Tamper: overwrite the file with different content of the same length.
	tampered := bytes.Repeat([]byte("X"), len(payload))
	if err := os.WriteFile(path, tampered, 0o644); err != nil {
		t.Fatal(err)
	}

	rf, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, err := rf.Stat()
	if err != nil {
		t.Fatal(err)
	}
	mb, err := mmapFile(rf, 0, stat.Size(), marklId)
	if err != nil {
		t.Fatal(err)
	}
	defer mb.Close()

	err = mb.Verify()
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("Verify on tampered blob: got %v, want ErrDigestMismatch", err)
	}
}

func TestVerify_NilMarklId(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	if err := os.WriteFile(path, []byte("anything"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mb, err := mmapFile(f, 0, 8, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer mb.Close()
	if err := mb.Verify(); err != nil {
		t.Fatalf("Verify with nil MarklId: %v", err)
	}
}

// io_Discard is a tiny local io.Writer drain to avoid an extra import.
type io_Discard struct{}

func (io_Discard) Write(p []byte) (int, error) { return len(p), nil }
