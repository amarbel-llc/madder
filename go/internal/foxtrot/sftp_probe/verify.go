package sftp_probe

import (
	"bytes"
	"encoding/hex"
	"io"

	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_io"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// SampleResult is the verdict for one (sample, candidate) pair.
// Ok is true when the candidate's reader pipeline accepted the
// sample bytes and the recovered content reproduced the
// expectedDigestHex passed to VerifySample. Stage classifies the
// failure when Ok is false. Err carries the underlying decode
// error when one was returned by the pipeline.
type SampleResult struct {
	Ok    bool
	Stage Stage
	Err   error
}

// VerifySample feeds blobReader through candidate.IOConfig's reader
// pipeline (decrypt → decompress → digest), compares the produced
// hex digest to expectedDigestHex, and reports the verdict.
//
// The function is pure: no SFTP, no filesystem, no global state.
// The caller is responsible for buffering the reader if it needs
// to be replayed against another candidate.
//
// Panics from any decode layer are recovered and converted to a
// failure result with Stage=StageDecrypt — the deepest pipeline
// layer that could plausibly panic on adversarial input. The
// intent is robustness, not classification accuracy on malformed
// blobs.
func VerifySample(
	blobReader io.Reader,
	expectedDigestHex string,
	candidate Candidate,
) (result SampleResult) {
	defer func() {
		if r := recover(); r != nil {
			result = SampleResult{
				Ok:    false,
				Stage: StageDecrypt,
				Err:   errors.Errorf("panic during VerifySample: %v", r),
			}
		}
	}()

	// blob_io.NewReader requires an io.ReadSeeker (the encryption
	// fallback in newFileReaderFromReadSeeker uses Seek). Buffer
	// the reader once so the pipeline can rewind if it needs to.
	buf, err := io.ReadAll(blobReader)
	if err != nil {
		return SampleResult{Stage: StageDecrypt, Err: errors.Wrap(err)}
	}

	reader, err := blob_io.NewReader(candidate.IOConfig, bytes.NewReader(buf))
	if err != nil {
		// Errors from NewReader come from the encryption WrapReader
		// stage (decrypter init) or the compression WrapReader.
		// Classification refines later as more cases land.
		return SampleResult{Stage: StageDecrypt, Err: errors.Wrap(err)}
	}
	defer reader.Close()

	if _, err := io.Copy(io.Discard, reader); err != nil {
		return SampleResult{Stage: StageDecompress, Err: errors.Wrap(err)}
	}

	gotHex := hex.EncodeToString(reader.GetMarklId().GetBytes())
	if gotHex != expectedDigestHex {
		return SampleResult{Stage: StageHashMismatch}
	}

	return SampleResult{Ok: true, Stage: StageOK}
}
