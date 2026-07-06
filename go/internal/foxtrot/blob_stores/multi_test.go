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
	_ "github.com/amarbel-llc/madder/go/internal/bravo/plugins/builtins"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_io"
	"github.com/amarbel-llc/piggy/go/pkgs/markl"
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

	// makeWriterErr, when non-nil, is returned in place of a writer
	// from MakeBlobWriter. Used to exercise the mirror writer
	// fan-out error path and the write-through ReadFill failure path.
	makeWriterErr error

	// makeReaderErr, when non-nil, is returned in place of a reader
	// from MakeBlobReader. Used to exercise the write-through read
	// source MakeBlobReader-fails branch.
	makeReaderErr error

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
	if s.makeReaderErr != nil {
		return nil, s.makeReaderErr
	}
	data := s.readerBytes[id.String()]
	hash, _ := markl.FormatHashSha256.Get() //repool:owned
	return markl_io.MakeReadCloser(hash, bytes.NewReader(data)), nil
}

func (s *multiModeStub) MakeBlobWriter(
	_ domain_interfaces.FormatHash,
) (domain_interfaces.BlobWriter, error) {
	s.makeWriterCount++
	if s.makeWriterErr != nil {
		return nil, s.makeWriterErr
	}
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

// TestMulti_Mirror_MakeBlobReader_AllMiss pins that with no child
// reporting HasBlob, Mirror's MakeBlobReader returns ErrBlobMissing
// (covering the clonedId/ErrBlobMissing return after the loop).
func TestMulti_Mirror_MakeBlobReader_AllMiss(t *testing.T) {
	id := makeMultiMirrorTestId(t, "mirror-reader-all-miss")

	storeA := &multiModeStub{hasIds: map[string]bool{}}
	storeB := &multiModeStub{hasIds: map[string]bool{}}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
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

// TestMulti_Mirror_MakeBlobWriter_ChildError pins that an error from a
// child's MakeBlobWriter aborts the fan-out and surfaces the error.
func TestMulti_Mirror_MakeBlobWriter_ChildError(t *testing.T) {
	boom := errors.New("child writer boom")
	storeA := &multiModeStub{}
	storeB := &multiModeStub{makeWriterErr: boom}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	writer, gotErr := m.MakeBlobWriter(markl.FormatHashSha256)
	if writer != nil {
		t.Fatalf("MakeBlobWriter: got non-nil writer, want nil on child error")
	}
	if gotErr == nil {
		t.Fatalf("MakeBlobWriter: got nil error, want non-nil")
	}
}

// TestMulti_WriteThrough_MakeBlobReader_ReadSourceError pins that an
// error from the read source's MakeBlobReader propagates up rather
// than being silently swallowed.
func TestMulti_WriteThrough_MakeBlobReader_ReadSourceError(t *testing.T) {
	id := makeMultiMirrorTestId(t, "wt-reader-readsource-err")
	idKey := id.String()

	boom := errors.New("read source boom")
	writeStore := &multiModeStub{hasIds: map[string]bool{idKey: false}}
	readSrc := &multiModeStub{
		hasIds:        map[string]bool{idKey: true},
		makeReaderErr: boom,
	}

	m, err := NewMulti(&spyActiveContext{}).
		WriteTo(BlobStoreInitialized{BlobStore: writeStore}).
		Read(BlobStoreInitialized{BlobStore: readSrc}).
		ReadFill(false).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	reader, gotErr := m.MakeBlobReader(id)
	if reader != nil {
		t.Fatalf("MakeBlobReader: got non-nil reader, want nil on read error")
	}
	if !errors.Is(gotErr, boom) {
		t.Fatalf("MakeBlobReader error: got %v, want %v", gotErr, boom)
	}
}

// TestMulti_WriteThrough_MakeBlobReader_ReadFill_WriterError pins that
// when ReadFill is on but the write store's MakeBlobWriter fails, the
// caller still receives the source reader (the tee is skipped, but
// the read itself is not disrupted).
func TestMulti_WriteThrough_MakeBlobReader_ReadFill_WriterError(t *testing.T) {
	id := makeMultiMirrorTestId(t, "wt-reader-readfill-writer-err")
	idKey := id.String()

	payload := []byte("read-source-bytes")
	writeStore := &multiModeStub{
		hasIds:        map[string]bool{idKey: false},
		defaultHash:   markl.FormatHashSha256,
		makeWriterErr: errors.New("write store writer boom"),
	}
	readSrc := &multiModeStub{
		hasIds:      map[string]bool{idKey: true},
		readerBytes: map[string][]byte{idKey: payload},
	}

	m, err := NewMulti(&spyActiveContext{}).
		WriteTo(BlobStoreInitialized{BlobStore: writeStore}).
		Read(BlobStoreInitialized{BlobStore: readSrc}).
		ReadFill(true).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	reader, gotErr := m.MakeBlobReader(id)
	if gotErr != nil {
		t.Fatalf("MakeBlobReader: %v (writer failure should not disrupt read)", gotErr)
	}
	defer reader.Close() //defer:err-checked

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("reader bytes: got %q, want %q", got, payload)
	}
	if writeStore.makeWriterCount != 1 {
		t.Fatalf(
			"write store MakeBlobWriter count: got %d, want 1 (attempted but failed)",
			writeStore.makeWriterCount,
		)
	}
}

// TestMulti_WriteThrough_GetBlobStoreConfig_FromWriteStore pins
// delegation of GetBlobStoreConfig to the write store in WriteThrough.
func TestMulti_WriteThrough_GetBlobStoreConfig_FromWriteStore(t *testing.T) {
	wantConfig := stubBlobStoreConfig{storeType: "write-store"}
	writeStore := &multiModeStub{config: wantConfig}
	readSrc := &multiModeStub{
		config: stubBlobStoreConfig{storeType: "read-source"},
	}

	m, err := NewMulti(&spyActiveContext{}).
		WriteTo(BlobStoreInitialized{BlobStore: writeStore}).
		Read(BlobStoreInitialized{BlobStore: readSrc}).
		ReadFill(false).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	got := m.GetBlobStoreConfig()
	if got != wantConfig {
		t.Fatalf("GetBlobStoreConfig: got %#v, want %#v", got, wantConfig)
	}
}

// TestMulti_WriteThrough_GetBlobIOWrapper_FromWriteStore pins
// delegation of GetBlobIOWrapper to the write store in WriteThrough.
func TestMulti_WriteThrough_GetBlobIOWrapper_FromWriteStore(t *testing.T) {
	wantWrapper := stubBlobIOWrapper{tag: "write-store"}
	writeStore := &multiModeStub{ioWrapper: wantWrapper}
	readSrc := &multiModeStub{ioWrapper: stubBlobIOWrapper{tag: "read-source"}}

	m, err := NewMulti(&spyActiveContext{}).
		WriteTo(BlobStoreInitialized{BlobStore: writeStore}).
		Read(BlobStoreInitialized{BlobStore: readSrc}).
		ReadFill(false).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	got := m.GetBlobIOWrapper()
	if got != wantWrapper {
		t.Fatalf("GetBlobIOWrapper: got %#v, want %#v", got, wantWrapper)
	}
}

// TestMulti_WriteThrough_AllBlobs_MergesWriteAndReadSources pins that
// AllBlobs in WriteThrough mode merges the write store's sequence
// with each read source's sequence. Exercises the allBlobSources
// modeWriteThrough branch.
func TestMulti_WriteThrough_AllBlobs_MergesWriteAndReadSources(t *testing.T) {
	d1 := makeMultiAllBlobsTestId(t, 0x11)
	d2 := makeMultiAllBlobsTestId(t, 0x22)
	d3 := makeMultiAllBlobsTestId(t, 0x33)

	writeStore := &multiModeStub{allBlobsSeq: makeMarklIdSeq(d1)}
	readSrc := &multiModeStub{allBlobsSeq: makeMarklIdSeq(d2, d3)}

	m, err := NewMulti(&spyActiveContext{}).
		WriteTo(BlobStoreInitialized{BlobStore: writeStore}).
		Read(BlobStoreInitialized{BlobStore: readSrc}).
		ReadFill(false).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	gotIds, gotErrs := drainAllBlobs(m.AllBlobs())
	if len(gotErrs) != 0 {
		t.Fatalf("AllBlobs: unexpected errors: %v", gotErrs)
	}

	wantIds := []string{d1.String(), d2.String(), d3.String()}
	if !reflect.DeepEqual(gotIds, wantIds) {
		t.Fatalf("AllBlobs: got %v, want %v", gotIds, wantIds)
	}
}

// TestMulti_AllBlobs_CallerEarlyCancel_StopsAfterFirstYield pins that
// when the caller breaks out of the range loop on the very first
// yield, the merge returns immediately rather than continuing. This
// covers the `!yield(minId, nil) { return }` early-out in the merge
// main loop.
func TestMulti_AllBlobs_CallerEarlyCancel_StopsAfterFirstYield(t *testing.T) {
	d1 := makeMultiAllBlobsTestId(t, 0x11)
	d2 := makeMultiAllBlobsTestId(t, 0x22)

	storeA := &multiModeStub{allBlobsSeq: makeMarklIdSeq(d1, d2)}
	storeB := &multiModeStub{allBlobsSeq: makeMarklIdSeq()}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	var seen []string
	for id, err := range m.AllBlobs() {
		if err != nil {
			t.Fatalf("AllBlobs: unexpected error: %v", err)
		}
		seen = append(seen, id.String())
		break // cancel after first yield
	}

	if len(seen) != 1 || seen[0] != d1.String() {
		t.Fatalf("AllBlobs early-cancel: got %v, want [%s]", seen, d1.String())
	}
}

// TestMulti_AllBlobs_CallerEarlyCancel_OnError stops the merge on the
// first error yield. Covers `!yield(nil, h.err) { return }`.
func TestMulti_AllBlobs_CallerEarlyCancel_OnError(t *testing.T) {
	boom := errors.New("immediate boom")

	storeA := &multiModeStub{
		allBlobsSeq: func(yield func(domain_interfaces.MarklId, error) bool) {
			yield(nil, boom)
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

	var seenErrs []error
	for _, e := range m.AllBlobs() {
		if e != nil {
			seenErrs = append(seenErrs, e)
			break // cancel on first error
		}
	}

	if len(seenErrs) != 1 || !errors.Is(seenErrs[0], boom) {
		t.Fatalf("AllBlobs early-cancel-on-error: got %v, want [%v]", seenErrs, boom)
	}
}

// hasBlobCountingStub is a multiModeStub variant whose HasBlob also
// counts how many times it was probed. Used by short-circuit tests
// that need to assert later children are never asked.
type hasBlobCountingStub struct {
	*multiModeStub
	probeCount int
}

func (s *hasBlobCountingStub) HasBlob(id domain_interfaces.MarklId) bool {
	s.probeCount++
	return s.multiModeStub.HasBlob(id)
}

// TestMulti_Mirror_HasBlob_ShortCircuitsOnFirstHit pins the
// manpage's HASBLOB claim: in Mirror mode, a positive answer from
// the first child must terminate the scan; the second child must
// never see a probe.
func TestMulti_Mirror_HasBlob_ShortCircuitsOnFirstHit(t *testing.T) {
	id := makeMultiMirrorTestId(t, "mirror-hasblob-short-circuit")
	idKey := id.String()

	storeA := &hasBlobCountingStub{
		multiModeStub: &multiModeStub{hasIds: map[string]bool{idKey: true}},
	}
	storeB := &hasBlobCountingStub{
		multiModeStub: &multiModeStub{hasIds: map[string]bool{idKey: true}},
	}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if !m.HasBlob(id) {
		t.Fatal("HasBlob: got false, want true (first child has it)")
	}
	if storeA.probeCount != 1 {
		t.Fatalf("storeA probe count: got %d, want 1", storeA.probeCount)
	}
	if storeB.probeCount != 0 {
		t.Fatalf(
			"storeB probe count: got %d, want 0 (short-circuit after first hit)",
			storeB.probeCount,
		)
	}
}

// TestMulti_WriteThrough_HasBlob_ShortCircuitsOnWriteStore pins the
// write-through variant: a hit on the write store must terminate the
// scan before any read source is probed.
func TestMulti_WriteThrough_HasBlob_ShortCircuitsOnWriteStore(t *testing.T) {
	id := makeMultiMirrorTestId(t, "wt-hasblob-short-circuit-write")
	idKey := id.String()

	writeStore := &hasBlobCountingStub{
		multiModeStub: &multiModeStub{hasIds: map[string]bool{idKey: true}},
	}
	readSrc := &hasBlobCountingStub{
		multiModeStub: &multiModeStub{hasIds: map[string]bool{idKey: true}},
	}

	m, err := NewMulti(&spyActiveContext{}).
		WriteTo(BlobStoreInitialized{BlobStore: writeStore}).
		Read(BlobStoreInitialized{BlobStore: readSrc}).
		ReadFill(false).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if !m.HasBlob(id) {
		t.Fatal("HasBlob: got false, want true (write store has it)")
	}
	if writeStore.probeCount != 1 {
		t.Fatalf("write store probe count: got %d, want 1", writeStore.probeCount)
	}
	if readSrc.probeCount != 0 {
		t.Fatalf(
			"read source probe count: got %d, want 0 (short-circuit on write hit)",
			readSrc.probeCount,
		)
	}
}

// mismatchedIdWriter returns whatever MarklId it was constructed with.
// Two of these with different ids drive the
// multiStoreBlobWriter.GetMarklId panic-on-mismatch contract.
type mismatchedIdWriter struct {
	id domain_interfaces.MarklId
}

func (w *mismatchedIdWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *mismatchedIdWriter) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(struct{ io.Writer }{w}, r)
}
func (w *mismatchedIdWriter) Close() error                          { return nil }
func (w *mismatchedIdWriter) GetMarklId() domain_interfaces.MarklId { return w.id }

// TestMulti_Mirror_MultiStoreBlobWriter_GetMarklId_PanicsOnMismatch
// pins the manpage's contract that GetMarklId panics when children
// disagree on the computed id — every child consumed the same bytes
// via io.MultiWriter and was created with the same hash type, so a
// mismatch is a contract violation rather than a recoverable state.
func TestMulti_Mirror_MultiStoreBlobWriter_GetMarklId_PanicsOnMismatch(t *testing.T) {
	idA := makeMultiMirrorTestId(t, "mwriter-getmarklid-mismatch-A")
	idB := makeMultiMirrorTestId(t, "mwriter-getmarklid-mismatch-B")
	if idA.String() == idB.String() {
		t.Fatal("test setup: mismatched ids must differ")
	}

	writerA := &mismatchedIdWriter{id: idA}
	writerB := &mismatchedIdWriter{id: idB}

	storeA := &writerOverrideStub{
		multiModeStub:  &multiModeStub{},
		overrideWriter: writerA,
	}
	storeB := &writerOverrideStub{
		multiModeStub:  &multiModeStub{},
		overrideWriter: writerB,
	}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	mw, err := m.MakeBlobWriter(markl.FormatHashSha256)
	if err != nil {
		t.Fatalf("MakeBlobWriter: %v", err)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("GetMarklId: got no panic, want panic on child mismatch")
		}
	}()
	_ = mw.GetMarklId()
}

// TestMulti_Mirror_SingleChild_RoundTrip pins the degenerate-case
// claim: Mirror with one child Builds, reads, writes, and merges
// AllBlobs as a thin pass-through.
func TestMulti_Mirror_SingleChild_RoundTrip(t *testing.T) {
	id := makeMultiMirrorTestId(t, "mirror-single-child")
	idKey := id.String()
	payload := []byte("single-mirror-payload")
	storeWriter := &spyBlobWriter{}

	store := &multiModeStub{
		hasIds:       map[string]bool{idKey: true},
		readerBytes:  map[string][]byte{idKey: payload},
		writerToHand: storeWriter,
		allBlobsSeq:  makeMarklIdSeq(id),
	}

	m, err := NewMulti(&spyActiveContext{}).
		Mirror(BlobStoreInitialized{BlobStore: store}).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if !m.HasBlob(id) {
		t.Fatal("HasBlob: got false, want true")
	}

	reader, err := m.MakeBlobReader(id)
	if err != nil {
		t.Fatalf("MakeBlobReader: %v", err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("reader.Close: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("reader bytes: got %q, want %q", got, payload)
	}

	writer, err := m.MakeBlobWriter(markl.FormatHashSha256)
	if err != nil {
		t.Fatalf("MakeBlobWriter: %v", err)
	}
	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close: %v", err)
	}
	if !bytes.Equal(storeWriter.received, payload) {
		t.Fatalf("written bytes: got %q, want %q", storeWriter.received, payload)
	}

	ids, errs := drainAllBlobs(m.AllBlobs())
	if len(errs) != 0 {
		t.Fatalf("AllBlobs errors: %v", errs)
	}
	if len(ids) != 1 || ids[0] != id.String() {
		t.Fatalf("AllBlobs ids: got %v, want [%s]", ids, id.String())
	}
}

// TestMulti_WriteThrough_ZeroReadStores_Build pins the degenerate
// case claim: WriteTo(store).Build() with no Read calls succeeds and
// behaves as a single-store wrapper — reads come from the write
// store, all-miss returns ErrBlobMissing.
func TestMulti_WriteThrough_ZeroReadStores_Build(t *testing.T) {
	idHit := makeMultiMirrorTestId(t, "wt-zero-reads-hit")
	idMiss := makeMultiMirrorTestId(t, "wt-zero-reads-miss")
	hitKey := idHit.String()
	payload := []byte("zero-reads-payload")

	writeStore := &multiModeStub{
		hasIds:      map[string]bool{hitKey: true},
		readerBytes: map[string][]byte{hitKey: payload},
	}

	m, err := NewMulti(&spyActiveContext{}).
		WriteTo(BlobStoreInitialized{BlobStore: writeStore}).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if !m.HasBlob(idHit) {
		t.Fatal("HasBlob(idHit): got false, want true")
	}

	reader, err := m.MakeBlobReader(idHit)
	if err != nil {
		t.Fatalf("MakeBlobReader(idHit): %v", err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("reader.Close: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("reader bytes: got %q, want %q", got, payload)
	}

	if _, gotErr := m.MakeBlobReader(idMiss); !errors.Is(
		gotErr, blob_io.ErrBlobMissing{},
	) {
		t.Fatalf("MakeBlobReader(idMiss): got %v, want ErrBlobMissing", gotErr)
	}
}

// errorOnCloseWriter is a spyBlobWriter that always returns an error
// from Close. Used to exercise the mirror writer Close fan-out error
// path.
type errorOnCloseWriter struct {
	spyBlobWriter
}

func (w *errorOnCloseWriter) Close() error {
	w.closed.Store(true)
	w.closeCount.Add(1)
	return errors.New("close boom")
}

// TestMulti_Mirror_MultiStoreBlobWriter_CloseChildError pins that an
// error from a child writer's Close surfaces from the multiStoreBlobWriter's
// Close (the err-from-childWriter.Close branch).
func TestMulti_Mirror_MultiStoreBlobWriter_CloseChildError(t *testing.T) {
	failing := &errorOnCloseWriter{}
	good := &spyBlobWriter{}

	storeA := &multiModeStub{writerToHand: good}
	storeB := &multiModeStub{}
	storeB.writerToHand = nil
	// Wire storeB.MakeBlobWriter to return the failing writer directly
	// without spyBlobWriter wrapping.
	storeBWrap := &writerOverrideStub{
		multiModeStub:  storeB,
		overrideWriter: failing,
	}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeBWrap},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	writer, err := m.MakeBlobWriter(markl.FormatHashSha256)
	if err != nil {
		t.Fatalf("MakeBlobWriter: %v", err)
	}

	if _, err := writer.Write([]byte("payload")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := writer.Close(); err == nil {
		t.Fatal("Close: got nil, want error from failing child Close")
	}
}

// writerOverrideStub wraps multiModeStub so MakeBlobWriter returns a
// pre-built BlobWriter (e.g. errorOnCloseWriter) rather than the
// stub's spyBlobWriter. Defined here so it can reference the failing
// writer concrete type from the same _test.go file.
type writerOverrideStub struct {
	*multiModeStub
	overrideWriter domain_interfaces.BlobWriter
}

func (s *writerOverrideStub) MakeBlobWriter(
	_ domain_interfaces.FormatHash,
) (domain_interfaces.BlobWriter, error) {
	s.makeWriterCount++
	return s.overrideWriter, nil
}

// markIdGetterWriter is a BlobWriter that returns a fixed MarklId.
// Used to exercise multiStoreBlobWriter.GetMarklId aggregation.
type markIdGetterWriter struct {
	id domain_interfaces.MarklId
}

func (w *markIdGetterWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *markIdGetterWriter) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(struct{ io.Writer }{w}, r)
}
func (w *markIdGetterWriter) Close() error                          { return nil }
func (w *markIdGetterWriter) GetMarklId() domain_interfaces.MarklId { return w.id }

// TestMulti_Mirror_MultiStoreBlobWriter_GetMarklId_FirstAndEqual pins
// that the aggregated GetMarklId returns the first child's id when all
// children agree on the same id. Exercises the first-time-set and
// markl.AssertEqual happy-path branches.
func TestMulti_Mirror_MultiStoreBlobWriter_GetMarklId_FirstAndEqual(t *testing.T) {
	// Use real markl ids so AssertEqual takes the bytes-equal path.
	id := makeMultiMirrorTestId(t, "mwriter-getmarklid")

	writerA := &markIdGetterWriter{id: id}
	writerB := &markIdGetterWriter{id: id}

	storeA := &writerOverrideStub{multiModeStub: &multiModeStub{}, overrideWriter: writerA}
	storeB := &writerOverrideStub{multiModeStub: &multiModeStub{}, overrideWriter: writerB}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	mw, err := m.MakeBlobWriter(markl.FormatHashSha256)
	if err != nil {
		t.Fatalf("MakeBlobWriter: %v", err)
	}

	got := mw.GetMarklId()
	if got == nil || got.String() != id.String() {
		t.Fatalf("GetMarklId: got %v, want %v", got, id)
	}
}

// TestMulti_Mirror_MultiStoreBlobWriter_ReadFrom pins that calling
// ReadFrom on the multi writer copies the source bytes through to the
// io.MultiWriter sink (exercises the ReadFrom Copy path).
func TestMulti_Mirror_MultiStoreBlobWriter_ReadFrom(t *testing.T) {
	writerA := &spyBlobWriter{}
	writerB := &spyBlobWriter{}

	storeA := &multiModeStub{writerToHand: writerA}
	storeB := &multiModeStub{writerToHand: writerB}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	mw, err := m.MakeBlobWriter(markl.FormatHashSha256)
	if err != nil {
		t.Fatalf("MakeBlobWriter: %v", err)
	}

	rf, ok := mw.(io.ReaderFrom)
	if !ok {
		t.Fatalf("multi writer does not implement io.ReaderFrom")
	}

	payload := []byte("read-from-payload")
	n, err := rf.ReadFrom(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("ReadFrom n: got %d, want %d", n, len(payload))
	}

	if !bytes.Equal(writerA.received, payload) {
		t.Errorf("storeA bytes: got %q, want %q", writerA.received, payload)
	}
	if !bytes.Equal(writerB.received, payload) {
		t.Errorf("storeB bytes: got %q, want %q", writerB.received, payload)
	}
}

// TestMulti_Mirror_MultiStoreBlobWriter_ReadFrom_ChildWriteError pins
// that an error from a child writer during the io.Copy fan-out
// surfaces from ReadFrom (the err-wrap branch in ReadFrom).
func TestMulti_Mirror_MultiStoreBlobWriter_ReadFrom_ChildWriteError(t *testing.T) {
	good := &spyBlobWriter{}
	failing := &spyBlobWriter{failAfterBytes: 1}

	storeA := &multiModeStub{writerToHand: good}
	storeB := &multiModeStub{writerToHand: failing}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	mw, err := m.MakeBlobWriter(markl.FormatHashSha256)
	if err != nil {
		t.Fatalf("MakeBlobWriter: %v", err)
	}

	rf := mw.(io.ReaderFrom)
	_, err = rf.ReadFrom(bytes.NewReader([]byte("payload bigger than 1 byte")))
	if err == nil {
		t.Fatal("ReadFrom: got nil, want error from failing child write")
	}
}

// TestMulti_Mirror_MakeBlobReader_UnavailableFallsThrough pins the
// #209 contract: a child that returns blob_io.ErrBlobStoreUnavailable
// from MakeBlobReader is miss-equivalent and the next child must be
// consulted. Without the fix Multi propagates the error and the
// caller never sees the available sibling's bytes.
func TestMulti_Mirror_MakeBlobReader_UnavailableFallsThrough(t *testing.T) {
	id := makeMultiMirrorTestId(t, "mirror-reader-unavailable-fallthrough")
	idKey := id.String()

	payload := []byte("from-available-store")

	// storeA claims the blob but returns an unavailability error on
	// reader open (simulating a dial/handshake/auth failure that
	// passed HasBlob's cheap probe but failed when reaching for
	// actual bytes).
	storeA := &multiModeStub{
		hasIds: map[string]bool{idKey: true},
		makeReaderErr: blob_io.ErrBlobStoreUnavailable{
			StoreId: "remote-archive",
			Reason:  "ssh dial",
			Cause:   errors.New("ssh: handshake failed"),
		},
	}
	storeB := &multiModeStub{
		hasIds:      map[string]bool{idKey: true},
		readerBytes: map[string][]byte{idKey: payload},
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
		t.Fatalf("MakeBlobReader: %v (want fallthrough to available sibling)", err)
	}
	defer reader.Close() //defer:err-checked

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("reader bytes: got %q, want %q", got, payload)
	}
}

// TestMulti_Mirror_MakeBlobReader_AllUnavailable_ReturnsMissing pins
// that when every child is unavailable, Multi reports ErrBlobMissing
// (not the underlying unavailability error) — the contract is "no
// store can serve this blob," and the caller layer above handles the
// miss case uniformly.
func TestMulti_Mirror_MakeBlobReader_AllUnavailable_ReturnsMissing(t *testing.T) {
	id := makeMultiMirrorTestId(t, "mirror-reader-all-unavailable")
	idKey := id.String()

	storeA := &multiModeStub{
		hasIds: map[string]bool{idKey: true},
		makeReaderErr: blob_io.ErrBlobStoreUnavailable{
			StoreId: "remote-A",
			Cause:   errors.New("ssh: handshake failed"),
		},
	}
	storeB := &multiModeStub{
		hasIds: map[string]bool{idKey: true},
		makeReaderErr: blob_io.ErrBlobStoreUnavailable{
			StoreId: "remote-B",
			Cause:   errors.New("dial tcp: connection refused"),
		},
	}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	reader, gotErr := m.MakeBlobReader(id)
	if reader != nil {
		t.Fatalf("MakeBlobReader: got non-nil reader, want nil on all-unavailable")
	}
	if !errors.Is(gotErr, blob_io.ErrBlobMissing{}) {
		t.Fatalf("MakeBlobReader error: got %v, want ErrBlobMissing", gotErr)
	}
}

// TestMulti_WriteThrough_MakeBlobReader_UnavailableReadSource_FallsThrough
// pins the same #209 contract on the WriteThrough read-source loop:
// an unavailable read source falls through to the next configured
// source.
func TestMulti_WriteThrough_MakeBlobReader_UnavailableReadSource_FallsThrough(t *testing.T) {
	id := makeMultiMirrorTestId(t, "wt-reader-unavailable-fallthrough")
	idKey := id.String()

	payload := []byte("from-second-read-source")

	writeStore := &multiModeStub{hasIds: map[string]bool{idKey: false}}
	readA := &multiModeStub{
		hasIds: map[string]bool{idKey: true},
		makeReaderErr: blob_io.ErrBlobStoreUnavailable{
			StoreId: "remote-A",
			Cause:   errors.New("ssh: unable to authenticate"),
		},
	}
	readB := &multiModeStub{
		hasIds:      map[string]bool{idKey: true},
		readerBytes: map[string][]byte{idKey: payload},
	}

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

	reader, err := m.MakeBlobReader(id)
	if err != nil {
		t.Fatalf("MakeBlobReader: %v (want fallthrough to readB)", err)
	}
	defer reader.Close() //defer:err-checked

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("reader bytes: got %q, want %q", got, payload)
	}
}

// TestMulti_WriteThrough_MakeBlobReader_UnavailableWriteStore_FallsThrough
// pins that an unavailable write store (HasBlob succeeded as a
// best-effort, then MakeBlobReader fails with unavailability) falls
// through to the read sources rather than hard-failing the read.
func TestMulti_WriteThrough_MakeBlobReader_UnavailableWriteStore_FallsThrough(t *testing.T) {
	id := makeMultiMirrorTestId(t, "wt-reader-unavailable-write-store")
	idKey := id.String()

	payload := []byte("from-read-source")

	writeStore := &multiModeStub{
		hasIds: map[string]bool{idKey: true},
		makeReaderErr: blob_io.ErrBlobStoreUnavailable{
			StoreId: "write-store",
			Cause:   errors.New("ssh: handshake failed"),
		},
	}
	readSrc := &multiModeStub{
		hasIds:      map[string]bool{idKey: true},
		readerBytes: map[string][]byte{idKey: payload},
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
		t.Fatalf("MakeBlobReader: %v (want fallthrough to read source)", err)
	}
	defer reader.Close() //defer:err-checked

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("reader bytes: got %q, want %q", got, payload)
	}
}

