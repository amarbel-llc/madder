// Package builtins blank-imports every built-in plugin so their init()
// functions register them in plugins.Default. Import this package once
// from cmd/* main.go's import block to populate the registry.
package builtins

import (
	_ "github.com/amarbel-llc/madder/go/internal/bravo/plugins/gzip"
	_ "github.com/amarbel-llc/madder/go/internal/bravo/plugins/none"
	_ "github.com/amarbel-llc/madder/go/internal/bravo/plugins/zlib"
)
