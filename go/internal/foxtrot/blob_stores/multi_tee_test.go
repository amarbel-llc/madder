//go:build test

package blob_stores

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"testing"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

// firingActiveContext is a spyActiveContext that actually invokes its
// registered After callbacks when FireAfter is called. Lives in this
// _test.go file (not multi_test_helpers.go) because spyActiveContext
// is itself defined in a _test.go file — embedding it from a
// build-tag-gated regular package file breaks non-test compilation
// of the package under -tags test.
type firingActiveContext struct {
	spyActiveContext
	afterFuncs []interfaces.FuncActiveContext
}

func (c *firingActiveContext) After(f interfaces.FuncActiveContext) {
	c.afterFuncs = append(c.afterFuncs, f)
}

func (c *firingActiveContext) FireAfter() {
	for _, f := range c.afterFuncs {
		_ = f(c)
	}
}

func TestTee_ReadCopiesToSink(t *testing.T) {
	id := makeMultiMirrorTestId(t, "tee-read-copies")
	src := newControllableBlobReader(id)
	src.Feed([]byte("hello world"))
	src.Close()

	sink := &spyBlobWriter{}

	tee := newTeeBlobReader(&spyActiveContext{}, src, sink, id, nil)

	got, err := io.ReadAll(tee)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if !bytes.Equal(got, []byte("hello world")) {
		t.Fatalf("reader bytes: got %q, want %q", got, "hello world")
	}

	sink.mu.Lock()
	received := append([]byte(nil), sink.received...)
	sink.mu.Unlock()
	if !bytes.Equal(received, []byte("hello world")) {
		t.Fatalf("sink bytes: got %q, want %q", received, "hello world")
	}
}

func TestTee_CallerClose_CommitsSink(t *testing.T) {
	id := makeMultiMirrorTestId(t, "tee-close-commits")
	src := newControllableBlobReader(id)
	src.Feed([]byte("payload"))
	src.Close()

	sink := &spyBlobWriter{}

	tee := newTeeBlobReader(&spyActiveContext{}, src, sink, id, nil)

	if _, err := io.ReadAll(tee); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if err := tee.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !sink.closed.Load() {
		t.Fatalf("sink closed: got false, want true")
	}
}

