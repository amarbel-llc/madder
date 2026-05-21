// Package zlib is the zlib blob-encoding plugin.
// Reference: `madder-codec-zlib-v1@zlib`.
package zlib

import (
	stdzlib "compress/zlib"
	"io"

	"github.com/amarbel-llc/madder/go/internal/bravo/plugins"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

const Reference = "madder-codec-zlib-v1@zlib"

func init() {
	plugins.MustRegister(Reference, factory{})
}

type factory struct{}

func (factory) New() interfaces.IOWrapper { return wrapper{} }

type wrapper struct{}

func (wrapper) WrapWriter(w io.Writer) (io.WriteCloser, error) {
	return stdzlib.NewWriter(w), nil
}

func (wrapper) WrapReader(r io.Reader) (io.ReadCloser, error) {
	return stdzlib.NewReader(r)
}
