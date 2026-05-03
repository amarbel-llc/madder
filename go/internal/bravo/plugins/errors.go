package plugins

import "errors"

var (
	// ErrAlreadyRegistered is returned by Registry.Register when the
	// reference is already registered.
	ErrAlreadyRegistered = errors.New("plugin already registered")

	// ErrUnknownPlugin is returned by Registry.Resolve when the
	// reference is not registered.
	ErrUnknownPlugin = errors.New("unknown plugin reference")

	// ErrUnknownLegacyCompression is returned by LegacyCompressionRef
	// when the input string is not one of the known on-disk values
	// (`""`, `"none"`, `"gzip"`, `"zlib"`, `"zstd"`).
	ErrUnknownLegacyCompression = errors.New("unknown legacy compression-type")
)
