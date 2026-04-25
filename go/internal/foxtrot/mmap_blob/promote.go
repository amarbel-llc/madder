package mmap_blob

import (
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
)

// MakeMmapBlobFromBlobReader inspects reader. If it implements
// MmapSource and reports ok=true, returns an MmapBlob mapping the
// reported file region. Otherwise returns ErrMmapUnsupported.
//
// On success, ownership of the underlying file transfers to the
// returned MmapBlob. Caller MUST NOT also Close reader for the file
// portion — but reader.Close() is still safe to call (the
// implementation makes the file-close a no-op after handoff).
//
// On failure, reader is unchanged and remains the caller's to Close.
func MakeMmapBlobFromBlobReader(
	reader domain_interfaces.BlobReader,
) (MmapBlob, error) {
	src, ok := reader.(domain_interfaces.MmapSource)
	if !ok {
		return nil, ErrMmapUnsupported
	}
	file, offset, length, mmapOk, err := src.MmapSource()
	if err != nil {
		return nil, err
	}
	if !mmapOk {
		return nil, ErrMmapUnsupported
	}
	return mmapFile(file, offset, length, reader.GetMarklId())
}
