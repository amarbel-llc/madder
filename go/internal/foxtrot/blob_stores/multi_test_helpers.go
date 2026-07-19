//go:build test

package blob_stores

import (
	"io"
	"sync"
	"sync/atomic"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
)

// Compile-time guards: these helpers must satisfy the real blob
// reader/writer contracts so later tests can pass them where the
// production code expects a BlobReader / BlobWriter.
var (
	_ domain_interfaces.BlobReader = (*controllableBlobReader)(nil)
	_ domain_interfaces.BlobWriter = (*spyBlobWriter)(nil)
)

// controllableBlobReader yields bytes on demand. Tests call Feed(...)
// to make the next Read return those bytes; Close marks EOF.
type controllableBlobReader struct {
	mu      sync.Mutex
	queued  [][]byte
	closed  atomic.Bool
	id      domain_interfaces.MarklId
	onClose func()
}

func newControllableBlobReader(id domain_interfaces.MarklId) *controllableBlobReader {
	return &controllableBlobReader{id: id}
}

func (r *controllableBlobReader) Feed(b []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queued = append(r.queued, append([]byte(nil), b...))
}

func (r *controllableBlobReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.queued) == 0 {
		if r.closed.Load() {
			return 0, io.EOF
		}
		return 0, nil
	}
	chunk := r.queued[0]
	n := copy(p, chunk)
	if n < len(chunk) {
		r.queued[0] = chunk[n:]
	} else {
		r.queued = r.queued[1:]
	}
	return n, nil
}

func (r *controllableBlobReader) Close() error {
	r.closed.Store(true)
	r.mu.Lock()
	f := r.onClose
	r.mu.Unlock()
	if f != nil {
		f()
	}
	return nil
}

func (r *controllableBlobReader) GetMarklId() domain_interfaces.MarklId { return r.id }

// Stub the remaining BlobReader methods with minimal no-ops — tests
// that need them will be added later.
func (r *controllableBlobReader) ReadAt(p []byte, off int64) (int, error) {
	return 0, io.EOF
}

func (r *controllableBlobReader) Seek(offset int64, whence int) (int64, error) {
	return 0, nil
}

func (r *controllableBlobReader) WriteTo(w io.Writer) (int64, error) {
	var total int64
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			nw, _ := w.Write(buf[:n])
			total += int64(nw)
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

// spyBlobWriter records Write/Close calls. failAfterBytes > 0 causes
// Write to return an error once the cumulative byte count reaches it.
type spyBlobWriter struct {
	mu             sync.Mutex
	received       []byte
	closed         atomic.Bool
	closeCount     atomic.Int32
	closeErr       error
	failAfterBytes int
	bytesWritten   int
	computedId     domain_interfaces.MarklId
}

func (w *spyBlobWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failAfterBytes > 0 && w.bytesWritten+len(p) > w.failAfterBytes {
		return 0, io.ErrShortWrite
	}
	w.received = append(w.received, p...)
	w.bytesWritten += len(p)
	return len(p), nil
}

func (w *spyBlobWriter) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(struct{ io.Writer }{w}, r)
}

func (w *spyBlobWriter) Close() error {
	w.closed.Store(true)
	w.closeCount.Add(1)
	return w.closeErr
}

func (w *spyBlobWriter) GetMarklId() domain_interfaces.MarklId { return w.computedId }
