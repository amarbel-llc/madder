//go:build test

package blob_stores

import (
	"bytes"
	"io"
	"math/rand"
	"testing"
	"time"

	"code.linenisgreat.com/piggy/go/pkgs/markl"
)

// The store's own writer and reader must overlap SFTP round trips for a
// blob larger than one packet. pkg/sftp only pipelines a Write or Read
// whose buffer is LARGER than its 32 KiB packet; anything smaller is one
// request and one wait. Handing it 32 KiB pieces therefore moves a blob at
// one packet per round trip whatever the link can carry, which is what a
// real 100 ms link showed (about 0.37 MB/s).
//
// The harness injects 5 ms each way. An 8 MiB blob is 256 packets, so
// stop-and-wait costs at least 256 round trips = 2.56 s; pipelined it is a
// handful. The 1 s bound sits far from both.
const (
	pipeliningPayloadSize = 8 << 20
	pipeliningBound       = time.Second
)

func pipeliningPayload() []byte {
	payload := make([]byte, pipeliningPayloadSize)
	rand.New(rand.NewSource(1)).Read(payload)

	return payload
}

func TestSftpBlobWriter_PipelinesLargeBlobs(t *testing.T) {
	h := newSFTPBenchHarness(t, benchLatency, concurrentClientOptions()...)
	defer h.Close()

	store := newSftpHashTestStore(
		t, h.client, h.remotePath, true, markl.FormatHashBlake2b256,
	)

	payload := pipeliningPayload()

	writer, err := store.MakeBlobWriter(markl.FormatHashBlake2b256)
	if err != nil {
		t.Fatalf("MakeBlobWriter: %v", err)
	}

	start := time.Now()

	// Modest writes, as the compress and encrypt layers issue them. One
	// huge Write would skip the store's buffer and pipeline by itself,
	// hiding the very thing under test.
	if _, err = io.CopyBuffer(
		struct{ io.Writer }{writer},
		struct{ io.Reader }{bytes.NewReader(payload)},
		make([]byte, 16<<10),
	); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	if err = writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	elapsed := time.Since(start)
	t.Logf("wrote %d bytes in %s", len(payload), elapsed)

	if elapsed > pipeliningBound {
		t.Errorf(
			"writing %d bytes took %s, want under %s: writes are not pipelined",
			len(payload), elapsed, pipeliningBound,
		)
	}

	// Pipelined writes land out of order on the wire; the blob must still
	// be byte for byte what was written.
	reader, err := store.MakeBlobReader(writer.GetMarklId())
	if err != nil {
		t.Fatalf("MakeBlobReader: %v", err)
	}
	defer reader.Close() //defer:err-checked

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if !bytes.Equal(got, payload) {
		t.Fatalf("read back %d bytes that differ from the %d written", len(got), len(payload))
	}

	if !bytes.Equal(reader.GetMarklId().GetBytes(), writer.GetMarklId().GetBytes()) {
		t.Fatalf("digest read back differs from digest written")
	}
}

func TestSftpBlobReader_PipelinesLargeBlobs(t *testing.T) {
	h := newSFTPBenchHarness(t, benchLatency, concurrentClientOptions()...)
	defer h.Close()

	store := newSftpHashTestStore(
		t, h.client, h.remotePath, true, markl.FormatHashBlake2b256,
	)

	payload := pipeliningPayload()

	writer, err := store.MakeBlobWriter(markl.FormatHashBlake2b256)
	if err != nil {
		t.Fatalf("MakeBlobWriter: %v", err)
	}

	if _, err = io.Copy(writer, bytes.NewReader(payload)); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	if err = writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reader, err := store.MakeBlobReader(writer.GetMarklId())
	if err != nil {
		t.Fatalf("MakeBlobReader: %v", err)
	}
	defer reader.Close() //defer:err-checked

	start := time.Now()

	// Small reads, as the decrypt and decompress layers issue them.
	var got bytes.Buffer

	if _, err = io.CopyBuffer(
		&got,
		struct{ io.Reader }{reader},
		make([]byte, 4096),
	); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	elapsed := time.Since(start)
	t.Logf("read %d bytes in %s", len(payload), elapsed)

	if elapsed > pipeliningBound {
		t.Errorf(
			"reading %d bytes took %s, want under %s: reads are not pipelined",
			len(payload), elapsed, pipeliningBound,
		)
	}

	if !bytes.Equal(got.Bytes(), payload) {
		t.Fatalf("read %d bytes that differ from the %d written", got.Len(), len(payload))
	}
}

