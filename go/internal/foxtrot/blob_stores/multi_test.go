//go:build test

package blob_stores

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/markl_io"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	_ "github.com/amarbel-llc/madder/go/internal/bravo/plugins/builtins"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_io"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
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

	// makeWriterCount tracks how many times MakeBlobWriter has been
	// invoked on this stub. Used by the WriteThrough writer tests to
	// pin that read sources never have their writer constructed.
	makeWriterCount int

	// allBlobsSeq, when non-nil, is the SeqError the stub yields from
	// AllBlobs(). Tests construct it via makeMarklIdSeq or inline.
	allBlobsSeq interfaces.SeqError[domain_interfaces.MarklId]

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
	s.makeWriterCount++
	w := s.writerToHand
	if w == nil {
		w = &spyBlobWriter{}
	}
	s.lastWriter = w
	return w, nil
}

func (s *multiModeStub) AllBlobs() interfaces.SeqError[domain_interfaces.MarklId] {
	if s.allBlobsSeq != nil {
		return s.allBlobsSeq
	}
	return func(yield func(domain_interfaces.MarklId, error) bool) {}
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

// makeMultiAllBlobsTestId returns a deterministic SHA-256 markl id whose
// raw byte content is the 32-byte repetition of seedByte. This makes the
// id's lex byte-order trivially predictable across tests: id(0xAA) sorts
// before id(0xBB).
func makeMultiAllBlobsTestId(
	t *testing.T,
	seedByte byte,
) domain_interfaces.MarklId {
	t.Helper()
	return makeMultiAllBlobsTestIdForFormat(t, markl.FormatHashSha256, seedByte)
}

// makeMultiAllBlobsTestIdForFormat is the format-parameterized variant of
// makeMultiAllBlobsTestId. Both registered hash formats (sha256 and
// blake2b256) emit 32-byte digests, so the same seed-byte repetition
// trick works across formats.
func makeMultiAllBlobsTestIdForFormat(
	t *testing.T,
	formatHash markl.FormatHash,
	seedByte byte,
) domain_interfaces.MarklId {
	t.Helper()
	hexString := strings.Repeat(
		hex.EncodeToString([]byte{seedByte}),
		formatHash.GetSize(),
	)
	id, repool := formatHash.GetBlobIdForHexString(hexString)
	t.Cleanup(repool)
	return id
}

// makeMarklIdSeq adapts a slice of ids into a SeqError that yields each
// id with a nil error.
func makeMarklIdSeq(
	ids ...domain_interfaces.MarklId,
) interfaces.SeqError[domain_interfaces.MarklId] {
	return func(yield func(domain_interfaces.MarklId, error) bool) {
		for _, id := range ids {
			if !yield(id, nil) {
				return
			}
		}
	}
}

// drainAllBlobs collects every (id, err) the seq yields. Errors are
// returned as a separate slice in yield order; callers assert on both.
func drainAllBlobs(
	seq interfaces.SeqError[domain_interfaces.MarklId],
) (ids []string, errs []error) {
	for id, err := range seq {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		ids = append(ids, id.String())
	}
	return ids, errs
}

// TestMulti_AllBlobs_SameHashDedupes pins the N-way merge: storeA
// yields [d1, d2, d3] and storeB yields [d2, d3, d4]. Same-hash heads
// (d2, d3 present in both) collapse to a single yield, producing the
// sorted union [d1, d2, d3, d4].
func TestMulti_AllBlobs_SameHashDedupes(t *testing.T) {
	d1 := makeMultiAllBlobsTestId(t, 0x11)
	d2 := makeMultiAllBlobsTestId(t, 0x22)
	d3 := makeMultiAllBlobsTestId(t, 0x33)
	d4 := makeMultiAllBlobsTestId(t, 0x44)

	storeA := &multiModeStub{
		allBlobsSeq: makeMarklIdSeq(d1, d2, d3),
	}
	storeB := &multiModeStub{
		allBlobsSeq: makeMarklIdSeq(d2, d3, d4),
	}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	gotIds, gotErrs := drainAllBlobs(m.AllBlobs())
	if len(gotErrs) != 0 {
		t.Fatalf("AllBlobs: unexpected errors: %v", gotErrs)
	}

	wantIds := []string{d1.String(), d2.String(), d3.String(), d4.String()}
	if !reflect.DeepEqual(gotIds, wantIds) {
		t.Fatalf("AllBlobs: got %v, want %v", gotIds, wantIds)
	}
}

// TestMulti_AllBlobs_BothStoresEmpty pins that two empty sources yield
// nothing through the merge.
func TestMulti_AllBlobs_BothStoresEmpty(t *testing.T) {
	storeA := &multiModeStub{allBlobsSeq: makeMarklIdSeq()}
	storeB := &multiModeStub{allBlobsSeq: makeMarklIdSeq()}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	gotIds, gotErrs := drainAllBlobs(m.AllBlobs())
	if len(gotErrs) != 0 {
		t.Fatalf("AllBlobs: unexpected errors: %v", gotErrs)
	}
	if len(gotIds) != 0 {
		t.Fatalf("AllBlobs: got %v, want []", gotIds)
	}
}

// TestMulti_AllBlobs_OneStoreEmpty pins that a non-empty source merged
// with an empty one yields the non-empty source's full sequence.
func TestMulti_AllBlobs_OneStoreEmpty(t *testing.T) {
	d1 := makeMultiAllBlobsTestId(t, 0x11)
	d2 := makeMultiAllBlobsTestId(t, 0x22)

	storeA := &multiModeStub{
		allBlobsSeq: makeMarklIdSeq(d1, d2),
	}
	storeB := &multiModeStub{
		allBlobsSeq: makeMarklIdSeq(),
	}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	gotIds, gotErrs := drainAllBlobs(m.AllBlobs())
	if len(gotErrs) != 0 {
		t.Fatalf("AllBlobs: unexpected errors: %v", gotErrs)
	}

	wantIds := []string{d1.String(), d2.String()}
	if !reflect.DeepEqual(gotIds, wantIds) {
		t.Fatalf("AllBlobs: got %v, want %v", gotIds, wantIds)
	}
}

// TestMulti_AllBlobs_PropagatesErrors pins error propagation: a source
// that yields an error mid-sequence surfaces that error through the
// merged seq, and subsequent good entries still come through.
func TestMulti_AllBlobs_PropagatesErrors(t *testing.T) {
	d1 := makeMultiAllBlobsTestId(t, 0x11)
	d3 := makeMultiAllBlobsTestId(t, 0x33)

	sentinel := errors.New("mid-sequence boom")

	storeA := &multiModeStub{
		allBlobsSeq: func(
			yield func(domain_interfaces.MarklId, error) bool,
		) {
			if !yield(d1, nil) {
				return
			}
			if !yield(nil, sentinel) {
				return
			}
			if !yield(d3, nil) {
				return
			}
		},
	}
	storeB := &multiModeStub{allBlobsSeq: makeMarklIdSeq()}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	gotIds, gotErrs := drainAllBlobs(m.AllBlobs())
	if len(gotErrs) != 1 || gotErrs[0] != sentinel {
		t.Fatalf("AllBlobs errors: got %v, want [%v]", gotErrs, sentinel)
	}

	wantIds := []string{d1.String(), d3.String()}
	if !reflect.DeepEqual(gotIds, wantIds) {
		t.Fatalf("AllBlobs ids: got %v, want %v", gotIds, wantIds)
	}
}

// TestMulti_AllBlobs_TwoSourcesErrorAtDifferentSteps tightens the
// multi-source error-interleaving contract (Task 7 carry-forward).
// storeA errors after d1; storeB errors after d2. The merge must
// surface both errors and continue past each, yielding the remaining
// good ids from each source.
func TestMulti_AllBlobs_TwoSourcesErrorAtDifferentSteps(t *testing.T) {
	d1 := makeMultiAllBlobsTestId(t, 0x11)
	d2 := makeMultiAllBlobsTestId(t, 0x22)
	d3 := makeMultiAllBlobsTestId(t, 0x33)
	d4 := makeMultiAllBlobsTestId(t, 0x44)

	errA := errors.New("storeA mid-sequence boom")
	errB := errors.New("storeB mid-sequence boom")

	storeA := &multiModeStub{
		allBlobsSeq: func(
			yield func(domain_interfaces.MarklId, error) bool,
		) {
			if !yield(d1, nil) {
				return
			}
			if !yield(nil, errA) {
				return
			}
			if !yield(d3, nil) {
				return
			}
		},
	}
	storeB := &multiModeStub{
		allBlobsSeq: func(
			yield func(domain_interfaces.MarklId, error) bool,
		) {
			if !yield(d2, nil) {
				return
			}
			if !yield(nil, errB) {
				return
			}
			if !yield(d4, nil) {
				return
			}
		},
	}

	m, err := NewMulti(&spyActiveContext{}).
		Mirror(
			BlobStoreInitialized{BlobStore: storeA},
			BlobStoreInitialized{BlobStore: storeB},
		).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	gotIds, gotErrs := drainAllBlobs(m.AllBlobs())

	if len(gotErrs) != 2 {
		t.Fatalf("AllBlobs errors: got %d, want 2: %v", len(gotErrs), gotErrs)
	}
	errSet := map[error]bool{gotErrs[0]: true, gotErrs[1]: true}
	if !errSet[errA] || !errSet[errB] {
		t.Fatalf("AllBlobs errors: got %v, want both %v and %v", gotErrs, errA, errB)
	}

	wantIds := []string{d1.String(), d2.String(), d3.String(), d4.String()}
	gotIdsSorted := append([]string(nil), gotIds...)
	sort.Strings(gotIdsSorted)
	wantIdsSorted := append([]string(nil), wantIds...)
	sort.Strings(wantIdsSorted)
	if !reflect.DeepEqual(gotIdsSorted, wantIdsSorted) {
		t.Fatalf("AllBlobs ids (sorted): got %v, want %v", gotIdsSorted, wantIdsSorted)
	}
}

// TestMulti_AllBlobs_BothSourcesErrorImmediately pins behavior when
// every source's first head is an error. Both errors must surface
// and the merge terminates with no ids.
func TestMulti_AllBlobs_BothSourcesErrorImmediately(t *testing.T) {
	errA := errors.New("storeA initial boom")
	errB := errors.New("storeB initial boom")

	storeA := &multiModeStub{
		allBlobsSeq: func(yield func(domain_interfaces.MarklId, error) bool) {
			yield(nil, errA)
		},
	}
	storeB := &multiModeStub{
		allBlobsSeq: func(yield func(domain_interfaces.MarklId, error) bool) {
			yield(nil, errB)
		},
	}

	m, err := NewMulti(&spyActiveContext{}).
		Mirror(
			BlobStoreInitialized{BlobStore: storeA},
			BlobStoreInitialized{BlobStore: storeB},
		).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	gotIds, gotErrs := drainAllBlobs(m.AllBlobs())
	if len(gotIds) != 0 {
		t.Fatalf("AllBlobs ids: got %v, want []", gotIds)
	}
	if len(gotErrs) != 2 {
		t.Fatalf("AllBlobs errors: got %d, want 2: %v", len(gotErrs), gotErrs)
	}
	errSet := map[error]bool{gotErrs[0]: true, gotErrs[1]: true}
	if !errSet[errA] || !errSet[errB] {
		t.Fatalf("AllBlobs errors: got %v, want both %v and %v", gotErrs, errA, errB)
	}
}

// TestMulti_AllBlobs_CrossHashPassesThrough pins that ids under
// different hash formats never compare equal and therefore pass through
// the merge as separate entries. storeA yields blake2b256 ids and
// storeB yields sha256 ids; the merged seq must contain the union of
// both sets with no dedupe across formats. Order depends on
// (format-id, raw bytes) lex compare — testing the set is more readable
// and matches the spec's "by design" framing.
func TestMulti_AllBlobs_CrossHashPassesThrough(t *testing.T) {
	blakeA := makeMultiAllBlobsTestIdForFormat(t, markl.FormatHashBlake2b256, 0x11)
	blakeB := makeMultiAllBlobsTestIdForFormat(t, markl.FormatHashBlake2b256, 0x22)
	sha256A := makeMultiAllBlobsTestIdForFormat(t, markl.FormatHashSha256, 0x11)
	sha256B := makeMultiAllBlobsTestIdForFormat(t, markl.FormatHashSha256, 0x22)

	storeA := &multiModeStub{
		allBlobsSeq: makeMarklIdSeq(blakeA, blakeB),
	}
	storeB := &multiModeStub{
		allBlobsSeq: makeMarklIdSeq(sha256A, sha256B),
	}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	gotIds, gotErrs := drainAllBlobs(m.AllBlobs())
	if len(gotErrs) != 0 {
		t.Fatalf("AllBlobs: unexpected errors: %v", gotErrs)
	}

	wantSet := []string{
		blakeA.String(),
		blakeB.String(),
		sha256A.String(),
		sha256B.String(),
	}
	gotSet := append([]string(nil), gotIds...)
	sort.Strings(gotSet)
	sort.Strings(wantSet)

	if !reflect.DeepEqual(gotSet, wantSet) {
		t.Fatalf(
			"AllBlobs cross-hash set: got %v, want %v (no dedupe across formats)",
			gotSet,
			wantSet,
		)
	}
}

// TestMulti_AllBlobs_SkipsNilIdHead pins that a child yielding
// (nil id, nil err) — a misbehaving producer — is skipped instead of
// being passed to compareMarklIds, where it would panic. The merge
// continues with the remaining good ids.
func TestMulti_AllBlobs_SkipsNilIdHead(t *testing.T) {
	d1 := makeMultiAllBlobsTestId(t, 0x11)
	d2 := makeMultiAllBlobsTestId(t, 0x22)

	storeA := &multiModeStub{
		allBlobsSeq: func(
			yield func(domain_interfaces.MarklId, error) bool,
		) {
			if !yield(d1, nil) {
				return
			}
			// Misbehaving emit: nil id, nil err. The Multi merge must
			// treat this as a skipped head, not feed it to
			// compareMarklIds.
			if !yield(nil, nil) {
				return
			}
			if !yield(d2, nil) {
				return
			}
		},
	}
	storeB := &multiModeStub{allBlobsSeq: makeMarklIdSeq()}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	gotIds, gotErrs := drainAllBlobs(m.AllBlobs())
	if len(gotErrs) != 0 {
		t.Fatalf("AllBlobs: unexpected errors: %v", gotErrs)
	}

	wantIds := []string{d1.String(), d2.String()}
	if !reflect.DeepEqual(gotIds, wantIds) {
		t.Fatalf("AllBlobs: got %v, want %v", gotIds, wantIds)
	}
}

// TestMulti_WriteThrough_HasBlob_WriteOrAnyRead pins write-through
// HasBlob: it returns true if any of the write store or read sources
// claims the blob, and false only when none do.
func TestMulti_WriteThrough_HasBlob_WriteOrAnyRead(t *testing.T) {
	id := makeMultiMirrorTestId(t, "wt-has-blob")
	idKey := id.String()

	type want struct {
		name     string
		writeHas bool
		readAHas bool
		readBHas bool
		expected bool
	}

	cases := []want{
		{name: "only-write", writeHas: true, readAHas: false, readBHas: false, expected: true},
		{name: "only-one-read", writeHas: false, readAHas: false, readBHas: true, expected: true},
		{name: "both", writeHas: true, readAHas: true, readBHas: false, expected: true},
		{name: "none", writeHas: false, readAHas: false, readBHas: false, expected: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			writeStore := &multiModeStub{hasIds: map[string]bool{idKey: c.writeHas}}
			readA := &multiModeStub{hasIds: map[string]bool{idKey: c.readAHas}}
			readB := &multiModeStub{hasIds: map[string]bool{idKey: c.readBHas}}

			m, err := NewMulti(&spyActiveContext{}).
				WriteTo(BlobStoreInitialized{BlobStore: writeStore}).
				Read(
					BlobStoreInitialized{BlobStore: readA},
					BlobStoreInitialized{BlobStore: readB},
				).
				ReadFill(false).
				Build()
			if err != nil {
				t.Fatalf("Build: %v", err)
			}

			if got := m.HasBlob(id); got != c.expected {
				t.Fatalf("HasBlob: got %v, want %v", got, c.expected)
			}
		})
	}
}

