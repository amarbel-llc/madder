//go:build test && unix

package mmap_blob

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestMmapFile_Bytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	payload := []byte("zero copy hello world")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mb, err := mmapFile(f, 0, int64(len(payload)), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer mb.Close()
	if !bytes.Equal(mb.Bytes(), payload) {
		t.Fatalf("bytes mismatch: got %q want %q", mb.Bytes(), payload)
	}
}

func TestMmapFile_CloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mb, err := mmapFile(f, 0, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := mb.Close(); err != nil {
		t.Fatal(err)
	}
	if err := mb.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
