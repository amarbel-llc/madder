//go:build test

package blob_stores

import (
	"bytes"
	"io"
	"testing"
)

func TestTee_ReadCopiesToSink(t *testing.T) {
	id := makeMultiMirrorTestId(t, "tee-read-copies")
	src := newControllableBlobReader(id)
	src.Feed([]byte("hello world"))
	src.Close()

	sink := &spyBlobWriter{}

	tee := newTeeBlobReader(&spyActiveContext{}, src, sink, id)

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

	tee := newTeeBlobReader(&spyActiveContext{}, src, sink, id)

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

	tee := newTeeBlobReader(&spyActiveContext{}, src, sink, id)

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

	tee := newTeeBlobReader(&spyActiveContext{}, src, sink, id)

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

	tee := newTeeBlobReader(&spyActiveContext{}, src, sink, id)

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

	tee := newTeeBlobReader(&spyActiveContext{}, src, sink, expected)
	if _, err := io.ReadAll(tee); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	err := tee.Close()
	if err == nil {
		t.Fatal("expected digest mismatch error; got nil")
	}
}

func TestTee_DoubleClose_IsNoop(t *testing.T) {
	id := makeMultiMirrorTestId(t, "tee-double-close")
	src := newControllableBlobReader(id)
	src.Feed([]byte("data"))
	src.Close()

	sink := &spyBlobWriter{}

	tee := newTeeBlobReader(&spyActiveContext{}, src, sink, id)

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