// A blob that fits in one packet must still cost one read request, not a
// burst of speculative ones: most blobs are small, and the remote answers
// each request.
func TestSftpBlobReader_SmallBlobReadsOnce(t *testing.T) {
	h := newSFTPBenchHarness(t, 0, concurrentClientOptions()...)
	defer h.Close()

	store := newSftpHashTestStore(
		t, h.client, h.remotePath, true, markl.FormatHashBlake2b256,
	)

	payload := []byte("a small blob")

	writer, err := store.MakeBlobWriter(markl.FormatHashBlake2b256)
	if err != nil {
		t.Fatalf("MakeBlobWriter: %v", err)
	}

	if _, err = writer.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err = writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reader, err := store.MakeBlobReader(writer.GetMarklId())
	if err != nil {
		t.Fatalf("MakeBlobReader: %v", err)
	}
	defer reader.Close() //defer:err-checked

	file := reader.(*sftpReader).source

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if !bytes.Equal(got, payload) {
		t.Fatalf("read %q, want %q", got, payload)
	}

	if file.largestFill > sftpPacketSize {
		t.Errorf(
			"a %d-byte blob was read with a %d-byte request, want at most one packet (%d)",
			len(payload), file.largestFill, sftpPacketSize,
		)
	}
}

// The read-ahead must stay a faithful Reader, ReaderAt and Seeker over its
// file: ReadAt does not move the stream, and Seek accounts for what is
// buffered but unread.
func TestSftpReadAhead_SeekAndReadAt(t *testing.T) {
	payload := pipeliningPayload()[:200_000]

	reader := newSftpReadAhead(bytes.NewReader(payload))
	defer reader.release()

	var err error

	head := make([]byte, 100)
	if _, err = io.ReadFull(reader, head); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}

	at := make([]byte, 50)
	if _, err = reader.ReadAt(at, 150_000); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}

	if !bytes.Equal(at, payload[150_000:150_050]) {
		t.Fatalf("ReadAt returned the wrong bytes")
	}

	// ReadAt must not have moved the stream.
	next := make([]byte, 100)
	if _, err = io.ReadFull(reader, next); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}

	if !bytes.Equal(next, payload[100:200]) {
		t.Fatalf("stream position moved after ReadAt")
	}

	if _, err = reader.Seek(70_000, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}

	sought := make([]byte, 100)
	if _, err = io.ReadFull(reader, sought); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}

	if !bytes.Equal(sought, payload[70_000:70_100]) {
		t.Fatalf("read after Seek returned the wrong bytes")
	}

	// A relative seek is relative to the stream, not to how far the
	// read-ahead has pulled from the file.
	if _, err = reader.Seek(900, io.SeekCurrent); err != nil {
		t.Fatalf("Seek: %v", err)
	}

	if _, err = io.ReadFull(reader, sought); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}

	if !bytes.Equal(sought, payload[71_000:71_100]) {
		t.Fatalf("read after a relative Seek returned the wrong bytes")
	}

	// And it reads to the end, byte for byte, across the switch from
	// one-packet fills to full windows.
	if _, err = reader.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}

	all, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if !bytes.Equal(all, payload) {
		t.Fatalf("read %d bytes that differ from the %d in the file", len(all), len(payload))
	}
}