// TestMulti_WriteThrough_MakeBlobReader_WriteStoreFirst pins read
// precedence: when both the write store and a read source claim to
// have the blob, the reader comes from the write store.
func TestMulti_WriteThrough_MakeBlobReader_WriteStoreFirst(t *testing.T) {
	id := makeMultiMirrorTestId(t, "wt-reader-write-first")
	idKey := id.String()

	bytesFromWrite := []byte("from-write-store")
	bytesFromRead := []byte("from-read-source")

	writeStore := &multiModeStub{
		hasIds:      map[string]bool{idKey: true},
		readerBytes: map[string][]byte{idKey: bytesFromWrite},
	}
	readSrc := &multiModeStub{
		hasIds:      map[string]bool{idKey: true},
		readerBytes: map[string][]byte{idKey: bytesFromRead},
	}

	m, err := NewMulti(&spyActiveContext{}).
		WriteTo(BlobStoreInitialized{BlobStore: writeStore}).
		Read(BlobStoreInitialized{BlobStore: readSrc}).
		ReadFill(false).
		Build()
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

	if !bytes.Equal(got, bytesFromWrite) {
		t.Fatalf(
			"reader bytes: got %q, want %q (write store first)",
			got,
			bytesFromWrite,
		)
	}
}

// TestMulti_WriteThrough_MakeBlobReader_FallsBackToReadSource_NoFill
// pins that with ReadFill(false), a write-store miss falls back to
// the first read source that has the blob and returns its reader
// directly — no copy-back through the write store.
func TestMulti_WriteThrough_MakeBlobReader_FallsBackToReadSource_NoFill(t *testing.T) {
	id := makeMultiMirrorTestId(t, "wt-reader-fallback-no-fill")
	idKey := id.String()

	bytesFromRead := []byte("from-read-source")

	writeStore := &multiModeStub{
		hasIds:      map[string]bool{idKey: false},
		readerBytes: map[string][]byte{},
	}
	readSrc := &multiModeStub{
		hasIds:      map[string]bool{idKey: true},
		readerBytes: map[string][]byte{idKey: bytesFromRead},
	}

	m, err := NewMulti(&spyActiveContext{}).
		WriteTo(BlobStoreInitialized{BlobStore: writeStore}).
		Read(BlobStoreInitialized{BlobStore: readSrc}).
		ReadFill(false).
		Build()
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

	if !bytes.Equal(got, bytesFromRead) {
		t.Fatalf(
			"reader bytes: got %q, want %q (read source)",
			got,
			bytesFromRead,
		)
	}

	// With ReadFill=false, the write store must NOT have had a
	// writer constructed during the read.
	if writeStore.makeWriterCount != 0 {
		t.Fatalf(
			"write store MakeBlobWriter count: got %d, want 0 (no fill)",
			writeStore.makeWriterCount,
		)
	}
	if writeStore.lastWriter != nil {
		t.Fatalf("write store lastWriter: got non-nil, want nil (no fill)")
	}
}