func TestTee_SinkWriteError_PoisonsButReadContinues(t *testing.T) {
	id := makeMultiMirrorTestId(t, "tee-sink-poisoned")
	src := newControllableBlobReader(id)
	src.Feed([]byte("hello world"))
	src.Close()

	sink := &spyBlobWriter{failAfterBytes: 3}

	tee := newTeeBlobReader(&spyActiveContext{}, src, sink, id, nil)

	got, err := io.ReadAll(tee)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if !bytes.Equal(got, []byte("hello world")) {
		t.Fatalf("reader bytes: got %q, want %q (full source despite sink failure)", got, "hello world")
	}

	if err := tee.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestTee_GetMarklId_DelegatesToSource(t *testing.T) {
	id := makeMultiMirrorTestId(t, "tee-markl-id")
	src := newControllableBlobReader(id)
	sink := &spyBlobWriter{}

	tee := newTeeBlobReader(&spyActiveContext{}, src, sink, id, nil)

	got := tee.GetMarklId()
	if got == nil || got.String() != id.String() {
		t.Fatalf("GetMarklId: got %v, want %v", got, id)
	}
}

// TestTee_PartialRead_FlushAndCommitDrainsRest pins that when a caller
// reads only part of the source and abandons the reader, flushAndCommit
// (the ctx.After path) drains the rest through the tee so the sink
// ends up with the complete payload. Without the drain, sink would
// hold only the bytes the caller actively read.
func TestTee_PartialRead_FlushAndCommitDrainsRest(t *testing.T) {
	id := makeMultiMirrorTestId(t, "tee-partial-drain")
	src := newControllableBlobReader(id)
	src.Feed([]byte("hello world"))
	src.Close()

	sink := &spyBlobWriter{}

	tee := newTeeBlobReader(&spyActiveContext{}, src, sink, id, nil)

	buf := make([]byte, 5)
	if _, err := tee.Read(buf); err != nil {
		t.Fatalf("partial Read: %v", err)
	}

	if err := tee.flushAndCommit(); err != nil {
		t.Fatalf("flushAndCommit: %v", err)
	}

	sink.mu.Lock()
	received := append([]byte(nil), sink.received...)
	sink.mu.Unlock()
	if !bytes.Equal(received, []byte("hello world")) {
		t.Fatalf("sink bytes after drain: got %q, want %q", received, "hello world")
	}
	if !sink.closed.Load() {
		t.Fatalf("sink closed: got false, want true")
	}
}

// TestTee_DigestMismatch_SameHash_ReturnsError pins the detect-and-report
// digest verification: when expected is set and the sink's computed
// MarklId differs from expected under the same hash format,
// flushAndCommit (and thus Close) must surface a digest-mismatch error.
// Cross-hash mismatches are out of scope — Task 12 wires
// BlobForeignDigestAdder.
func TestTee_DigestMismatch_SameHash_ReturnsError(t *testing.T) {
	expected := makeMultiMirrorTestId(t, "tee-mismatch-expected")
	actual := makeMultiMirrorTestId(t, "tee-mismatch-actual")
	if expected.GetMarklFormat().GetMarklFormatId() !=
		actual.GetMarklFormat().GetMarklFormatId() {
		t.Fatalf(
			"test setup: expected and actual must share a hash format; got %s vs %s",
			expected.GetMarklFormat().GetMarklFormatId(),
			actual.GetMarklFormat().GetMarklFormatId(),
		)
	}

	src := newControllableBlobReader(expected)
	src.Feed([]byte("payload"))
	src.Close()

	sink := &spyBlobWriter{computedId: actual}

	tee := newTeeBlobReader(&spyActiveContext{}, src, sink, expected, nil)
	if _, err := io.ReadAll(tee); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	err := tee.Close()
	if err == nil {
		t.Fatal("expected digest mismatch error; got nil")
	}
}

func TestTee_PartialDrain_CtxAfterDrainsRest(t *testing.T) {
	id := makeMultiMirrorTestId(t, "tee-partial-ctx-after")
	src := newControllableBlobReader(id)
	src.Feed([]byte("hello world"))
	src.Close()

	sink := &spyBlobWriter{}
	ctx := &firingActiveContext{}

	tee := newTeeBlobReader(ctx, src, sink, id, nil)

	buf := make([]byte, 3)
	if _, err := tee.Read(buf); err != nil {
		t.Fatalf("partial Read: %v", err)
	}

	ctx.FireAfter()

	sink.mu.Lock()
	received := append([]byte(nil), sink.received...)
	sink.mu.Unlock()
	if !bytes.Equal(received, []byte("hello world")) {
		t.Fatalf("sink bytes after ctx.After: got %q, want %q", received, "hello world")
	}
	if !sink.closed.Load() {
		t.Fatalf("sink closed after ctx.After: got false, want true")
	}
	_ = tee
}

func TestTee_CallerCloseFirst_CtxAfterIsNoop(t *testing.T) {
	id := makeMultiMirrorTestId(t, "tee-close-first-after-noop")
	src := newControllableBlobReader(id)
	src.Feed([]byte("payload"))
	src.Close()

	sink := &spyBlobWriter{}
	ctx := &firingActiveContext{}

	tee := newTeeBlobReader(ctx, src, sink, id, nil)

	if _, err := io.ReadAll(tee); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if err := tee.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ctx.FireAfter()

	if !sink.closed.Load() {
		t.Fatalf("sink closed: got false, want true")
	}
	if got := sink.closeCount.Load(); got != 1 {
		t.Fatalf("sink Close call count: got %d, want 1 (ctx.After must be a no-op after caller Close)", got)
	}
}

func TestTee_CallerNeverCloses_CtxAfterDrainsAndCloses(t *testing.T) {
	id := makeMultiMirrorTestId(t, "tee-never-close-after-drains")
	src := newControllableBlobReader(id)
	src.Feed([]byte("hello world"))
	src.Close()

	sink := &spyBlobWriter{}
	ctx := &firingActiveContext{}

	tee := newTeeBlobReader(ctx, src, sink, id, nil)

	ctx.FireAfter()

	sink.mu.Lock()
	received := append([]byte(nil), sink.received...)
	sink.mu.Unlock()
	if !bytes.Equal(received, []byte("hello world")) {
		t.Fatalf("sink bytes after ctx.After: got %q, want %q", received, "hello world")
	}
	if !sink.closed.Load() {
		t.Fatalf("sink closed after ctx.After: got false, want true")
	}
	_ = tee
}

func TestTee_DoubleClose_IsNoop(t *testing.T) {
	id := makeMultiMirrorTestId(t, "tee-double-close")
	src := newControllableBlobReader(id)
	src.Feed([]byte("data"))
	src.Close()

	sink := &spyBlobWriter{}

	tee := newTeeBlobReader(&spyActiveContext{}, src, sink, id, nil)

	if _, err := io.ReadAll(tee); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if err := tee.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := tee.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestMulti_WriteThrough_MakeBlobReader_ReadFill_WiresTee pins that when
// readFill is true and the blob is served from a read source, the
// returned reader bytes are tee'd into a writer constructed on the
// write store.
func TestMulti_WriteThrough_MakeBlobReader_ReadFill_WiresTee(t *testing.T) {
	id := makeMultiMirrorTestId(t, "wt-reader-readfill-tee")
	idKey := id.String()

	payload := []byte("read-source-bytes")

	writeStoreWriter := &spyBlobWriter{}
	writeStore := &multiModeStub{
		hasIds:       map[string]bool{idKey: false},
		writerToHand: writeStoreWriter,
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

	reader, err := m.MakeBlobReader(id)
	if err != nil {
		t.Fatalf("MakeBlobReader: %v", err)
	}

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if err := reader.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !bytes.Equal(got, payload) {
		t.Fatalf("reader bytes: got %q, want %q", got, payload)
	}

	if writeStore.makeWriterCount != 1 {
		t.Fatalf(
			"write store MakeBlobWriter count: got %d, want 1 (tee constructs one writer)",
			writeStore.makeWriterCount,
		)
	}

	writeStoreWriter.mu.Lock()
	received := append([]byte(nil), writeStoreWriter.received...)
	writeStoreWriter.mu.Unlock()
	if !bytes.Equal(received, payload) {
		t.Fatalf("write store sink bytes: got %q, want %q", received, payload)
	}

	if !writeStoreWriter.closed.Load() {
		t.Fatalf("write store sink closed: got false, want true (Close commits)")
	}
}

// foreignDigestAdderStub wraps multiModeStub with a BlobForeignDigestAdder
// implementation that records every (foreign, native) call. Lives in this
// _test.go file because it embeds multiModeStub, which is defined in
// multi_test.go.
type foreignDigestAdderStub struct {
	*multiModeStub

	mu    sync.Mutex
	calls []foreignDigestCall
}

type foreignDigestCall struct {
	foreign string
	native  string
}

func (s *foreignDigestAdderStub) AddForeignBlobDigestForNativeDigest(
	foreign, native domain_interfaces.MarklId,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, foreignDigestCall{
		foreign: foreign.String(),
		native:  native.String(),
	})
	return nil
}

func makeBlake2b256TestId(t *testing.T, seed string) domain_interfaces.MarklId {
	t.Helper()
	raw := sha256.Sum256([]byte(seed))
	id, repool := markl.FormatHashBlake2b256.GetBlobIdForHexString(
		hex.EncodeToString(raw[:]),
	)
	t.Cleanup(repool)
	return id
}

// TestTee_CrossHash_RegistersForeignDigestMapping pins that when the
// expected id (from the read source) and the sink's computed id are in
// different hash formats, flushAndCommit registers the cross-hash
// mapping with the write store via BlobForeignDigestAdder. Without the
// mapping a later lookup by either digest would not resolve the same
// physical blob.
func TestTee_CrossHash_RegistersForeignDigestMapping(t *testing.T) {
	expected := makeMultiMirrorTestId(t, "tee-cross-hash-expected") // sha256
	native := makeBlake2b256TestId(t, "tee-cross-hash-native")      // blake2b256

	if expected.GetMarklFormat().GetMarklFormatId() ==
		native.GetMarklFormat().GetMarklFormatId() {
		t.Fatalf(
			"test setup: expected and native must have different hash formats; both are %s",
			expected.GetMarklFormat().GetMarklFormatId(),
		)
	}

	src := newControllableBlobReader(expected)
	src.Feed([]byte("cross-hash-payload"))
	src.Close()

	sink := &spyBlobWriter{computedId: native}

	adder := &foreignDigestAdderStub{
		multiModeStub: &multiModeStub{writerToHand: sink},
	}

	tee := newTeeBlobReader(&spyActiveContext{}, src, sink, expected, adder)
	if _, err := io.ReadAll(tee); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if err := tee.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	adder.mu.Lock()
	calls := append([]foreignDigestCall(nil), adder.calls...)
	adder.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("adder calls: got %d, want 1 (calls=%v)", len(calls), calls)
	}
	if calls[0].foreign != expected.String() {
		t.Fatalf("foreign digest: got %s, want %s", calls[0].foreign, expected)
	}
	if calls[0].native != native.String() {
		t.Fatalf("native digest: got %s, want %s", calls[0].native, native)
	}
}

// erroringWriter returns a fixed error from Write. Used to exercise
// the tee's WriteTo werr-not-nil branch.
type erroringWriter struct {
	boom error
}

func (w erroringWriter) Write(_ []byte) (int, error) { return 0, w.boom }

// shortWriter reports writing fewer bytes than were given without
// returning an error. Used to exercise the tee's WriteTo nw<n branch,
// which surfaces io.ErrShortWrite.
type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

// readErrSource is a BlobReader that returns a non-EOF error on Read.
// Used to exercise the tee's WriteTo "err != nil && err != EOF" branch.
type readErrSource struct {
	*controllableBlobReader
	readErr error
}

func (r *readErrSource) Read(_ []byte) (int, error) { return 0, r.readErr }

// TestTee_WriteTo_DownstreamWriteError pins that an error from the
// destination writer surfaces from WriteTo (the werr-not-nil branch).
func TestTee_WriteTo_DownstreamWriteError(t *testing.T) {
	id := makeMultiMirrorTestId(t, "tee-writeto-werr")
	src := newControllableBlobReader(id)
	src.Feed([]byte("hello"))
	src.Close()

	sink := &spyBlobWriter{}
	tee := newTeeBlobReader(&spyActiveContext{}, src, sink, id, nil)

	boom := errors.New("downstream boom")
	_, err := tee.WriteTo(erroringWriter{boom: boom})
	if !errors.Is(err, boom) {
		t.Fatalf("WriteTo error: got %v, want %v", err, boom)
	}
}

// TestTee_WriteTo_ShortWrite pins that a destination writer reporting
// nw < n surfaces io.ErrShortWrite from WriteTo.
func TestTee_WriteTo_ShortWrite(t *testing.T) {
	id := makeMultiMirrorTestId(t, "tee-writeto-short")
	src := newControllableBlobReader(id)
	src.Feed([]byte("hello"))
	src.Close()

	sink := &spyBlobWriter{}
	tee := newTeeBlobReader(&spyActiveContext{}, src, sink, id, nil)

	_, err := tee.WriteTo(shortWriter{})
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("WriteTo error: got %v, want io.ErrShortWrite", err)
	}
}

