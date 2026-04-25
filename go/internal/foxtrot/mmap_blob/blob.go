// Package mmap_blob promotes a BlobReader to a zero-copy []byte view
// backed by file mmap, when the underlying storage permits.
package mmap_blob

import (
	"errors"
	"io"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
)

var (
	// ErrMmapUnsupported is returned when MakeMmapBlobFromBlobReader
	// cannot promote the reader — wrong store, non-file backing, or
	// wrappers preclude byte-identity.
	ErrMmapUnsupported = errors.New("mmap_blob: blob is not mmap-able")

	// ErrDigestMismatch is returned only from MmapBlob.Verify() when
	// the recomputed digest does not match the recorded MarklId.
	ErrDigestMismatch = errors.New("mmap_blob: digest mismatch")
)

// MmapBlob is a zero-copy view of a blob's bytes. Bytes() returns a
// slice valid until Close(). Close is idempotent.
type MmapBlob interface {
	Bytes() []byte
	GetMarklId() domain_interfaces.MarklId
	Verify() error
	io.Closer
}
