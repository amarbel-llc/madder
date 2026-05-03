package plugins

import (
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// legacyCompressionTable maps on-disk compression-type strings to
// plugin references. The empty string is the v1/v2 default and means
// "no compression."
var legacyCompressionTable = map[string]string{
	"":     "madder-codec-none-v1@none",
	"none": "madder-codec-none-v1@none",
	"gzip": "madder-codec-gzip-v1@gzip",
	"zlib": "madder-codec-zlib-v1@zlib",
	"zstd": "madder-codec-zstd-v1@zstd",
}

// LegacyCompressionRef returns the plugin reference equivalent to a
// legacy on-disk compression-type string. Used when loading V1/V2/V3
// store configs to bridge their string-typed field into the plugin
// abstraction.
func LegacyCompressionRef(legacy string) (string, error) {
	ref, ok := legacyCompressionTable[legacy]
	if !ok {
		return "", errors.Errorf("%w: %q", ErrUnknownLegacyCompression, legacy)
	}
	return ref, nil
}