// TestTee_WriteTo_SourceReadError pins that a non-EOF error from the
// source's Read surfaces from WriteTo (the err != nil && err != EOF
// branch).
func TestTee_WriteTo_SourceReadError(t *testing.T) {
	id := makeMultiMirrorTestId(t, "tee-writeto-read-err")
	inner := newControllableBlobReader(id)
	boom := errors.New("source read boom")
	src := &readErrSource{controllableBlobReader: inner, readErr: boom}

	sink := &spyBlobWriter{}
	tee := newTeeBlobReader(&spyActiveContext{}, src, sink, id, nil)

	var w bytes.Buffer
	_, err := tee.WriteTo(&w)
	if !errors.Is(err, boom) {
		t.Fatalf("WriteTo error: got %v, want %v", err, boom)
	}
}

// readAtSeekSource extends controllableBlobReader with stub ReadAt /
// Seek implementations that return distinguishable sentinel values so
// the tee's passthrough can be observed end-to-end.
type readAtSeekSource struct {
	*controllableBlobReader
	readAtN     int
	readAtErr   error
	readAtCalls int
	seekOff     int64
	seekErr     error
	seekCalls   int
}

func (r *readAtSeekSource) ReadAt(_ []byte, _ int64) (int, error) {
	r.readAtCalls++
	return r.readAtN, r.readAtErr
}

