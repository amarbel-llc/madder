package tree_capture_receipt

import (
	"bytes"
	"io"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// Read fetches the blob named by id from blobStore and parses it
// according to its hyphence type-tag. Returns a typed value (currently
// always *V1) plus the type-tag string for callers that want to log or
// branch on it.
//
// Mirrors the typed_blob_store/Tag dispatcher pattern from dodder:
// the metadata reader captures the type-tag, then the body is
// re-decoded by the matching ReadV<n>. Returns Blob (any) so future
// versions land as new cases without breaking callers.
func Read(
	blobStore domain_interfaces.BlobReaderFactory,
	id domain_interfaces.MarklId,
) (Blob, string, error) {
	reader, err := blobStore.MakeBlobReader(id)
	if err != nil {
		return nil, "", errors.Wrap(err)
	}

	defer errors.DeferredCloser(&err, reader)

	return ReadFrom(reader)
}

// ReadFrom parses a hyphence-wrapped receipt from r, dispatching on
// the type-tag captured during the hyphence metadata pass.
//
// Strategy: hyphence.Reader does the boundary parsing exactly once;
// the metadata reader captures the type-tag; the body bytes are
// buffered and then handed to the version-specific body parser. We
// never re-implement hyphence's framing.
func ReadFrom(r io.Reader) (Blob, string, error) {
	mr := &v1MetadataReader{}
	body := &bufferReader{}

	hr := hyphence.Reader{
		RequireMetadata: true,
		Metadata:        mr,
		Blob:            body,
	}

	if _, err := hr.ReadFrom(r); err != nil {
		return nil, mr.typeTag, errors.Wrap(err)
	}

	switch mr.typeTag {
	case TypeTagV1:
		entries, _, err := decodeV1Body(bytes.NewReader(body.buf.Bytes()))
		if err != nil {
			return nil, mr.typeTag, errors.Wrap(err)
		}
		return &V1{Hint: mr.hint, Entries: entries}, mr.typeTag, nil

	default:
		return nil, mr.typeTag, errors.ErrorWithStackf(
			"tree_capture_receipt: unsupported type-tag %q", mr.typeTag,
		)
	}
}

// bufferReader is an io.ReaderFrom that copies all bytes into an
// in-memory buffer for later version-specific decoding. Used by the
// dispatcher in ReadFrom to defer body parsing until the type-tag is
// known.
type bufferReader struct {
	buf bytes.Buffer
}

func (br *bufferReader) ReadFrom(r io.Reader) (int64, error) {
	return br.buf.ReadFrom(r)
}
