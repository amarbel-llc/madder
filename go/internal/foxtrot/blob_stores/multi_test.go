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
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
)

// multiModeStub is a BlobStore double for Multi's mode-dispatch tests.
// It lets each test control HasBlob per-id, hand out a reader that
// yields a known byte pattern, and capture the writer it returned so
// the test can inspect bytes that were broadcast to it.
//
// The description/defaultHash/config/ioWrapper fields back the
// Get*-delegation assertions added in Task 6. Each test sets only the
// fields it inspects; unset fields fall through to the embedded
// (nil) BlobStore interface and would panic if exercised.
type multiModeStub struct {
	domain_interfaces.BlobStore

	hasIds       map[string]bool
	readerBytes  map[string][]byte
	lastWriter   *spyBlobWriter
	writerToHand *spyBlobWriter

	description string
	defaultHash domain_interfaces.FormatHash
	config      domain_interfaces.BlobStoreConfig
	ioWrapper   domain_interfaces.BlobIOWrapper
}

func (s *multiModeStub) GetBlobStoreDescription() string {
	return s.description
}

func (s *multiModeStub) GetDefaultHashType() domain_interfaces.FormatHash {
	return s.defaultHash
}

func (s *multiModeStub) GetBlobStoreConfig() domain_interfaces.BlobStoreConfig {
	return s.config
}

func (s *multiModeStub) GetBlobIOWrapper() domain_interfaces.BlobIOWrapper {
	return s.ioWrapper
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

// TestMulti_Mirror_GetBlobStoreDescription pins the wrapper synthesizing
// "multi/mirror(A,B,C)" from its children's descriptions. Each stub
// reports a single-letter description; the wrapper joins them with
// commas inside the multi/mirror(...) envelope.
func TestMulti_Mirror_GetBlobStoreDescription(t *testing.T) {
	storeA := &multiModeStub{description: "A"}
	storeB := &multiModeStub{description: "B"}
	storeC := &multiModeStub{description: "C"}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
		BlobStoreInitialized{BlobStore: storeC},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	got := m.GetBlobStoreDescription()
	want := "multi/mirror(A,B,C)"
	if got != want {
		t.Fatalf("GetBlobStoreDescription: got %q, want %q", got, want)
	}
}

// TestMulti_Mirror_GetDefaultHashType_FirstChild pins delegation to the
// first child for the default hash type — Mirror is heterogeneous-safe
// in principle, but the wrapper's own default tracks the first child.
// FormatHash interface values aren't `==`-comparable (their concrete
// type contains a *sync.Pool); compare by GetMarklFormatId() instead.
func TestMulti_Mirror_GetDefaultHashType_FirstChild(t *testing.T) {
	storeA := &multiModeStub{defaultHash: markl.FormatHashSha256}
	storeB := &multiModeStub{} // would panic if consulted

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	got := m.GetDefaultHashType()
	if got == nil {
		t.Fatalf("GetDefaultHashType: got nil, want first child's hash")
	}
	if got.GetMarklFormatId() != markl.FormatHashSha256.GetMarklFormatId() {
		t.Fatalf(
			"GetDefaultHashType: got %q, want %q (first child)",
			got.GetMarklFormatId(),
			markl.FormatHashSha256.GetMarklFormatId(),
		)
	}
}

// stubBlobStoreConfig is a minimal BlobStoreConfig used only as a
// distinguishable sentinel value for TestMulti_Mirror_GetBlobStoreConfig_FirstChild.
type stubBlobStoreConfig struct {
	storeType string
}

func (c stubBlobStoreConfig) GetBlobStoreType() string { return c.storeType }

// TestMulti_Mirror_GetBlobStoreConfig_FirstChild pins delegation of
// GetBlobStoreConfig to the first child.
func TestMulti_Mirror_GetBlobStoreConfig_FirstChild(t *testing.T) {
	wantConfig := stubBlobStoreConfig{storeType: "first-child"}
	storeA := &multiModeStub{config: wantConfig}
	storeB := &multiModeStub{
		config: stubBlobStoreConfig{storeType: "second-child"},
	}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	got := m.GetBlobStoreConfig()
	if got != wantConfig {
		t.Fatalf(
			"GetBlobStoreConfig: got %#v, want %#v (first child)",
			got,
			wantConfig,
		)
	}
}

// stubBlobIOWrapper is a minimal BlobIOWrapper used only as a
// distinguishable sentinel value for TestMulti_Mirror_GetBlobIOWrapper_FirstChild.
type stubBlobIOWrapper struct {
	tag string
}

func (w stubBlobIOWrapper) GetBlobEncryption() domain_interfaces.MarklId {
	return nil
}

func (w stubBlobIOWrapper) GetBlobCompression() interfaces.IOWrapper {
	return nil
}

// TestMulti_Mirror_GetBlobIOWrapper_FirstChild pins delegation of
// GetBlobIOWrapper to the first child.
func TestMulti_Mirror_GetBlobIOWrapper_FirstChild(t *testing.T) {
	wantWrapper := stubBlobIOWrapper{tag: "first-child"}
	storeA := &multiModeStub{ioWrapper: wantWrapper}
	storeB := &multiModeStub{
		ioWrapper: stubBlobIOWrapper{tag: "second-child"},
	}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	got := m.GetBlobIOWrapper()
	if got != wantWrapper {
		t.Fatalf(
			"GetBlobIOWrapper: got %#v, want %#v (first child)",
			got,
			wantWrapper,
		)
	}
}
