package hyphence

import (
	"io"

	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// MetadataStreamer is the metadata consumer for `hyphence meta`. It
// copies metadata bytes verbatim from the piped reader supplied by
// hyphence.Reader's metadata pipeline to W. No per-line validation
// happens here — `hyphence meta` is intentionally lenient; users who
// want strict checks run `hyphence validate` first.
type MetadataStreamer struct {
	W io.Writer
}

func (m *MetadataStreamer) ReadFrom(r io.Reader) (int64, error) {
	n, err := io.Copy(m.W, r)
	if err != nil {
		return n, errors.Wrap(err)
	}
	return n, nil
}
