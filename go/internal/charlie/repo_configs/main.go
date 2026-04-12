package repo_configs

import (
	"github.com/amarbel-llc/madder/go/internal/bravo/ids"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/collections_slice"
)

type (
	DefaultsGetter interface {
		GetDefaults() Defaults
	}

	Defaults interface {
		GetDefaultType() ids.TypeStruct
		GetDefaultTags() collections_slice.Slice[ids.TagStruct]
	}
)
