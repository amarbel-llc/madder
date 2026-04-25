//go:build unix

package mmap_blob

import (
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// Verify recomputes the digest of the mmap'd bytes through the recorded
// MarklId's hash format and compares against the recorded MarklId.
// Returns ErrDigestMismatch on a content/digest mismatch. Returns nil if
// marklId is nil or null (caller passed an unidentified blob).
//
// Verify is opt-in — never called from Bytes() or Close(). Callers who
// want digest checking on every read must invoke Verify() themselves.
func (m *mmapBlob) Verify() error {
	if markl.IsNull(m.marklId) {
		return nil
	}

	// MarklId.GetMarklFormat() returns the lightweight MarklFormat
	// interface; GetHash() lives on the FormatHash side, resolved by
	// format-id.
	formatHash, err := markl.GetFormatHashOrError(
		m.marklId.GetMarklFormat().GetMarklFormatId(),
	)
	if err != nil {
		return errors.Wrap(err)
	}

	hash, repool := formatHash.GetHash() //repool:owned
	defer repool()

	if _, err := hash.Write(m.bytes); err != nil {
		return errors.Wrap(err)
	}

	got, repoolGot := hash.GetMarklId() //repool:owned
	defer repoolGot()

	if !markl.Equals(got, m.marklId) {
		return ErrDigestMismatch
	}

	return nil
}
