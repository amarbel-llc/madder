package env_dir

import (
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ohio"
	"github.com/amarbel-llc/purse-first/libs/dewey/delta/compression_type"
)

// TODO move into own package

func MakeConfig(
	hashFormat domain_interfaces.FormatHash,
	funcJoin func(string, ...string) string,
	compression interfaces.IOWrapper,
	encryption domain_interfaces.MarklId,
) Config {
	var ioWrapper interfaces.IOWrapper = defaultEncryptionIOWrapper

	if encryption != nil {
		var err error
		ioWrapper, err = encryption.GetIOWrapper()
		errors.PanicIfError(err)
	}

	return Config{
		hashFormat:  hashFormat,
		funcJoin:    funcJoin,
		compression: compression,
		encryption:  ioWrapper,
	}
}

var (
	defaultCompressionTypeValue = compression_type.CompressionTypeNone
	defaultEncryptionIOWrapper  = ohio.NopeIOWrapper{}
	DefaultConfig               = Config{
		hashFormat:  blob_store_configs.DefaultHashType,
		compression: &defaultCompressionTypeValue,
		encryption:  &defaultEncryptionIOWrapper,
	}
)

type Config struct {
	hashFormat domain_interfaces.FormatHash
	// TODO replace with path generator interface
	funcJoin    func(string, ...string) string
	compression interfaces.IOWrapper
	encryption  interfaces.IOWrapper
}

func (config Config) GetBlobCompression() interfaces.IOWrapper {
	if config.compression == nil {
		return &defaultCompressionTypeValue
	} else {
		return config.compression
	}
}

func (config Config) GetBlobEncryption() interfaces.IOWrapper {
	if config.encryption == nil {
		return defaultEncryptionIOWrapper
	} else {
		return config.encryption
	}
}

// HasIdentityWrappers returns true when both blob wrappers are
// byte-identity (none compression, no-op encryption). When true, the
// on-disk file bytes equal the logical blob bytes — a precondition
// for direct file mmap.
func (config Config) HasIdentityWrappers() bool {
	compType, ok := config.GetBlobCompression().(*compression_type.CompressionType)
	if !ok {
		return false
	}
	if *compType != compression_type.CompressionTypeNone &&
		*compType != compression_type.CompressionTypeEmpty {
		return false
	}
	// NopeIOWrapper has value-receiver methods, so both ohio.NopeIOWrapper
	// and *ohio.NopeIOWrapper satisfy interfaces.IOWrapper. DefaultConfig
	// stores the pointer form, but Config values built via MakeConfig
	// with an empty EncryptionKeys land on the value form (because
	// MakeConfig overwrites the default pointer when its encryption arg
	// is a typed-nil MarklId whose GetIOWrapper returns nil, and
	// GetBlobEncryption then falls back to the value-form
	// defaultEncryptionIOWrapper). Accept both forms here — the
	// byte-identity property is the same.
	switch config.GetBlobEncryption().(type) {
	case *ohio.NopeIOWrapper, ohio.NopeIOWrapper:
		return true
	default:
		return false
	}
}
