package blob_io

//go:generate dagnabit export

import (
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/bravo/plugins/none"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/ohio"
)

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
	defaultCompressionWrapper  interfaces.IOWrapper = ohio.NopeIOWrapper{}
	defaultEncryptionIOWrapper                      = ohio.NopeIOWrapper{}
	DefaultConfig                                   = Config{
		hashFormat:  blob_store_configs.DefaultHashType,
		compression: defaultCompressionWrapper,
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

// GetHashFormat exposes the hash format Config was built with.
// Used by sftp_probe.VerifySample to plug a digester into a
// manually-composed reader chain that bypasses NewReader's
// identity-fallback (which would mask wrong-compression failures
// as hash mismatches and corrupt the probe's stage
// classification).
func (config Config) GetHashFormat() domain_interfaces.FormatHash {
	if config.hashFormat == nil {
		return blob_store_configs.DefaultHashType
	}
	return config.hashFormat
}

func (config Config) GetBlobCompression() interfaces.IOWrapper {
	if config.compression == nil {
		return defaultCompressionWrapper
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
	if !none.IsIdentity(config.GetBlobCompression()) {
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
