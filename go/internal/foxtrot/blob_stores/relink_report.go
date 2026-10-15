package blob_stores

import (
	"io"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

// ForeignDigestAliasReporter is implemented by a store that can say whether
// a blob it listed is a foreign-digest alias (a link to the blob's native
// object) rather than a full copy of the bytes under that digest. The answer
// is only meaningful for ids the store has listed, so call AllBlobs first.
type ForeignDigestAliasReporter interface {
	IsForeignDigestAlias(id domain_interfaces.MarklId) bool
}

// StoredSizeReporter is implemented by a store that can say how many bytes
// an object occupies where it is stored, which differs from the length of
// its bytes read back when the store compresses or encrypts.
type StoredSizeReporter interface {
	StoredSize(id domain_interfaces.MarklId) (int64, error)
}

// RelinkState says what a read-only relink check found for one object.
type RelinkState string

const (
	// RelinkRelinkable: the object is a full copy under a foreign digest,
	// its bytes do hash to that digest, and the native object with the same
	// bytes exists. Replacing the copy by an alias would reclaim its bytes.
	RelinkRelinkable RelinkState = "relinkable"

	// RelinkPlaintextMismatch: the bytes read back do not hash to the
	// object's own id. Nothing may be relinked on the strength of it.
	RelinkPlaintextMismatch RelinkState = "plaintext_mismatch"

	// RelinkNativeMissing: the bytes are intact but the native object they
	// resolve to does not exist, so the copy is the only one and is not a
	// duplicate of anything.
	RelinkNativeMissing RelinkState = "native_missing"

	// RelinkUnreadable: the object could not be read, so there is no
	// native id to resolve.
	RelinkUnreadable RelinkState = "unreadable"
)

// RelinkRecord is the result of checking one object. Native is nil when the
// object could not be read to completion.
type RelinkRecord struct {
	Id               domain_interfaces.MarklId
	Native           domain_interfaces.MarklId
	NativeExists     bool
	PlaintextMatches bool
	Bytes            int64 // length of the bytes read back
	StoredBytes      int64 // bytes the object occupies in the store; -1 if unknown
	ReclaimableBytes int64 // StoredBytes when known, else Bytes, if relinkable
	State            RelinkState
	Err              error
}

// Mismatch reports whether the record is anything other than relinkable.
func (record RelinkRecord) Mismatch() bool {
	return record.State != RelinkRelinkable
}

// CheckRelinkCandidate reads id from store once, hashing the bytes under both
// id's own hash type (through the store's reader) and nativeHash, then asks
// whether the native object exists. It writes nothing.
func CheckRelinkCandidate(
	store domain_interfaces.BlobStore,
	id domain_interfaces.MarklId,
	nativeHash markl.FormatHash,
) (record RelinkRecord) {
	record.Id = id
	record.StoredBytes = -1

	reader, err := store.MakeBlobReader(id)
	if err != nil {
		record.State = RelinkUnreadable
		record.Err = err

		return record
	}

	nativeDigest, _ := nativeHash.Get() //repool:owned

	counter := &countingWriter{}

	if _, err = io.Copy(io.MultiWriter(nativeDigest, counter), reader); err != nil {
		_ = reader.Close()
		record.State = RelinkUnreadable
		record.Err = err

		return record
	}

	record.Bytes = counter.n
	record.PlaintextMatches = markl.Equals(id, reader.GetMarklId())

	if err = reader.Close(); err != nil {
		record.State = RelinkUnreadable
		record.Err = err

		return record
	}

	nativeId, _ := nativeDigest.GetMarklId() //repool:owned
	record.Native, _ = markl.Clone(nativeId) //repool:owned
	record.NativeExists = store.HasBlob(record.Native)

	switch {
	case !record.PlaintextMatches:
		record.State = RelinkPlaintextMismatch
		record.Err = errors.Errorf(
			"bytes hash to %s under %s, not the object's id",
			reader.GetMarklId(),
			id.GetMarklFormat().GetMarklFormatId(),
		)

	case !record.NativeExists:
		record.State = RelinkNativeMissing

	default:
		record.State = RelinkRelinkable
		record.ReclaimableBytes = record.Bytes

		if sizer, ok := store.(StoredSizeReporter); ok {
			// The size on the remote is what a relink frees. If it cannot
			// be had, the plaintext length stands in and StoredBytes stays
			// -1 so the report does not claim to know it.
			if stored, sizeErr := sizer.StoredSize(id); sizeErr == nil {
				record.StoredBytes = stored
				record.ReclaimableBytes = stored
			}
		}
	}

	return record
}

type countingWriter struct{ n int64 }

func (writer *countingWriter) Write(p []byte) (int, error) {
	writer.n += int64(len(p))
	return len(p), nil
}
