package domain_interfaces

//go:generate dagnabit export

import (
	"io"
	"os"

	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
)

type (
	BlobIOWrapper interface {
		GetBlobEncryption() MarklId
		GetBlobCompression() interfaces.IOWrapper
	}

	BlobIOWrapperGetter interface {
		GetBlobIOWrapper() BlobIOWrapper
	}

	ReadAtSeeker interface {
		io.ReaderAt
		io.Seeker
	}

	BlobReader interface {
		io.WriterTo
		io.ReadCloser

		ReadAtSeeker
		MarklIdGetter
	}

	BlobWriter interface {
		io.ReaderFrom
		io.WriteCloser
		MarklIdGetter
	}

	BlobReaderFactory interface {
		MakeBlobReader(MarklId) (BlobReader, error)
	}

	// MmapSource is implemented by a BlobReader whose bytes equal a
	// contiguous file region. Only the local hash-bucketed store's
	// reader implements this in v1; non-local stores (SFTP, in-memory)
	// and stores wrapping the file with non-identity encoding return
	// ok=false from MmapSource().
	//
	// On ok=true, ownership of file transfers to the caller; the caller
	// is responsible for closing it (typically the MmapBlob does this).
	// MmapSource is a one-shot transfer: subsequent calls return
	// ok=false because the source no longer holds the file.
	MmapSource interface {
		MmapSource() (file *os.File, offset int64, length int64, ok bool, err error)
	}

	BlobWriterFactory interface {
		MakeBlobWriter(FormatHash) (BlobWriter, error)
	}

	BlobAccess interface {
		HasBlob(MarklId) bool
		BlobReaderFactory
		BlobWriterFactory
	}

	NamedBlobAccess interface {
		MakeNamedBlobReader(string) (BlobReader, error)
		MakeNamedBlobWriter(string) (BlobWriter, error)
	}

	// BlobStoreConfig is the layer-0 marker for blob-store configuration
	// objects. Per ADR 0005, the config returned here describes blob-store
	// properties (hash type, buckets, compression, encryption); transport
	// configuration for remote stores lives elsewhere
	// (BlobStoreInitialized.Config).
	BlobStoreConfig interface {
		GetBlobStoreType() string
	}

	BlobStore interface {
		BlobAccess
		BlobIOWrapperGetter

		GetBlobStoreDescription() string
		GetDefaultHashType() FormatHash
		GetBlobStoreConfig() BlobStoreConfig
		AllBlobs() interfaces.SeqError[MarklId]
	}

	// Blobs represent persisted files, like blobs in Git. Blobs are used by
	// Zettels, types, tags, config, and inventory lists.
	BlobPool[BLOB any] interface {
		GetBlob(MarklId) (BLOB, interfaces.FuncRepool, error)
	}

	Format[BLOB any, BLOB_PTR interfaces.Ptr[BLOB]] interface {
		SavedBlobFormatter
		interfaces.CoderReadWriter[BLOB_PTR]
	}

	TypedStore[
		BLOB any,
		BLOB_PTR interfaces.Ptr[BLOB],
	] interface {
		// TODO remove and replace with two-step process
		SaveBlobText(BLOB_PTR) (MarklId, int64, error)
		Format[BLOB, BLOB_PTR]
		// TODO remove
		BlobPool[BLOB_PTR]
	}

	SavedBlobFormatter interface {
		FormatSavedBlob(io.Writer, MarklId) (int64, error)
	}

	BlobForeignDigestAdder interface {
		AddForeignBlobDigestForNativeDigest(foreign, native MarklId) error
	}
)
