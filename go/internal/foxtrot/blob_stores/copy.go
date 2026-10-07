package blob_stores

import (
	"io"
	"time"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

// ForeignDigestAliasSupporter is implemented by a store whose ability to
// record foreign-digest aliases depends on how it is configured, so that
// satisfying BlobForeignDigestAdder is not by itself the answer. A store
// that does not implement it supports aliases exactly when it is an adder.
type ForeignDigestAliasSupporter interface {
	SupportsForeignDigestAliases() bool
}

// SupportsForeignDigestAliases reports whether store can record a
// foreign-digest alias.
func SupportsForeignDigestAliases(store domain_interfaces.BlobStore) bool {
	if _, ok := store.(domain_interfaces.BlobForeignDigestAdder); !ok {
		return false
	}

	if supporter, ok := store.(ForeignDigestAliasSupporter); ok {
		return supporter.SupportsForeignDigestAliases()
	}

	return true
}

func CopyBlobIfNecessary(
	ctx errors.Context,
	dst domain_interfaces.BlobStore,
	src domain_interfaces.BlobStore,
	expectedDigest domain_interfaces.MarklId,
	extraWriter io.Writer,
	hashType domain_interfaces.FormatHash,
) (copyResult CopyResult) {
	return copyBlobIfNecessary(
		ctx, dst, src, expectedDigest, extraWriter, hashType, true,
	)
}

// copyBlobIfNecessary is CopyBlobIfNecessary with the alias handling
// switchable: copying the blob an alias points at must not itself go
// looking for aliases, or a link that points at itself, or two that point
// at each other, would recurse without end.
func copyBlobIfNecessary(
	ctx errors.Context,
	dst domain_interfaces.BlobStore,
	src domain_interfaces.BlobStore,
	expectedDigest domain_interfaces.MarklId,
	extraWriter io.Writer,
	hashType domain_interfaces.FormatHash,
	carryAliases bool,
) (copyResult CopyResult) {
	copyResult.BlobId = expectedDigest

	// first check if we have the blob already, intentionally before checking if
	// `src` is non-nil, as this method is also used to emit a list of missing
	// blobs in the remote transfer protocol
	if dst.HasBlob(expectedDigest) {
		copyResult.bytesWritten = -1
		copyResult.state = CopyResultStateExistsLocally

		// A resumed cross-hash sync lands here via the alias an earlier run
		// left behind, before any writer exists — so the destination digest
		// has to be read back from that alias rather than computed.
		if resolver, ok := dst.(domain_interfaces.BlobForeignDigestResolver); ok {
			native, isAlias, err := resolver.ResolveForeignBlobDigest(expectedDigest)
			if err != nil {
				copyResult.SetError(err)
				return copyResult
			}

			if isAlias {
				copyResult.DestBlobId = native
			}
		}

		return copyResult
	}

	if src == nil {
		copyResult.bytesWritten = -1
		copyResult.state = CopyResultStateNilRemoteBlobStore
		return copyResult
	}

	if err := markl.AssertIdIsNotNull(expectedDigest); err != nil {
		copyResult.bytesWritten = -1
		copyResult.state = CopyResultStateExistsLocallyAndRemotely
		return copyResult
	}

	errors.PanicIfError(markl.AssertIdIsNotNull(expectedDigest))

	// With no rehash requested, an id the source holds only as an alias of
	// another blob is carried across as an alias, not as a second copy of
	// the bytes under another name.
	if hashType == nil && carryAliases {
		if aliasResult, handled := copyForeignDigestAlias(
			ctx,
			dst,
			src,
			expectedDigest,
			extraWriter,
		); handled {
			return aliasResult
		}
	}

	var readCloser domain_interfaces.BlobReader

	{
		var err error

		if readCloser, err = src.MakeBlobReader(expectedDigest); err != nil {
			copyResult.SetError(err)
			return copyResult
		}
	}

	defer errors.ContextMustClose(ctx, readCloser)

	var writeCloser domain_interfaces.BlobWriter

	if hashType == nil {
		var err error

		if hashType, err = markl.GetFormatHashOrError(
			expectedDigest.GetMarklFormat().GetMarklFormatId(),
		); err != nil {
			copyResult.SetError(err)
			return copyResult
		}
	}

	{
		var err error

		if writeCloser, err = dst.MakeBlobWriter(hashType); err != nil {
			copyResult.SetError(err)
			return copyResult
		}
	}

	defer errors.ContextMustClose(ctx, writeCloser)

	outputWriter := io.Writer(writeCloser)

	if extraWriter != nil {
		outputWriter = io.MultiWriter(outputWriter, extraWriter)
	}

	{
		var err error

		copyResult.bytesWritten, err = io.Copy(
			outputWriter,
			readCloser,
		)
		if err != nil {
			copyResult.setErrorAfterCopy(copyResult.bytesWritten, err)
			return copyResult
		} else {
			copyResult.state = CopyResultStateSuccess
		}
	}

	readerDigest := readCloser.GetMarklId()

	writerDigest := writeCloser.GetMarklId()

	if !markl.Equals(readerDigest, expectedDigest) {
		copyResult.setErrorAfterCopy(
			copyResult.bytesWritten,
			errors.Errorf(
				"lookup digest was %s while read digest was %s",
				expectedDigest,
				readerDigest,
			),
		)

		return copyResult
	}

	crossHash := expectedDigest.GetMarklFormat().GetMarklFormatId() !=
		writerDigest.GetMarklFormat().GetMarklFormatId()

	if crossHash {
		if adder, ok := dst.(domain_interfaces.BlobForeignDigestAdder); ok &&
			SupportsForeignDigestAliases(dst) {
			if err := adder.AddForeignBlobDigestForNativeDigest(
				expectedDigest,
				writerDigest,
			); err != nil {
				copyResult.setErrorAfterCopy(copyResult.bytesWritten, err)
				return copyResult
			}
		}

		copyResult.DestBlobId = writerDigest
	} else {
		if err := markl.AssertEqual(expectedDigest, writerDigest); err != nil {
			copyResult.setErrorAfterCopy(
				copyResult.bytesWritten,
				err,
			)

			return copyResult
		}
	}

	return copyResult
}

// copyForeignDigestAlias handles a blob id that src holds as a foreign-digest
// alias of a blob stored under another hash type (the links a cross-hash
// sync leaves behind). Copying such an id the ordinary way stores the same
// bytes a second time under the alias's hash type; a store with tens of
// thousands of aliases doubles in size on the destination.
//
// Instead: make sure the native blob is in dst, check that the alias really
// is that blob's digest, and record the alias in dst. handled is false
// when the id is not an alias or either store cannot do this, and the
// caller copies as usual.
//
// The check reads the blob from src and digests it under the alias's hash
// type. It costs a read of the source but no transfer, and it is what the
// ordinary copy verified too: without it a wrong link in src would be
// reproduced in dst unnoticed.
func copyForeignDigestAlias(
	ctx errors.Context,
	dst domain_interfaces.BlobStore,
	src domain_interfaces.BlobStore,
	foreign domain_interfaces.MarklId,
	extraWriter io.Writer,
) (copyResult CopyResult, handled bool) {
	resolver, ok := src.(domain_interfaces.BlobForeignDigestResolver)
	if !ok {
		return copyResult, false
	}

	if !SupportsForeignDigestAliases(dst) {
		return copyResult, false
	}

	adder := dst.(domain_interfaces.BlobForeignDigestAdder)

	copyResult.BlobId = foreign

	native, isAlias, err := resolver.ResolveForeignBlobDigest(foreign)
	if err != nil {
		copyResult.SetError(err)
		return copyResult, true
	}

	if !isAlias {
		return copyResult, false
	}

	var bytesWritten int64

	if !dst.HasBlob(native) {
		nativeResult := copyBlobIfNecessary(
			ctx, dst, src, native, extraWriter, nil, false,
		)

		if nativeResult.state != CopyResultStateSuccess && !nativeResult.Exists() {
			nativeResult.BlobId = foreign
			return nativeResult, true
		}

		if nativeResult.bytesWritten > 0 {
			bytesWritten = nativeResult.bytesWritten
		}
	}

	if err = verifyBlobDigest(src, foreign); err != nil {
		copyResult.SetError(err)
		return copyResult, true
	}

	if err = adder.AddForeignBlobDigestForNativeDigest(foreign, native); err != nil {
		copyResult.setErrorAfterCopy(bytesWritten, err)
		return copyResult, true
	}

	copyResult.DestBlobId = native
	copyResult.bytesWritten = bytesWritten
	copyResult.state = CopyResultStateSuccess

	return copyResult, true
}

// verifyBlobDigest reads id from store to the end and checks the bytes
// digest to id.
func verifyBlobDigest(
	store domain_interfaces.BlobStore,
	id domain_interfaces.MarklId,
) (err error) {
	reader, err := store.MakeBlobReader(id)
	if err != nil {
		return err
	}

	if _, err = io.Copy(io.Discard, reader); err != nil {
		return errors.Join(err, reader.Close())
	}

	// Compared before Close: the reader may own the digest it hands out.
	if actual := reader.GetMarklId(); !markl.Equals(actual, id) {
		err = errors.Errorf(
			"alias %s does not match the blob it points at, which digests to %s",
			id,
			actual,
		)

		return errors.Join(err, reader.Close())
	}

	return reader.Close()
}

func CopyReaderToWriter(
	ctx errors.Context,
	dst domain_interfaces.BlobWriter,
	src io.Reader,
	expected domain_interfaces.MarklId,
	extraWriter io.Writer,
	heartbeats func(time time.Time),
	pulse time.Duration,
) (copyResult CopyResult) {
	var writer io.Writer = dst

	if extraWriter != nil {
		writer = io.MultiWriter(dst, extraWriter)
	}

	if err := errors.RunChildContextWithPrintTicker(
		ctx,
		func(ctx errors.Context) {
			var err error

			if copyResult.bytesWritten, err = io.Copy(writer, src); err != nil {
				ctx.Cancel(err)
			}
		},
		heartbeats,
		pulse,
	); err != nil {
		copyResult.setErrorAfterCopy(copyResult.bytesWritten, err)
		return copyResult
	} else {
		copyResult.state = CopyResultStateSuccess
	}

	if err := dst.Close(); err != nil {
		copyResult.setErrorAfterCopy(copyResult.bytesWritten, err)
		return copyResult
	}

	copyResult.BlobId = dst.GetMarklId()

	if expected != nil {
		if err := markl.AssertEqual(expected, copyResult.BlobId); err != nil {
			copyResult.setErrorAfterCopy(copyResult.bytesWritten, err)
			return copyResult
		}
	}

	return copyResult
}
