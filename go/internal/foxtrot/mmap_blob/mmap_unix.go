//go:build unix

package mmap_blob

import (
	"os"
	"sync"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"golang.org/x/sys/unix"
)

type mmapBlob struct {
	bytes   []byte
	marklId domain_interfaces.MarklId
	file    *os.File

	closeOnce sync.Once
	closeErr  error
}

var _ MmapBlob = (*mmapBlob)(nil)

// mmapFile wraps unix.Mmap for a contiguous read-only mapping over
// the file region [offset, offset+length). On success the returned
// MmapBlob takes ownership of file and closes it via Close(). On
// error the caller retains ownership of file and must close it.
func mmapFile(
	file *os.File,
	offset, length int64,
	marklId domain_interfaces.MarklId,
) (MmapBlob, error) {
	if length == 0 {
		// unix.Mmap rejects length=0 with EINVAL; treat empty blobs as
		// a valid no-op mapping. Use []byte{} (not nil) so callers
		// observe a non-nil, zero-length slice in both code paths.
		return &mmapBlob{bytes: []byte{}, file: file, marklId: marklId}, nil
	}
	data, err := unix.Mmap(
		int(file.Fd()),
		offset,
		int(length),
		unix.PROT_READ,
		unix.MAP_SHARED,
	)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	return &mmapBlob{
		bytes:   data,
		marklId: marklId,
		file:    file,
	}, nil
}

func (m *mmapBlob) Bytes() []byte                         { return m.bytes }
func (m *mmapBlob) GetMarklId() domain_interfaces.MarklId { return m.marklId }

// Close unmaps the bytes and closes the file, idempotently. If both
// fail, the Munmap error wins (file-close error is recorded only when
// closeErr is still nil).
func (m *mmapBlob) Close() error {
	m.closeOnce.Do(func() {
		if len(m.bytes) > 0 {
			if err := unix.Munmap(m.bytes); err != nil {
				m.closeErr = errors.Wrap(err)
			}
		}
		m.bytes = nil
		if m.file != nil {
			if err := m.file.Close(); err != nil && m.closeErr == nil {
				m.closeErr = errors.Wrap(err)
			}
			m.file = nil
		}
	})
	return m.closeErr
}

// Verify is a stub here. Real implementation lands in task 8 of the
// mmap blob access plan.
func (m *mmapBlob) Verify() error { return nil }
