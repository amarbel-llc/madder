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

func TestMakeMmapBlob_LocalFileIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	payload := []byte("the quick brown fox")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	br, err := env_dir.NewFileReaderOrErrNotExist(env_dir.DefaultConfig, path)
	if err != nil {
		t.Fatal(err)
	}
	mb, err := MakeMmapBlobFromBlobReader(br)
	if err != nil {
		t.Fatal(err)
	}
	defer mb.Close()
	if !bytes.Equal(mb.Bytes(), payload) {
		t.Fatalf("Bytes() pre-Close: got %q want %q", mb.Bytes(), payload)
	}
	// After successful promotion, br.Close() must not double-close
	// the underlying file (mb owns it now).
	if err := br.Close(); err != nil {
		t.Fatalf("br.Close after promotion: %v", err)
	}
	// And the mmap'd view must survive the source-reader Close —
	// that's the practical contract callers depend on.
	if !bytes.Equal(mb.Bytes(), payload) {
		t.Fatalf("Bytes() post-Close: got %q want %q", mb.Bytes(), payload)
	}
}

func TestMakeMmapBlob_BytesReader(t *testing.T) {
	br, err := env_dir.NewReader(env_dir.DefaultConfig, bytes.NewReader([]byte("hi")))
	if err != nil {
		t.Fatal(err)
	}
	_, err = MakeMmapBlobFromBlobReader(br)
	if !errors.Is(err, ErrMmapUnsupported) {
		t.Fatalf("got %v, want ErrMmapUnsupported", err)
	}
}
