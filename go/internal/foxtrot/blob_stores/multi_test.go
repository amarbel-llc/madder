//go:build test

package blob_stores

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/markl_io"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	_ "github.com/amarbel-llc/madder/go/internal/bravo/plugins/builtins"
)

// multiModeStub is a BlobStore double for Multi's mode-dispatch tests.
// It lets each test control HasBlob per-id, hand out a reader that
// yields a known byte pattern, and capture the writer it returned so
// the test can inspect bytes that were broadcast to it.
type multiModeStub struct {
	domain_interfaces.BlobStore

	hasIds       map[string]bool
	readerBytes  map[string][]byte
	lastWriter   *spyBlobWriter
	writerToHand *spyBlobWriter
}

func (s *multiModeStub) HasBlob(id domain_interfaces.MarklId) bool {
	if s.hasIds == nil {
		return false
	}
	return s.hasIds[id.String()]
}

func (s *multiModeStub) MakeBlobReader(
	id domain_interfaces.MarklId,
) (domain_interfaces.BlobReader, error) {
	data := s.readerBytes[id.String()]
	hash, _ := markl.FormatHashSha256.Get() //repool:owned
	return markl_io.MakeReadCloser(hash, bytes.NewReader(data)), nil
}

func (s *multiModeStub) MakeBlobWriter(
	_ domain_interfaces.FormatHash,
) (domain_interfaces.BlobWriter, error) {
	w := s.writerToHand
	if w == nil {
		w = &spyBlobWriter{}
	}
	s.lastWriter = w
	return w, nil
}

// makeMultiMirrorTestId returns a deterministic markl ID for tests
// where the actual hash content does not matter — only stability across
// HasBlob lookups.
func makeMultiMirrorTestId(t *testing.T, seed string) domain_interfaces.MarklId {
	t.Helper()
	raw := sha256.Sum256([]byte(seed))
	id, repool := markl.FormatHashSha256.GetBlobIdForHexString(
		hex.EncodeToString(raw[:]),
	)
	t.Cleanup(repool)
	return id
}

func TestMulti_Mirror_HasBlob_AnyChild(t *testing.T) {
	id := makeMultiMirrorTestId(t, "mirror-has-blob")
	idKey := id.String()

	type want struct {
		name     string
		aHas     bool
		bHas     bool
		expected bool
	}

	cases := []want{
		{name: "first-has", aHas: true, bHas: false, expected: true},
		{name: "second-has", aHas: false, bHas: true, expected: true},
		{name: "both-have", aHas: true, bHas: true, expected: true},
		{name: "neither", aHas: false, bHas: false, expected: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			storeA := &multiModeStub{hasIds: map[string]bool{idKey: c.aHas}}
			storeB := &multiModeStub{hasIds: map[string]bool{idKey: c.bHas}}

			m, err := NewMulti(&spyActiveContext{}).Mirror(
				BlobStoreInitialized{BlobStore: storeA},
				BlobStoreInitialized{BlobStore: storeB},
			).Build()
			if err != nil {
				t.Fatalf("Build: %v", err)
			}

			if got := m.HasBlob(id); got != c.expected {
				t.Fatalf("HasBlob: got %v, want %v", got, c.expected)
			}
		})
	}
}

func TestMulti_Mirror_MakeBlobReader_FirstHit(t *testing.T) {
	id := makeMultiMirrorTestId(t, "mirror-reader-first-hit")
	idKey := id.String()

	bytesFromA := []byte("from-store-A")
	bytesFromB := []byte("from-store-B")

	// Both stores claim to have the blob, but each yields distinct
	// bytes. Mirror's reader should come from the first child.
	storeA := &multiModeStub{
		hasIds:      map[string]bool{idKey: true},
		readerBytes: map[string][]byte{idKey: bytesFromA},
	}
	storeB := &multiModeStub{
		hasIds:      map[string]bool{idKey: true},
		readerBytes: map[string][]byte{idKey: bytesFromB},
	}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	reader, err := m.MakeBlobReader(id)
	if err != nil {
		t.Fatalf("MakeBlobReader: %v", err)
	}
	defer reader.Close() //defer:err-checked

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if !bytes.Equal(got, bytesFromA) {
		t.Fatalf(
			"reader bytes: got %q, want %q (first-child store)",
			got,
			bytesFromA,
		)
	}
}

func TestMulti_Mirror_MakeBlobWriter_WritesToAll(t *testing.T) {
	storeAWriter := &spyBlobWriter{}
	storeBWriter := &spyBlobWriter{}

	storeA := &multiModeStub{writerToHand: storeAWriter}
	storeB := &multiModeStub{writerToHand: storeBWriter}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	payload := []byte("mirror-write-payload")

	writer, err := m.MakeBlobWriter(markl.FormatHashSha256)
	if err != nil {
		t.Fatalf("MakeBlobWriter: %v", err)
	}

	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !bytes.Equal(storeAWriter.received, payload) {
		t.Errorf(
			"storeA bytes: got %q, want %q",
			storeAWriter.received,
			payload,
		)
	}

	if !bytes.Equal(storeBWriter.received, payload) {
		t.Errorf(
			"storeB bytes: got %q, want %q",
			storeBWriter.received,
			payload,
		)
	}
}
