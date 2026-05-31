// Package plugins is madder's blob-encoding plugin registry. Each
// plugin is a stream-to-stream transform satisfying
// interfaces.IOWrapper. Per FDR 0004, plugins are referenced by
// `<type-tag>@<builtin-plugin-id>`; v0 builtin-plugin-id is the leaf
// name of the Go package housing the plugin's factory.
package plugins

//go:generate dagnabit export

import (
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

// Factory constructs a plugin instance. v0 plugins are non-parametric;
// future parametric plugins (e.g. zstd-with-dict in FDR 0010) accept
// configuration via a separate side-data interface.
type Factory interface {
	New() interfaces.IOWrapper
}
