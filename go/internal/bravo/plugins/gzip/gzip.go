// Package gzip is the gzip blob-encoding plugin.
// Reference: `madder-codec-gzip-v1@gzip`.
package gzip

import (
	stdgzip "compress/gzip"
	"io"

	"code.linenisgreat.com/madder/go/internal/bravo/plugins"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
)

const Reference = "madder-codec-gzip-v1@gzip"

func init() {
	plugins.MustRegister(Reference, factory{})
}

type factory struct{}

func (factory) New() interfaces.IOWrapper { return wrapper{} }

type wrapper struct{}

func (wrapper) WrapWriter(w io.Writer) (io.WriteCloser, error) {
	return stdgzip.NewWriter(w), nil
}

func (wrapper) WrapReader(r io.Reader) (io.ReadCloser, error) {
	return stdgzip.NewReader(r)
}
