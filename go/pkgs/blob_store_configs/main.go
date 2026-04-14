package blob_store_configs

import (
	internal "github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
)

// Config interfaces
type (
	Config                      = internal.Config
	ConfigUpgradeable           = internal.ConfigUpgradeable
	ConfigMutable               = internal.ConfigMutable
	ConfigHashType              = internal.ConfigHashType
	ConfigLocalMutable          = internal.ConfigLocalMutable
	ConfigLocalHashBucketed     = internal.ConfigLocalHashBucketed
	ConfigInventoryArchive      = internal.ConfigInventoryArchive
	DeltaConfigImmutable        = internal.DeltaConfigImmutable
	SignatureConfigImmutable    = internal.SignatureConfigImmutable
	SelectorConfigImmutable     = internal.SelectorConfigImmutable
	ConfigInventoryArchiveDelta = internal.ConfigInventoryArchiveDelta
	ConfigPointer               = internal.ConfigPointer
	ConfigSFTPRemotePath        = internal.ConfigSFTPRemotePath
	ConfigSFTPUri               = internal.ConfigSFTPUri
	ConfigSFTPConfigExplicit    = internal.ConfigSFTPConfigExplicit
)

// Error types
type ErrUnsupportedHashType = internal.ErrUnsupportedHashType

// Value types
type (
	HashType        = internal.HashType
	EncryptionKeys  = internal.EncryptionKeys
	SignatureConfig = internal.SignatureConfig
	SelectorConfig  = internal.SelectorConfig
	DeltaConfig     = internal.DeltaConfig
)

// TOML config types
type (
	TomlLocalHashBucketedV1 = internal.TomlLocalHashBucketedV1
	TomlLocalHashBucketedV2 = internal.TomlLocalHashBucketedV2
	TomlV3                  = internal.TomlV3
	TomlSFTPV0              = internal.TomlSFTPV0
	TomlSFTPViaSSHConfigV0  = internal.TomlSFTPViaSSHConfigV0
	TomlPointerV0           = internal.TomlPointerV0
	TomlUriV0               = internal.TomlUriV0
	TomlInventoryArchiveV0  = internal.TomlInventoryArchiveV0
	TomlInventoryArchiveV1  = internal.TomlInventoryArchiveV1
	TomlInventoryArchiveV2  = internal.TomlInventoryArchiveV2
)

// Typed config wrappers
type (
	TypedConfig        = internal.TypedConfig
	TypedMutableConfig = internal.TypedMutableConfig
	DefaultType        = internal.DefaultType
	ConfigNamed        = internal.ConfigNamed
)

// Constants
const (
	HashTypeSha256     = internal.HashTypeSha256
	HashTypeBlake2b256 = internal.HashTypeBlake2b256
	HashTypeDefault    = internal.HashTypeDefault
	DefaultHashTypeId  = internal.DefaultHashTypeId
)

// Variables
var (
	DefaultHashType    = internal.DefaultHashType
	DefaultHashBuckets = internal.DefaultHashBuckets
	ConfigKeyValues    = internal.ConfigKeyValues
	ConfigKeyNames     = internal.ConfigKeyNames
	Coder              = internal.Coder
	Default            = internal.Default
)

// Decode functions
var (
	DecodeTomlLocalHashBucketedV1 = internal.DecodeTomlLocalHashBucketedV1
	DecodeTomlLocalHashBucketedV2 = internal.DecodeTomlLocalHashBucketedV2
	DecodeTomlV3                  = internal.DecodeTomlV3
	DecodeTomlSFTPV0              = internal.DecodeTomlSFTPV0
	DecodeTomlSFTPViaSSHConfigV0  = internal.DecodeTomlSFTPViaSSHConfigV0
	DecodeTomlPointerV0           = internal.DecodeTomlPointerV0
	DecodeTomlUriV0               = internal.DecodeTomlUriV0
	DecodeTomlInventoryArchiveV0  = internal.DecodeTomlInventoryArchiveV0
	DecodeTomlInventoryArchiveV1  = internal.DecodeTomlInventoryArchiveV1
	DecodeTomlInventoryArchiveV2  = internal.DecodeTomlInventoryArchiveV2
)
