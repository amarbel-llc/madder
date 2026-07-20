package blob_stores

import (
	"io"
	"sync/atomic"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

// teeBlobReader wraps a source BlobReader so each Read also copies the
// bytes into a sink BlobWriter that targets the write store. The first
// Close — whether from the caller or from ctx.After — commits the sink;
// done serializes those paths so the sink is closed exactly once. A
// sink Write failure flips sinkDead, which silences further tee writes
// without disrupting the read; the caller still gets the full source
// bytes.
type teeBlobReader struct {
	src        domain_interfaces.BlobReader
	sink       domain_interfaces.BlobWriter
	writeStore domain_interfaces.BlobStore
	expected   domain_interfaces.MarklId
	sinkDead   atomic.Bool
	done       atomic.Bool
}

var _ domain_interfaces.BlobReader = (*teeBlobReader)(nil)

func newTeeBlobReader(
	ctx interfaces.ActiveContext,
	src domain_interfaces.BlobReader,
	sink domain_interfaces.BlobWriter,
	expected domain_interfaces.MarklId,
	writeStore domain_interfaces.BlobStore,
) *teeBlobReader {
	t := &teeBlobReader{
		src:        src,
		sink:       sink,
		writeStore: writeStore,
		expected:   expected,
	}
	ctx.After(errors.MakeFuncContextFromFuncErr(func() error {
		return t.flushAndCommit()
	}))
	return t
}

func (t *teeBlobReader) Read(p []byte) (int, error) {
	n, err := t.src.Read(p)
	if n > 0 && !t.sinkDead.Load() {
		if _, werr := t.sink.Write(p[:n]); werr != nil {
			t.sinkDead.Store(true)
		}
	}
	return n, err
}

func (t *teeBlobReader) WriteTo(w io.Writer) (int64, error) {
	var total int64
	buf := make([]byte, 32*1024)
	for {
		n, err := t.Read(buf)
		if n > 0 {
			nw, werr := w.Write(buf[:n])
			total += int64(nw)
			if werr != nil {
				return total, werr
			}
			if nw < n {
				return total, io.ErrShortWrite
			}
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

func (t *teeBlobReader) ReadAt(p []byte, off int64) (int, error) {
	return t.src.ReadAt(p, off)
}

func (t *teeBlobReader) Seek(offset int64, whence int) (int64, error) {
	return t.src.Seek(offset, whence)
}

func (t *teeBlobReader) GetMarklId() domain_interfaces.MarklId {
	return t.src.GetMarklId()
}

func (t *teeBlobReader) Close() error {
	return t.flushAndCommit()
}

// flushAndCommit drains any unread bytes through the tee, closes both
// sides, and reconciles expected vs. the sink's computed MarklId.
// Same-hash mismatches are detect-and-reported; cross-hash mismatches
// register a foreign-digest mapping on the write store when it
// implements BlobForeignDigestAdder (mirrors CopyBlobIfNecessary).
// ActiveContext.After runs after the context is complete, so the
// caller's read goroutine has terminated by the time this fires via
// the After path; the drain cannot race a live Read.
func (t *teeBlobReader) flushAndCommit() error {
	if t.done.Swap(true) {
		return nil
	}
	_, _ = io.Copy(io.Discard, t)
	srcErr := t.src.Close()
	sinkErr := t.sink.Close()
	var digestErr error
	if t.expected != nil && !t.sinkDead.Load() {
		writerDigest := t.sink.GetMarklId()
		if writerDigest != nil {
			if t.expected.GetMarklFormat().GetMarklFormatId() ==
				writerDigest.GetMarklFormat().GetMarklFormatId() {
				if !markl.Equals(t.expected, writerDigest) {
					digestErr = errors.Errorf(
						"tee digest mismatch: expected %s, sink produced %s",
						t.expected,
						writerDigest,
					)
				}
			} else if adder, ok := t.writeStore.(domain_interfaces.BlobForeignDigestAdder); ok {
				_ = adder.AddForeignBlobDigestForNativeDigest(t.expected, writerDigest)
			}
		}
	}
	return errors.Join(srcErr, sinkErr, digestErr)
}
