//go:build test && unix

package blob_stores

import (
	"bytes"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/foxtrot/mmap_blob"
)

// TestMakeBlobReaderPromotesToMmap exercises the public path that
// library/embedding consumers will use: open a localHashBucketed store,
// write a payload through MakeBlobWriter, read it back via
// MakeBlobReader, promote that BlobReader via
// mmap_blob.MakeMmapBlobFromBlobReader, and assert Bytes() matches the
// original payload byte-for-byte.
//
// This is the integration counterpart to the unit-level negative cases
// in env_dir/blob_reader_mmap_test.go and mmap_blob/promote_test.go —
// neither of those exercises the full store-write + store-read +
// promotion pipeline in a single test. See ADR / design doc
// docs/plans/2026-04-25-mmap-blob-access-design.md.
func TestMakeBlobReaderPromotesToMmap(t *testing.T) {
	store := makeTestStore(t)

	payload := []byte("integration payload for mmap promotion via the store API")

	digest, err := writeBlob(store, payload)
	if err != nil {
		t.Fatalf("writeBlob: %v", err)
	}

	reader, err := store.MakeBlobReader(digest)
	if err != nil {
		t.Fatalf("MakeBlobReader(%s): %v", digest, err)
	}

	mb, err := mmap_blob.MakeMmapBlobFromBlobReader(reader)
	if err != nil {
		// Promotion failed — close the reader the caller still owns
		// (per the MakeMmapBlobFromBlobReader contract on failure).
		_ = reader.Close()
		t.Fatalf("MakeMmapBlobFromBlobReader: %v", err)
	}

	defer mb.Close() //defer:err-checked

	// On successful promotion, ownership of the underlying *os.File
	// transferred to the MmapBlob. Closing the reader must not
	// double-close that file.
	if err := reader.Close(); err != nil {
		t.Fatalf("reader.Close after promotion: %v", err)
	}

	got := mb.Bytes()
	if !bytes.Equal(got, payload) {
		t.Fatalf("Bytes() mismatch: got %q, want %q", got, payload)
	}
}
