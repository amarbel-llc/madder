package blob_stores

import (
	"io"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_io"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/ohio"
)

// discard is a virtual blob store whose MakeBlobWriter feeds raw bytes
// through the standard blob_io.NewWriter chain to a final io.Discard
// sink. The hash digester still sees every byte (writer.go:49 wires it
// upstream of compression+encryption via io.MultiWriter), so blob-ids
// produced here equal those a real store would produce for the same
// content. cutting-garden's diff command uses this to recompute
// content blob-ids while walking a tree without persisting anything.
type discard struct {
	hashFormat domain_interfaces.FormatHash
}

var _ domain_interfaces.BlobStore = discard{}

func (discard) GetBlobStoreDescription() string {
	return "(discard)"
}

func (s discard) GetDefaultHashType() domain_interfaces.FormatHash {
	return s.hashFormat
}

func (discard) GetBlobStoreConfig() domain_interfaces.BlobStoreConfig {
	return nil
}

func (discard) GetBlobIOWrapper() domain_interfaces.BlobIOWrapper {
	return discardIOWrapper{}
}

func (discard) HasBlob(domain_interfaces.MarklId) bool {
	return false
}

func (discard) MakeBlobReader(
	domain_interfaces.MarklId,
) (domain_interfaces.BlobReader, error) {
	return nil, errors.ErrorWithStackf(
		"discard blob store does not serve reads",
	)
}

func (discard) AllBlobs() interfaces.SeqError[domain_interfaces.MarklId] {
	return func(yield func(domain_interfaces.MarklId, error) bool) {}
}

func (s discard) MakeBlobWriter(
	hashFormat domain_interfaces.FormatHash,
) (domain_interfaces.BlobWriter, error) {
	if hashFormat == nil {
		hashFormat = s.hashFormat
	}

	writer, err := blob_io.NewWriter(
		blob_io.MakeConfig(hashFormat, nil, nil, nil),
		io.Discard,
	)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return writer, nil
}

type discardIOWrapper struct{}

func (discardIOWrapper) GetBlobEncryption() domain_interfaces.MarklId {
	return nil
}

func (discardIOWrapper) GetBlobCompression() interfaces.IOWrapper {
	return ohio.NopeIOWrapper{}
}

// NewDiscardBlobStore returns a BlobStoreInitialized whose MakeBlobWriter
// produces hash-only writers at the given hashFormat. Reads and HasBlob
// probes return as if the store is empty. The embedded ConfigNamed is
// zero-valued; callers that need an id-bearing handle (e.g. for
// command_components lookups) should not use this.
func NewDiscardBlobStore(
	hashFormat domain_interfaces.FormatHash,
) BlobStoreInitialized {
	return BlobStoreInitialized{
		ConfigNamed: blob_store_configs.ConfigNamed{},
		BlobStore:   discard{hashFormat: hashFormat},
	}
}
