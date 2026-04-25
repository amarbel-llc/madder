//go:build test

package env_dir

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/delta/compression_type"
)

func TestMmapSource_LocalFileIdentityWrappers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	payload := []byte("hello mmap world")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	br, err := NewFileReaderOrErrNotExist(DefaultConfig, path)
	if err != nil {
		t.Fatal(err)
	}
	defer br.Close()

	// NewFileReaderOrErrNotExist returns the BlobReader interface, so
	// the MmapSource capability has to be discovered via type-assert.
	// (Tests that go through NewReader directly skip this dance.)
	src, ok := br.(domain_interfaces.MmapSource)
	if !ok {
		t.Fatal("blobReader should implement MmapSource")
	}
	file, off, length, mmapOk, err := src.MmapSource()
	if err != nil {
		t.Fatal(err)
	}
	if !mmapOk {
		t.Fatal("expected ok=true for local file with default config")
	}
	if off != 0 {
		t.Fatalf("offset: got %d want 0", off)
	}
	if length != int64(len(payload)) {
		t.Fatalf("length: got %d want %d", length, len(payload))
	}
	if file == nil {
		t.Fatal("file is nil")
	}
}

func TestMmapSource_BytesReader(t *testing.T) {
	br, err := NewReader(DefaultConfig, bytes.NewReader([]byte("hi")))
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, mmapOk, err := br.MmapSource()
	if err != nil {
		t.Fatal(err)
	}
	if mmapOk {
		t.Fatal("expected ok=false for non-*os.File reader")
	}
}

func TestMmapSource_ZstdCompression(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	if err := os.WriteFile(path, []byte("zstd content"), 0o644); err != nil {
		t.Fatal(err)
	}

	zstd := compression_type.CompressionTypeZstd
	cfg := DefaultConfig
	cfg.compression = &zstd

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	br, err := NewReader(cfg, f)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, mmapOk, err := br.MmapSource()
	if err != nil {
		t.Fatal(err)
	}
	if mmapOk {
		t.Fatal("expected ok=false for zstd-configured reader")
	}
}
