package blob_stores

import (
	"io"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_io"
	"code.linenisgreat.com/piggy/go/pkgs/agent"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

// IsErrBlobUnreadable reports whether a VerifyBlob failure says nothing
// about the blob's bytes: the key agent could not be reached or would not
// answer, or the store itself was unavailable. Such a blob may be
// perfectly intact, so callers must not report it as corrupt
// (madder#298). Every other VerifyBlob failure (digest mismatch, a blob
// that will not decrypt or decompress with a working agent) does
// implicate the stored bytes.
func IsErrBlobUnreadable(err error) bool {
	return agent.IsErrAgent(err) || blob_io.IsBlobStoreUnavailable(err)
}

// TODO offer options like just checking the existence of the blob, getting its
// size, or full verification
func VerifyBlob(
	ctx errors.Context,
	blobStore domain_interfaces.BlobStore,
	expected domain_interfaces.MarklId,
	progressWriter io.Writer,
) (err error) {
	// TODO check if `blobStore` implements a `VerifyBlob` method and call that
	// instead (for expensive blob stores that may implement their own remote
	// verification, such as ssh, sftp, or something else)

	var readCloser domain_interfaces.BlobReader

	if readCloser, err = blobStore.MakeBlobReader(expected); err != nil {
		err = errors.Wrap(err)
		return err
	}

	if _, err = io.Copy(progressWriter, readCloser); err != nil {
		err = errors.Wrap(err)
		return err
	}

	if err = markl.AssertEqual(
		expected,
		readCloser.GetMarklId(),
	); err != nil {
		err = errors.Wrap(err)
		return err
	}

	if err = readCloser.Close(); err != nil {
		err = errors.Wrap(err)
		return err
	}

	return err
}
