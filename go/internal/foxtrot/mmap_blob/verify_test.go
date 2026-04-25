//go:build test && unix

package mmap_blob

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/echo/env_dir"
)

// writeBlobAndDigest materializes payload through env_dir.NewWriter so the
// on-disk file bytes match payload byte-for-byte (DefaultConfig is identity
// wrappers) and returns the MarklId the writer computed.
func writeBlobAndDigest(t *testing.T, path string, payload []byte) domain_interfaces.MarklId {
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
	return w.GetMarklId()
}

// mmapAt opens path and mmaps its full contents under marklId.
func mmapAt(t *testing.T, path string, marklId domain_interfaces.MarklId) MmapBlob {
	t.Helper()
	rf, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, err := rf.Stat()
	if err != nil {
		rf.Close()
		t.Fatal(err)
	}
	mb, err := mmapFile(rf, 0, stat.Size(), marklId)
	if err != nil {
		rf.Close()
		t.Fatal(err)
	}
	return mb
}

func TestVerify_Match(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	marklId := writeBlobAndDigest(t, path, []byte("verify happy path payload"))

	mb := mmapAt(t, path, marklId)
	defer mb.Close()

	if err := mb.Verify(); err != nil {
		t.Fatalf("Verify on intact blob: %v", err)
	}
}

func TestVerify_Mismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	payload := []byte("the original blob content")
	marklId := writeBlobAndDigest(t, path, payload)

	tampered := bytes.Repeat([]byte("X"), len(payload))
	if err := os.WriteFile(path, tampered, 0o644); err != nil {
		t.Fatal(err)
	}

	mb := mmapAt(t, path, marklId)
	defer mb.Close()

	if err := mb.Verify(); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("Verify on tampered blob: got %v, want ErrDigestMismatch", err)
	}
}

func TestVerify_NilMarklId(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	if err := os.WriteFile(path, []byte("anything"), 0o644); err != nil {
		t.Fatal(err)
	}

	mb := mmapAt(t, path, nil)
	defer mb.Close()

	if err := mb.Verify(); err != nil {
		t.Fatalf("Verify with nil MarklId: %v", err)
	}
}
