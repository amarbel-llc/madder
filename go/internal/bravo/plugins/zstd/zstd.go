// Package zstd is the zstd blob-encoding plugin.
// Reference: `madder-codec-zstd-v1@zstd`.
package zstd

import (
	"io"

	"github.com/DataDog/zstd"
	"github.com/amarbel-llc/madder/go/internal/bravo/plugins"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

const Reference = "madder-codec-zstd-v1@zstd"

func init() {
	plugins.MustRegister(Reference, factory{})
}

type factory struct{}

func (factory) New() interfaces.IOWrapper { return wrapper{} }

type wrapper struct{}

func (wrapper) WrapWriter(w io.Writer) (io.WriteCloser, error) {
	return zstd.NewWriter(w), nil
}

func (wrapper) WrapReader(r io.Reader) (io.ReadCloser, error) {
	return zstd.NewReader(r), nil
}