// TestMulti_Mirror_MakeBlobReader_NonUnavailableErrorPropagates pins
// that errors which are NOT unavailability (e.g. I/O corruption,
// permission denied, unexpected schema) still surface as-is and do
// NOT silently swallow into a miss. Guards against the classifier
// being broadened too far in a future change.
func TestMulti_Mirror_MakeBlobReader_NonUnavailableErrorPropagates(t *testing.T) {
	id := makeMultiMirrorTestId(t, "mirror-reader-real-error")
	idKey := id.String()

	realErr := errors.New("blob corruption detected at offset 17")
	storeA := &multiModeStub{
		hasIds:        map[string]bool{idKey: true},
		makeReaderErr: realErr,
	}
	storeB := &multiModeStub{
		hasIds:      map[string]bool{idKey: true},
		readerBytes: map[string][]byte{idKey: []byte("never-reached")},
	}

	m, err := NewMulti(&spyActiveContext{}).Mirror(
		BlobStoreInitialized{BlobStore: storeA},
		BlobStoreInitialized{BlobStore: storeB},
	).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	reader, gotErr := m.MakeBlobReader(id)
	if reader != nil {
		t.Fatalf("MakeBlobReader: got non-nil reader, want nil on real error")
	}
	if !errors.Is(gotErr, realErr) {
		t.Fatalf("MakeBlobReader error: got %v, want %v", gotErr, realErr)
	}
}
