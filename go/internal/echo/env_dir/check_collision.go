package env_dir

import (
	"bytes"
	"errors"
	"io"

	perrors "github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// ErrCollisionContentMismatch signals that two readers claimed to represent
// the same blob (same digest, same target path) but their byte streams
// diverged. For a collision-resistant hash the only way this fires is a
// true hash collision; for a weak-hash mode (hypothetical, see #31) it
// fires on any mid-air content divergence.
//
// This is exploratory infrastructure — not yet wired into the publish
// path. See docs/decisions/0003-blob-store-hardlink-writes.md for the
// EEXIST branch that will eventually call into this. Caller rationale
// tracked in https://github.com/amarbel-llc/madder/issues/31.
var ErrCollisionContentMismatch = errors.New(
	"blob content mismatch at same digest (hash collision)",
)

// checkCollisionBufSize matches git's check_collision() chunk size. 4096
// is enough to amortise syscall overhead against streaming disk reads
// without holding a large stack buffer.
const checkCollisionBufSize = 4096

// checkCollision returns nil iff a and b yield identical byte streams up
// to EOF. On any byte-level divergence — differing bytes at the same
// offset, or differing total lengths — returns ErrCollisionContentMismatch.
// Any Read error other than EOF from either reader is wrapped and
// propagated.
//
// The callers decide what "logical bytes" means. A raw os.File gives the
// on-disk encoded bytes (compressed, encrypted); a domain_interfaces
// BlobReader gives decoded plaintext. For the blob-mover EEXIST branch,
// logical-byte comparison (BlobReader) is the correct semantics because
// encryption with per-write nonces would produce different on-disk bytes
// for identical content.
func checkCollision(a, b io.Reader) error {
	bufA := make([]byte, checkCollisionBufSize)
	bufB := make([]byte, checkCollisionBufSize)

	for {
		nA, errA := io.ReadFull(a, bufA)
		if errA != nil && errA != io.EOF && errA != io.ErrUnexpectedEOF {
			return perrors.Wrapf(errA, "reading source stream")
		}

		nB, errB := io.ReadFull(b, bufB)
		if errB != nil && errB != io.EOF && errB != io.ErrUnexpectedEOF {
			return perrors.Wrapf(errB, "reading dest stream")
		}

		if nA != nB || !bytes.Equal(bufA[:nA], bufB[:nB]) {
			return ErrCollisionContentMismatch
		}

		// Either reader hitting EOF (or short-read on the last chunk)
		// means the stream is exhausted on that side; the equal-length
		// check above proves the other side is exhausted too.
		if errA == io.EOF || errA == io.ErrUnexpectedEOF {
			return nil
		}
	}
}

// verifyExistingBlobMatches is the callsite helper for the blob-mover's
// EEXIST branch. It opens both paths as BlobReaders under the given
// env_dir Config (so compression/encryption are unwrapped symmetrically)
// and delegates byte-compare to checkCollision.
//
// The caller (blob_mover.Close) invokes this only when the store's
// VerifyOnCollision flag is set; see ADR 0003 and issue #31.
func verifyExistingBlobMatches(
	config Config,
	tempPath, blobPath string,
) (err error) {
	// NewFileReaderOrErrNotExist owns the underlying file descriptor;
	// closing the returned BlobReader closes the file. Don't layer a
	// second close on the raw file.
	tempReader, err := NewFileReaderOrErrNotExist(config, tempPath)
	if err != nil {
		err = perrors.Wrapf(err, "opening temp %q for collision check", tempPath)
		return err
	}
	defer perrors.DeferredCloser(&err, tempReader)

	blobReader, err := NewFileReaderOrErrNotExist(config, blobPath)
	if err != nil {
		err = perrors.Wrapf(err, "opening existing blob %q for collision check", blobPath)
		return err
	}
	defer perrors.DeferredCloser(&err, blobReader)

	if err = checkCollision(tempReader, blobReader); err != nil {
		err = perrors.Wrapf(err, "temp=%q existing=%q", tempPath, blobPath)
		return err
	}

	return err
}
