package blob_stores

import (
	"io"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_io"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

type multiMode int

const (
	modeUnset multiMode = iota
	modeMirror
	modeWriteThrough
)

type Multi struct {
	ctx         interfaces.ActiveContext
	mode        multiMode
	childStores []BlobStoreInitialized // mirror mode
	writeStore  BlobStoreInitialized   // write-through mode (filled in Task 9)
	readStores  []BlobStoreInitialized // write-through mode (filled in Task 9)
	readFill    bool                   // write-through mode (filled in Task 9)
}

var _ domain_interfaces.BlobAccess = Multi{}

func (parentStore Multi) HasBlob(id domain_interfaces.MarklId) bool {
	for _, childStore := range parentStore.childStores {
		if childStore.HasBlob(id) {
			return true
		}
	}

	return false
}

func (parentStore Multi) MakeBlobReader(
	id domain_interfaces.MarklId,
) (domain_interfaces.BlobReader, error) {
	for _, childStore := range parentStore.childStores {
		if childStore.HasBlob(id) {
			return childStore.MakeBlobReader(id)
		}
	}

	clonedId, _ := markl.Clone(id) //repool:owned

	return nil, blob_io.ErrBlobMissing{
		BlobId: clonedId,
	}
}

func (parentStore Multi) MakeBlobWriter(
	marklHashType domain_interfaces.FormatHash,
) (domain_interfaces.BlobWriter, error) {
	writers := make([]io.Writer, len(parentStore.childStores))

	multiWriter := multiStoreBlobWriter{
		blobWriters: make(
			[]domain_interfaces.BlobWriter,
			len(parentStore.childStores),
		),
	}

	for i, childStore := range parentStore.childStores {
		var err error

		if multiWriter.blobWriters[i], err = childStore.MakeBlobWriter(
			marklHashType,
		); err != nil {
			err = errors.Wrap(err)
			return nil, err
		}

		writers[i] = multiWriter.blobWriters[i]
	}

	multiWriter.Writer = io.MultiWriter(writers...)

	return multiWriter, nil
}

type multiStoreBlobWriter struct {
	io.Writer
	blobWriters []domain_interfaces.BlobWriter
}

var _ domain_interfaces.BlobWriter = multiStoreBlobWriter{}

func (parentWriter multiStoreBlobWriter) ReadFrom(
	reader io.Reader,
) (n int64, err error) {
	if n, err = io.Copy(parentWriter, reader); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	return n, err
}

func (parentWriter multiStoreBlobWriter) Close() error {
	for _, childWriter := range parentWriter.blobWriters {
		if err := childWriter.Close(); err != nil {
			err = errors.Wrap(err)
			return err
		}
	}

	return nil
}

func (parentWriter multiStoreBlobWriter) GetMarklId() (first domain_interfaces.MarklId) {
	for _, childWriter := range parentWriter.blobWriters {
		next := childWriter.GetMarklId()

		if first == nil {
			first = next
		} else if err := markl.AssertEqual(first, next); err != nil {
			panic(err)
		}
	}

	return first
}
