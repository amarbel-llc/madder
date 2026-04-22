//go:build test

package env_dir

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestCheckCollision_IdenticalBytes is the happy path: two readers yielding
// exactly the same bytes return nil. This is the case that fires on the
// EEXIST branch of blob_mover.Close() when a same-digest writer truly
// wrote the same content.
func TestCheckCollision_IdenticalBytes(t *testing.T) {
	payload := []byte("identical payload for collision happy path")

	if err := checkCollision(
		bytes.NewReader(payload),
		bytes.NewReader(payload),
	); err != nil {
		t.Fatalf("checkCollision on identical readers: %v", err)
	}
}

// TestCheckCollision_EmptyStreams covers the degenerate case where both
// readers are empty. Reading both yields EOF immediately; the function must
// return nil (empty == empty).
func TestCheckCollision_EmptyStreams(t *testing.T) {
	if err := checkCollision(
		bytes.NewReader(nil),
		bytes.NewReader(nil),
	); err != nil {
		t.Fatalf("checkCollision on two empty readers: %v", err)
	}
}

// TestCheckCollision_DifferentBytes_SameLength is the true-hash-collision
// simulation: same length, same digest-would-be (from the caller's point of
// view), but the byte streams differ. Must return ErrCollisionContentMismatch.
func TestCheckCollision_DifferentBytes_SameLength(t *testing.T) {
	a := bytes.Repeat([]byte{0xAA}, 128)
	b := bytes.Repeat([]byte{0xBB}, 128)

	err := checkCollision(bytes.NewReader(a), bytes.NewReader(b))
	if !errors.Is(err, ErrCollisionContentMismatch) {
		t.Fatalf("checkCollision on different bytes: got %v, want ErrCollisionContentMismatch", err)
	}
}

// TestCheckCollision_SrcShorter covers the asymmetric length case where the
// source EOFs before the destination. The destination still has bytes to
// read — that is a mismatch, not a match. Must return
// ErrCollisionContentMismatch.
func TestCheckCollision_SrcShorter(t *testing.T) {
	a := []byte("short")
	b := []byte("short plus trailing bytes that keep going")

	err := checkCollision(bytes.NewReader(a), bytes.NewReader(b))
	if !errors.Is(err, ErrCollisionContentMismatch) {
		t.Fatalf("checkCollision on src-shorter: got %v, want ErrCollisionContentMismatch", err)
	}
}

// TestCheckCollision_DstShorter is the mirror of _SrcShorter: destination
// EOFs first.
func TestCheckCollision_DstShorter(t *testing.T) {
	a := []byte("source plus trailing bytes that keep going")
	b := []byte("source")

	err := checkCollision(bytes.NewReader(a), bytes.NewReader(b))
	if !errors.Is(err, ErrCollisionContentMismatch) {
		t.Fatalf("checkCollision on dst-shorter: got %v, want ErrCollisionContentMismatch", err)
	}
}

// TestCheckCollision_LargeIdentical exercises the streaming loop past the
// first buffer chunk. If the implementation returns nil after the first
// chunk comparison without reading to EOF, this would spuriously pass on
// two identical prefixes followed by differing tails — so we pair it with
// TestCheckCollision_LargeMismatchMidStream below.
func TestCheckCollision_LargeIdentical(t *testing.T) {
	payload := bytes.Repeat([]byte("abcdefgh"), 10_000) // 80KB, ~20 chunks

	if err := checkCollision(
		bytes.NewReader(payload),
		bytes.NewReader(payload),
	); err != nil {
		t.Fatalf("checkCollision on large identical: %v", err)
	}
}

// TestCheckCollision_LargeMismatchMidStream: identical prefix across many
// chunks, differing byte mid-way through, identical suffix. Guards against
// an implementation that short-circuits after the first chunk or uses a
// fixed sample rather than a full stream comparison.
func TestCheckCollision_LargeMismatchMidStream(t *testing.T) {
	payload := bytes.Repeat([]byte("abcdefgh"), 10_000)

	corrupted := append([]byte{}, payload...)
	corrupted[len(corrupted)/2] ^= 0x01

	err := checkCollision(
		bytes.NewReader(payload),
		bytes.NewReader(corrupted),
	)
	if !errors.Is(err, ErrCollisionContentMismatch) {
		t.Fatalf("checkCollision on large mid-stream mismatch: got %v, want ErrCollisionContentMismatch", err)
	}
}