func (r *readAtSeekSource) Seek(_ int64, _ int) (int64, error) {
	r.seekCalls++
	return r.seekOff, r.seekErr
}

// TestTee_ReadAt_DelegatesToSource pins that ReadAt is a thin
// passthrough that returns the source's (n, err) verbatim.
func TestTee_ReadAt_DelegatesToSource(t *testing.T) {
	id := makeMultiMirrorTestId(t, "tee-readat-delegate")
	inner := newControllableBlobReader(id)
	src := &readAtSeekSource{
		controllableBlobReader: inner,
		readAtN:                7,
		readAtErr:              io.EOF,
	}

	tee := newTeeBlobReader(&spyActiveContext{}, src, &spyBlobWriter{}, id, nil)

	buf := make([]byte, 8)
	n, err := tee.ReadAt(buf, 42)
	if n != 7 || !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAt: got (%d, %v), want (7, EOF)", n, err)
	}
	if src.readAtCalls != 1 {
		t.Fatalf("source ReadAt calls: got %d, want 1", src.readAtCalls)
	}
}

// TestTee_Seek_DelegatesToSource pins that Seek is a thin passthrough.
func TestTee_Seek_DelegatesToSource(t *testing.T) {
	id := makeMultiMirrorTestId(t, "tee-seek-delegate")
	inner := newControllableBlobReader(id)
	src := &readAtSeekSource{
		controllableBlobReader: inner,
		seekOff:                99,
	}

	tee := newTeeBlobReader(&spyActiveContext{}, src, &spyBlobWriter{}, id, nil)

	off, err := tee.Seek(42, io.SeekStart)
	if off != 99 || err != nil {
		t.Fatalf("Seek: got (%d, %v), want (99, nil)", off, err)
	}
	if src.seekCalls != 1 {
		t.Fatalf("source Seek calls: got %d, want 1", src.seekCalls)
	}
}
