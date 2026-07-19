package sftp_probe

import (
	"bytes"
	"encoding/hex"
	"io"
	"strings"

	markl_io_alfa "code.linenisgreat.com/madder/go/internal/alfa/markl_io"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
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

// VerifySample feeds blobReader through candidate.IOConfig's
// decrypt → decompress → digest pipeline and compares the produced
// hex digest to expectedDigestHex.
//
// The function is pure: no SFTP, no filesystem, no global state.
// The caller is responsible for buffering the reader if it needs
// to be replayed against another candidate.
//
// We compose the decrypter and decompressor directly rather than
// going through blob_io.NewReader. NewReader has a defensive
// identity-fallback when compression WrapReader errors; that
// fallback masks wrong-compression failures as hash_mismatch and
// would corrupt our failure-stage classification. The probe needs
// strict pipeline behavior: a wrong compression candidate must
// surface as StageDecompress, not as StageHashMismatch.
//
// Panics from any decode layer are recovered into a failure
// result. Stage is StageDecrypt by default (the deepest layer
// that could plausibly panic on adversarial input).
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

	buf, err := io.ReadAll(blobReader)
	if err != nil {
		return SampleResult{Stage: StageDecrypt, Err: errors.Wrap(err)}
	}

	decrypter, err := candidate.IOConfig.GetBlobEncryption().WrapReader(
		bytes.NewReader(buf),
	)
	if err != nil {
		return SampleResult{Stage: StageDecrypt, Err: errors.Wrap(err)}
	}

	expander, err := candidate.IOConfig.GetBlobCompression().WrapReader(decrypter)
	if err != nil {
		return SampleResult{Stage: StageDecompress, Err: errors.Wrap(err)}
	}

	hashFmt := candidate.IOConfig.GetHashFormat()
	hash, repool := hashFmt.GetHash()
	defer repool()

	digester := markl_io_alfa.MakeWriter(hash, nil)

	if _, err := io.Copy(digester, expander); err != nil {
		// Distinguish "encryption layer wrong, ciphertext fed into
		// decompressor" from "compression layer wrong on
		// cleartext." age error chains mention "age" or "no
		// identity matched"; everything else we treat as a
		// decompression-stage failure.
		if isAgeError(err) {
			return SampleResult{Stage: StageDecrypt, Err: errors.Wrap(err)}
		}
		return SampleResult{Stage: StageDecompress, Err: errors.Wrap(err)}
	}

	id, repoolId := hash.GetMarklId()
	defer repoolId()

	gotHex := hex.EncodeToString(id.GetBytes())
	if gotHex != expectedDigestHex {
		return SampleResult{Stage: StageHashMismatch}
	}

	return SampleResult{Ok: true, Stage: StageOK}
}

// isAgeError walks the error chain looking for the age package's
// signature error strings. The age library does not export a
// stable error type for "no identity matched" — the markl
// wrapper rewraps these and the easiest stable identifier is
// substring matching on the message. Brittle but bounded: only
// affects classification, not correctness.
func isAgeError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "age") ||
		strings.Contains(msg, "no identity matched") ||
		strings.Contains(msg, "no identities")
}