// TestCheckCollision_ChunkBoundaryMismatch covers a mismatch on the exact
// last byte of the first buffer chunk — a known source of off-by-one bugs
// in streaming byte-compare loops. The chunk size is an implementation
// detail, so we force multiple buffer sizes to cross.
func TestCheckCollision_ChunkBoundaryMismatch(t *testing.T) {
	for _, size := range []int{4095, 4096, 4097, 8192} {
		t.Run("size-"+strconv.Itoa(size), func(t *testing.T) {
			a := bytes.Repeat([]byte{'x'}, size)
			b := append([]byte{}, a...)
			b[size-1] = 'y'

			err := checkCollision(bytes.NewReader(a), bytes.NewReader(b))
			if !errors.Is(err, ErrCollisionContentMismatch) {
				t.Fatalf("size=%d: got %v, want ErrCollisionContentMismatch", size, err)
			}
		})
	}
}

// TestCheckCollision_SourceReaderError ensures an upstream Read error on
// the source side propagates (wrapped), rather than being silently treated
// as EOF / mismatch.
func TestCheckCollision_SourceReaderError(t *testing.T) {
	good := bytes.NewReader([]byte("doesn't matter"))
	broken := &alwaysErrReader{err: errors.New("disk exploded")}

	err := checkCollision(broken, good)
	if err == nil {
		t.Fatalf("checkCollision on broken source: got nil, want error")
	}
	if errors.Is(err, ErrCollisionContentMismatch) {
		t.Fatalf("checkCollision on broken source: returned collision error, want I/O error")
	}
}

// TestCheckCollision_DestReaderError: mirror of above.
func TestCheckCollision_DestReaderError(t *testing.T) {
	good := bytes.NewReader([]byte("doesn't matter"))
	broken := &alwaysErrReader{err: errors.New("network drop")}

	err := checkCollision(good, broken)
	if err == nil {
		t.Fatalf("checkCollision on broken dest: got nil, want error")
	}
	if errors.Is(err, ErrCollisionContentMismatch) {
		t.Fatalf("checkCollision on broken dest: returned collision error, want I/O error")
	}
}

// alwaysErrReader is a helper that returns the configured error on every
// Read call. Used to test I/O-error propagation.
type alwaysErrReader struct{ err error }

func (r *alwaysErrReader) Read(p []byte) (int, error) { return 0, r.err }

var _ io.Reader = (*alwaysErrReader)(nil)

// TestVerifyExistingBlobMatches_Identical is the happy path for the
// callsite helper used from blob_mover.Close's EEXIST branch: two files
// with identical bytes produce a nil error under the default env_dir
// config (no compression, no encryption).
func TestVerifyExistingBlobMatches_Identical(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("identical content for EEXIST verify helper")

	tempPath := filepath.Join(dir, "temp")
	blobPath := filepath.Join(dir, "blob")

	if err := os.WriteFile(tempPath, payload, 0o644); err != nil {
		t.Fatalf("WriteFile(temp): %v", err)
	}
	if err := os.WriteFile(blobPath, payload, 0o644); err != nil {
		t.Fatalf("WriteFile(blob): %v", err)
	}

	if err := verifyExistingBlobMatches(DefaultConfig, tempPath, blobPath); err != nil {
		t.Fatalf("verifyExistingBlobMatches on identical files: %v", err)
	}
}

// TestVerifyExistingBlobMatches_DetectsCollision is the reason this
// entire feature exists (issue #31): given two files whose digests would
// match (simulated via the caller's claim — this function doesn't
// recompute hashes) but whose bytes differ, verifyExistingBlobMatches
// must return an error wrapping ErrCollisionContentMismatch.
func TestVerifyExistingBlobMatches_DetectsCollision(t *testing.T) {
	dir := t.TempDir()

	tempPath := filepath.Join(dir, "temp")
	blobPath := filepath.Join(dir, "blob")

	if err := os.WriteFile(tempPath, []byte("payload A"), 0o644); err != nil {
		t.Fatalf("WriteFile(temp): %v", err)
	}
	if err := os.WriteFile(blobPath, []byte("payload B"), 0o644); err != nil {
		t.Fatalf("WriteFile(blob): %v", err)
	}

	err := verifyExistingBlobMatches(DefaultConfig, tempPath, blobPath)
	if !errors.Is(err, ErrCollisionContentMismatch) {
		t.Fatalf("verifyExistingBlobMatches on divergent files: got %v, want wrapped ErrCollisionContentMismatch", err)
	}
}