// TestMulti_WriteThrough_MakeBlobReader_AllMiss pins that when neither
// the write store nor any read source has the blob, MakeBlobReader
// returns ErrBlobMissing.
func TestMulti_WriteThrough_MakeBlobReader_AllMiss(t *testing.T) {
	id := makeMultiMirrorTestId(t, "wt-reader-all-miss")

	writeStore := &multiModeStub{hasIds: map[string]bool{}}
	readA := &multiModeStub{hasIds: map[string]bool{}}
	readB := &multiModeStub{hasIds: map[string]bool{}}

	m, err := NewMulti(&spyActiveContext{}).
		WriteTo(BlobStoreInitialized{BlobStore: writeStore}).
		Read(
			BlobStoreInitialized{BlobStore: readA},
			BlobStoreInitialized{BlobStore: readB},
		).
		ReadFill(false).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	reader, gotErr := m.MakeBlobReader(id)
	if reader != nil {
		t.Fatalf("MakeBlobReader: got non-nil reader, want nil on all-miss")
	}
	if !errors.Is(gotErr, blob_io.ErrBlobMissing{}) {
		t.Fatalf("MakeBlobReader error: got %v, want ErrBlobMissing", gotErr)
	}
}

// TestMulti_WriteThrough_MakeBlobWriter_WriteStoreOnly pins that
// writes from MakeBlobWriter go to the write store only — read
// sources never have their writer constructed.
func TestMulti_WriteThrough_MakeBlobWriter_WriteStoreOnly(t *testing.T) {
	writeStoreWriter := &spyBlobWriter{}
	writeStore := &multiModeStub{writerToHand: writeStoreWriter}
	readA := &multiModeStub{}
	readB := &multiModeStub{}

	m, err := NewMulti(&spyActiveContext{}).
		WriteTo(BlobStoreInitialized{BlobStore: writeStore}).
		Read(
			BlobStoreInitialized{BlobStore: readA},
			BlobStoreInitialized{BlobStore: readB},
		).
		ReadFill(false).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	payload := []byte("wt-write-payload")

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

	if !bytes.Equal(writeStoreWriter.received, payload) {
		t.Errorf(
			"write store bytes: got %q, want %q",
			writeStoreWriter.received,
			payload,
		)
	}

	if writeStore.makeWriterCount != 1 {
		t.Errorf(
			"write store MakeBlobWriter count: got %d, want 1",
			writeStore.makeWriterCount,
		)
	}
	if readA.makeWriterCount != 0 {
		t.Errorf(
			"readA MakeBlobWriter count: got %d, want 0",
			readA.makeWriterCount,
		)
	}
	if readB.makeWriterCount != 0 {
		t.Errorf(
			"readB MakeBlobWriter count: got %d, want 0",
			readB.makeWriterCount,
		)
	}
}

