//go:build test

package blob_stores

import (
	"bytes"
	"io"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/alfa/markl_io"
	_ "github.com/amarbel-llc/madder/go/internal/bravo/plugins/builtins"
	"github.com/amarbel-llc/piggy/go/markl/pkgs/markl"
)

// TestDiscardBlobStore_HashMatchesDirectDigester pins the discard store's
// behavioral contract: the markl-id its BlobWriter returns for a given
// byte stream MUST equal the markl-id a bare markl_io digester returns for
// the same bytes. cutting-garden's diff command relies on this — it
// compares discard-store-computed blob-ids against blob-ids in capture
// receipts, which were produced by real stores running the same digester
// chain (blob_io/writer.go:49 wires the digester upstream of compression
// and encryption, so both paths see raw input bytes).
func TestDiscardBlobStore_HashMatchesDirectDigester(t *testing.T) {
	cases := []struct {
		name       string
		hashFormat markl.FormatHash
		payload    []byte
	}{
		{
			name:       "sha256 small text",
			hashFormat: markl.FormatHashSha256,
			payload:    []byte("hello, cutting garden\n"),
		},
		{
			name:       "blake2b small text",
			hashFormat: markl.FormatHashBlake2b256,
			payload:    []byte("hello, cutting garden\n"),
		},
		{
			name:       "sha256 empty",
			hashFormat: markl.FormatHashSha256,
			payload:    []byte{},
		},
		{
			name:       "sha256 multi-write",
			hashFormat: markl.FormatHashSha256,
			payload:    bytes.Repeat([]byte("abc"), 4096),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeViaDiscardStore(t, tc.hashFormat, tc.payload)
			want := computeViaDirectDigester(t, tc.hashFormat, tc.payload)

			if got != want {
				t.Errorf(
					"discard store markl-id mismatch:\n  got:  %s\n  want: %s",
					got, want,
				)
			}
		})
	}
}

func computeViaDiscardStore(
	t *testing.T,
	hashFormat markl.FormatHash,
	payload []byte,
) string {
	t.Helper()

	store := NewDiscardBlobStore(hashFormat)

	writer, err := store.MakeBlobWriter(nil)
	if err != nil {
		t.Fatalf("MakeBlobWriter: %v", err)
	}

	if _, err = io.Copy(writer, bytes.NewReader(payload)); err != nil {
		t.Fatalf("io.Copy into writer: %v", err)
	}

	if err = writer.Close(); err != nil {
		t.Fatalf("writer.Close: %v", err)
	}

	return writer.GetMarklId().String()
}

func computeViaDirectDigester(
	t *testing.T,
	hashFormat markl.FormatHash,
	payload []byte,
) string {
	t.Helper()

	hash, repool := hashFormat.Get()
	defer repool()

	digester := markl_io.MakeWriter(hash, nil)

	if _, err := io.Copy(digester, bytes.NewReader(payload)); err != nil {
		t.Fatalf("io.Copy into digester: %v", err)
	}

	return digester.GetMarklId().String()
}

// TestDiscardBlobStore_HashFormatFallback verifies that calling
// MakeBlobWriter with a nil FormatHash falls back to the store's
// configured default — matching the localHashBucketed contract that
// capture relies on (capture.go:573 calls MakeBlobWriter(nil)).
func TestDiscardBlobStore_HashFormatFallback(t *testing.T) {
	store := NewDiscardBlobStore(markl.FormatHashBlake2b256)

	if got := store.GetDefaultHashType().GetMarklFormatId(); got != markl.FormatIdHashBlake2b256 {
		t.Fatalf("GetDefaultHashType: got %q, want %q",
			got, markl.FormatIdHashBlake2b256)
	}

	writer, err := store.MakeBlobWriter(nil)
	if err != nil {
		t.Fatalf("MakeBlobWriter(nil): %v", err)
	}

	if _, err = io.Copy(writer, bytes.NewReader([]byte("x"))); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}

	if err = writer.Close(); err != nil {
		t.Fatalf("writer.Close: %v", err)
	}

	id := writer.GetMarklId()
	if id.GetMarklFormat().GetMarklFormatId() != markl.FormatIdHashBlake2b256 {
		t.Errorf("markl-id format: got %q, want %q",
			id.GetMarklFormat().GetMarklFormatId(),
			markl.FormatIdHashBlake2b256)
	}
}
