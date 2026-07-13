package blob_stores

import (
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/piggy/go/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
)

// resolveWriteHashFormat picks the hash format a remote store's
// MakeBlobWriter should digest with, given the caller's requested type
// (nil means "use the store default"). Shared by the SFTP, S3, and
// WebDAV stores, which are structurally identical here.
//
// A multi-hash store honors any requested type: the blob is digested
// with it and lands under the matching <format-id>/ subtree (the layout
// follows from the digest via the store's keyForMarklId /
// remotePathForMerkleId / urlForMerkleId, exactly what the AllBlobs
// per-format walk expects). A single-hash store can only ever hold its
// configured type, so a mismatching request is rejected loudly rather
// than silently substituted — the pre-fix behavior that wrote a blob
// under the wrong hash with no signal at all (#261, #262).
func resolveWriteHashFormat(
	requested domain_interfaces.FormatHash,
	defaultHashType markl.FormatHash,
	multiHash bool,
	storeId string,
) (hashFormat markl.FormatHash, err error) {
	hashFormat = defaultHashType

	if requested != nil {
		if hashFormat, err = markl.GetFormatHashOrError(
			requested.GetMarklFormatId(),
		); err != nil {
			err = errors.Wrap(err)
			return hashFormat, err
		}
	}

	if !multiHash &&
		hashFormat.GetMarklFormatId() != defaultHashType.GetMarklFormatId() {
		err = errors.Errorf(
			"blob store %q is single-hash (%s); "+
				"cannot write requested hash type %s",
			storeId,
			defaultHashType.GetMarklFormatId(),
			hashFormat.GetMarklFormatId(),
		)
		return hashFormat, err
	}

	return hashFormat, err
}

// readHashFormatForDigest resolves the hash format a remote store's
// MakeBlobReader should verify with: the blob id's own markl format,
// not the store default. A multi-hash store holds blobs of several
// types, so digesting a non-default blob under defaultHashType would
// reconstruct the wrong id (#261, #262). Mirrors
// localHashBucketed.blobReaderFrom.
func readHashFormatForDigest(
	digest domain_interfaces.MarklId,
) (hashFormat markl.FormatHash, err error) {
	marklType := digest.GetMarklFormat()
	if marklType == nil || marklType.GetMarklFormatId() == "" {
		err = errors.Errorf("blob id has no markl hash format: %s", digest)
		return hashFormat, err
	}

	if hashFormat, err = markl.GetFormatHashOrError(
		marklType.GetMarklFormatId(),
	); err != nil {
		err = errors.Wrap(err)
		return hashFormat, err
	}

	return hashFormat, err
}