// TestMulti_WriteThrough_Description_NamesWriteAndRead pins the
// write-through GetBlobStoreDescription format:
// "multi/write-through(W=<desc>, R=<desc>, R=<desc>)".
func TestMulti_WriteThrough_Description_NamesWriteAndRead(t *testing.T) {
	writeStore := &multiModeStub{description: "local"}
	readA := &multiModeStub{description: "remoteA"}
	readB := &multiModeStub{description: "remoteB"}

	m, err := NewMulti(&spyActiveContext{}).
		WriteTo(BlobStoreInitialized{BlobStore: writeStore}).
		Read(
			BlobStoreInitialized{BlobStore: readA},
			BlobStoreInitialized{BlobStore: readB},
		).
		ReadFill(false).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	got := m.GetBlobStoreDescription()
	want := "multi/write-through(W=local, R=remoteA, R=remoteB)"
	if got != want {
		t.Fatalf("GetBlobStoreDescription: got %q, want %q", got, want)
	}
}

// TestMulti_WriteThrough_DefaultHashType_FromWriteStore pins that the
// wrapper's default hash type delegates to the write store, not to
// any read source.
func TestMulti_WriteThrough_DefaultHashType_FromWriteStore(t *testing.T) {
	writeStore := &multiModeStub{defaultHash: markl.FormatHashSha256}
	readSrc := &multiModeStub{} // would panic if consulted

	m, err := NewMulti(&spyActiveContext{}).
		WriteTo(BlobStoreInitialized{BlobStore: writeStore}).
		Read(BlobStoreInitialized{BlobStore: readSrc}).
		ReadFill(false).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	got := m.GetDefaultHashType()
	if got == nil {
		t.Fatalf("GetDefaultHashType: got nil, want write store's hash")
	}
	if got.GetMarklFormatId() != markl.FormatHashSha256.GetMarklFormatId() {
		t.Fatalf(
			"GetDefaultHashType: got %q, want %q (write store)",
			got.GetMarklFormatId(),
			markl.FormatHashSha256.GetMarklFormatId(),
		)
	}
}
