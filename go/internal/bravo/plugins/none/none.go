// Package none is the identity (passthrough) blob-encoding plugin.
// Reference: `madder-codec-none-v1@none`.
package none

import (
	"github.com/amarbel-llc/madder/go/internal/bravo/plugins"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ohio"
)

// Reference is the canonical plugin reference for the none codec.
const Reference = "madder-codec-none-v1@none"

// Wrapper is the singleton instance returned by the none plugin's
// factory. Callers checking for byte-identity behavior can compare
// against this value (used by env_dir.HasIdentityWrappers in a
// later slice).
var Wrapper = ohio.NopeIOWrapper{}

func init() {
	plugins.MustRegister(Reference, factory{})
}

type factory struct{}

func (factory) New() interfaces.IOWrapper { return Wrapper }
