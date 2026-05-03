// Package none is the identity (passthrough) blob-encoding plugin.
// Reference: `madder-codec-none-v1@none`.
package none

import (
	"github.com/amarbel-llc/madder/go/internal/bravo/plugins"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ohio"
)

const Reference = "madder-codec-none-v1@none"

// wrapper is the singleton instance returned by the none plugin's
// factory. Comparison against this value is the byte-identity check
// used by IsIdentity; consumers should call IsIdentity rather than
// reaching for the singleton directly.
var wrapper = ohio.NopeIOWrapper{}

func init() {
	plugins.MustRegister(Reference, factory{})
}

type factory struct{}

func (factory) New() interfaces.IOWrapper { return wrapper }

// IsIdentity reports whether w is the none plugin's identity wrapper —
// the byte-identity check used by env_dir.HasIdentityWrappers.
func IsIdentity(w interfaces.IOWrapper) bool {
	return w == wrapper
}
