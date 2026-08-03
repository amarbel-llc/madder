package blob_store_configs

//go:generate dagnabit export

import (
	"fmt"

	"code.linenisgreat.com/hyphence/go/hyphence"
	"code.linenisgreat.com/madder/go/internal/0/ids"
	charlie_bsc "code.linenisgreat.com/madder/go/internal/charlie/blob_store_configs"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
)

// Re-export all types from charlie/blob_store_configs
type (
	Config                      = charlie_bsc.Config
	ConfigUpgradeable           = charlie_bsc.ConfigUpgradeable
	ConfigMutable               = charlie_bsc.ConfigMutable
	ConfigInstanceId            = charlie_bsc.ConfigInstanceId
	ConfigInstanceIdMintable    = charlie_bsc.ConfigInstanceIdMintable
	ConfigHashType              = charlie_bsc.ConfigHashType
	ConfigCompressionType       = charlie_bsc.ConfigCompressionType
	ConfigLocalMutable          = charlie_bsc.ConfigLocalMutable
	ConfigLocalHashBucketed     = charlie_bsc.ConfigLocalHashBucketed
	ConfigInventoryArchive      = charlie_bsc.ConfigInventoryArchive
	DeltaConfigImmutable        = charlie_bsc.DeltaConfigImmutable
	SignatureConfigImmutable    = charlie_bsc.SignatureConfigImmutable
	SelectorConfigImmutable     = charlie_bsc.SelectorConfigImmutable
	ConfigInventoryArchiveDelta = charlie_bsc.ConfigInventoryArchiveDelta
	ConfigPointer               = charlie_bsc.ConfigPointer
	ConfigMulti                 = charlie_bsc.ConfigMulti
	ConfigSFTPRemotePath        = charlie_bsc.ConfigSFTPRemotePath
	ConfigSFTPUri               = charlie_bsc.ConfigSFTPUri
	ConfigSFTPConfigExplicit    = charlie_bsc.ConfigSFTPConfigExplicit
	ConfigWebDAV                = charlie_bsc.ConfigWebDAV
	ConfigS3                    = charlie_bsc.ConfigS3
	ErrUnsupportedHashType      = charlie_bsc.ErrUnsupportedHashType
	HashType                    = charlie_bsc.HashType
	EncryptionKeys              = charlie_bsc.EncryptionKeys
	SignatureConfig             = charlie_bsc.SignatureConfig
	SelectorConfig              = charlie_bsc.SelectorConfig
	DeltaConfig                 = charlie_bsc.DeltaConfig
	TomlLocalHashBucketedV1     = charlie_bsc.TomlLocalHashBucketedV1
	TomlLocalHashBucketedV2     = charlie_bsc.TomlLocalHashBucketedV2
	TomlV3                      = charlie_bsc.TomlV3
	TomlV4                      = charlie_bsc.TomlV4
	TomlSFTPV0                  = charlie_bsc.TomlSFTPV0
	TomlSFTPV1                  = charlie_bsc.TomlSFTPV1
	TomlSFTPViaSSHConfigV0      = charlie_bsc.TomlSFTPViaSSHConfigV0
	TomlSFTPViaSSHConfigV1      = charlie_bsc.TomlSFTPViaSSHConfigV1
	TomlWebDAVV0                = charlie_bsc.TomlWebDAVV0
	TomlWebDAVV1                = charlie_bsc.TomlWebDAVV1
	TomlS3V0                    = charlie_bsc.TomlS3V0
	TomlS3V1                    = charlie_bsc.TomlS3V1
	TomlPointerV0               = charlie_bsc.TomlPointerV0
	TomlPointerV1               = charlie_bsc.TomlPointerV1
	TomlPointerV2               = charlie_bsc.TomlPointerV2
	TomlMultiV0                 = charlie_bsc.TomlMultiV0
	TomlMultiV1                 = charlie_bsc.TomlMultiV1
	TomlUriV0                   = charlie_bsc.TomlUriV0
	TomlInventoryArchiveV0      = charlie_bsc.TomlInventoryArchiveV0
	TomlInventoryArchiveV1      = charlie_bsc.TomlInventoryArchiveV1
	TomlInventoryArchiveV2      = charlie_bsc.TomlInventoryArchiveV2
	TomlInventoryArchiveV3      = charlie_bsc.TomlInventoryArchiveV3
	TypedConfig                 = hyphence.TypedBlob[ids.TypeStruct, *ids.TypeStruct, markl.Id, *markl.Id, Config]
	TypedMutableConfig          = hyphence.TypedBlob[ids.TypeStruct, *ids.TypeStruct, markl.Id, *markl.Id, ConfigMutable]
)

// Re-export constants
const (
	HashTypeSha256     = charlie_bsc.HashTypeSha256
	HashTypeBlake2b256 = charlie_bsc.HashTypeBlake2b256
	HashTypeDefault    = charlie_bsc.HashTypeDefault
	DefaultHashTypeId  = charlie_bsc.DefaultHashTypeId
)

var (
	DefaultHashType    = charlie_bsc.DefaultHashType
	DefaultHashBuckets = charlie_bsc.DefaultHashBuckets
	ConfigKeyValues    = charlie_bsc.ConfigKeyValues
	ConfigKeyNames     = charlie_bsc.ConfigKeyNames
)

var SetMultiEncryptionFlagDefinition = charlie_bsc.SetMultiEncryptionFlagDefinition

// Re-export generated Decode/Encode functions
var (
	DecodeTomlLocalHashBucketedV1 = charlie_bsc.DecodeTomlLocalHashBucketedV1
	DecodeTomlLocalHashBucketedV2 = charlie_bsc.DecodeTomlLocalHashBucketedV2
	DecodeTomlV3                  = charlie_bsc.DecodeTomlV3
	DecodeTomlV4                  = charlie_bsc.DecodeTomlV4
	DecodeTomlSFTPV0              = charlie_bsc.DecodeTomlSFTPV0
	DecodeTomlSFTPV1              = charlie_bsc.DecodeTomlSFTPV1
	DecodeTomlSFTPViaSSHConfigV0  = charlie_bsc.DecodeTomlSFTPViaSSHConfigV0
	DecodeTomlSFTPViaSSHConfigV1  = charlie_bsc.DecodeTomlSFTPViaSSHConfigV1
	DecodeTomlWebDAVV0            = charlie_bsc.DecodeTomlWebDAVV0
	DecodeTomlWebDAVV1            = charlie_bsc.DecodeTomlWebDAVV1
	DecodeTomlS3V0                = charlie_bsc.DecodeTomlS3V0
	DecodeTomlS3V1                = charlie_bsc.DecodeTomlS3V1
	DecodeTomlPointerV0           = charlie_bsc.DecodeTomlPointerV0
	DecodeTomlPointerV1           = charlie_bsc.DecodeTomlPointerV1
	DecodeTomlPointerV2           = charlie_bsc.DecodeTomlPointerV2
	DecodeTomlMultiV0             = charlie_bsc.DecodeTomlMultiV0
	DecodeTomlMultiV1             = charlie_bsc.DecodeTomlMultiV1
	DecodeTomlUriV0               = charlie_bsc.DecodeTomlUriV0
	DecodeTomlInventoryArchiveV0  = charlie_bsc.DecodeTomlInventoryArchiveV0
	DecodeTomlInventoryArchiveV1  = charlie_bsc.DecodeTomlInventoryArchiveV1
	DecodeTomlInventoryArchiveV2  = charlie_bsc.DecodeTomlInventoryArchiveV2
	DecodeTomlInventoryArchiveV3  = charlie_bsc.DecodeTomlInventoryArchiveV3
)

// Interface satisfaction checks
var (
	_ ConfigSFTPRemotePath        = &TomlSFTPV0{}
	_ ConfigSFTPRemotePath        = &TomlSFTPViaSSHConfigV0{}
	_ ConfigMutable               = &TomlSFTPV0{}
	_ ConfigLocalHashBucketed     = TomlLocalHashBucketedV1{}
	_ ConfigUpgradeable           = TomlLocalHashBucketedV1{}
	_ ConfigLocalMutable          = &TomlLocalHashBucketedV1{}
	_ ConfigLocalHashBucketed     = TomlLocalHashBucketedV2{}
	_ ConfigUpgradeable           = TomlLocalHashBucketedV2{}
	_ ConfigLocalMutable          = &TomlLocalHashBucketedV2{}
	_ ConfigLocalHashBucketed     = TomlV3{}
	_ ConfigLocalMutable          = &TomlV3{}
	_ ConfigMutable               = &TomlV3{}
	_ ConfigUpgradeable           = TomlV3{}
	_ ConfigLocalHashBucketed     = TomlV4{}
	_ ConfigLocalMutable          = &TomlV4{}
	_ ConfigMutable               = &TomlV4{}
	_ ConfigInstanceIdMintable    = &TomlV4{}
	_ ConfigPointer               = TomlPointerV0{}
	_ ConfigMutable               = &TomlPointerV0{}
	_ ConfigPointer               = TomlPointerV1{}
	_ ConfigMutable               = &TomlPointerV1{}
	_ ConfigMulti                 = TomlMultiV0{}
	_ ConfigMutable               = &TomlMultiV0{}
	_ ConfigInventoryArchive      = TomlInventoryArchiveV0{}
	_ ConfigUpgradeable           = TomlInventoryArchiveV0{}
	_ ConfigMutable               = &TomlInventoryArchiveV0{}
	_ ConfigInventoryArchiveDelta = TomlInventoryArchiveV1{}
	_ ConfigUpgradeable           = TomlInventoryArchiveV1{}
	_ ConfigMutable               = &TomlInventoryArchiveV1{}
	_ SignatureConfigImmutable    = TomlInventoryArchiveV1{}
	_ SelectorConfigImmutable     = TomlInventoryArchiveV1{}
	_ ConfigInventoryArchiveDelta = TomlInventoryArchiveV2{}
	_ ConfigMutable               = &TomlInventoryArchiveV2{}
	_ SignatureConfigImmutable    = TomlInventoryArchiveV2{}
	_ SelectorConfigImmutable     = TomlInventoryArchiveV2{}
	_ ConfigSFTPRemotePath        = TomlSFTPViaSSHConfigV0{}
	_ ConfigMutable               = &TomlSFTPViaSSHConfigV0{}
	_ ConfigWebDAV                = &TomlWebDAVV0{}
	_ ConfigMutable               = &TomlWebDAVV0{}
	_ ConfigS3                    = &TomlS3V0{}
	_ ConfigMutable               = &TomlS3V0{}

	// FDR-0010 uuid-bearing versions: each new-version config satisfies the
	// same interfaces as its predecessor plus ConfigInstanceIdMintable, and
	// each predecessor gains an Upgrade() (ConfigUpgradeable).
	_ ConfigUpgradeable           = &TomlSFTPV0{}
	_ ConfigSFTPRemotePath        = &TomlSFTPV1{}
	_ ConfigMutable               = &TomlSFTPV1{}
	_ ConfigInstanceIdMintable    = &TomlSFTPV1{}
	_ ConfigUpgradeable           = TomlSFTPViaSSHConfigV0{}
	_ ConfigSFTPRemotePath        = TomlSFTPViaSSHConfigV1{}
	_ ConfigMutable               = &TomlSFTPViaSSHConfigV1{}
	_ ConfigInstanceIdMintable    = &TomlSFTPViaSSHConfigV1{}
	_ ConfigUpgradeable           = &TomlWebDAVV0{}
	_ ConfigWebDAV                = &TomlWebDAVV1{}
	_ ConfigMutable               = &TomlWebDAVV1{}
	_ ConfigInstanceIdMintable    = &TomlWebDAVV1{}
	_ ConfigUpgradeable           = &TomlS3V0{}
	_ ConfigS3                    = &TomlS3V1{}
	_ ConfigMutable               = &TomlS3V1{}
	_ ConfigInstanceIdMintable    = &TomlS3V1{}
	_ ConfigUpgradeable           = TomlPointerV1{}
	_ ConfigPointer               = TomlPointerV2{}
	_ ConfigMutable               = &TomlPointerV2{}
	_ ConfigInstanceIdMintable    = &TomlPointerV2{}
	_ ConfigUpgradeable           = TomlMultiV0{}
	_ ConfigMulti                 = TomlMultiV1{}
	_ ConfigMutable               = &TomlMultiV1{}
	_ ConfigInstanceIdMintable    = &TomlMultiV1{}
	_ ConfigUpgradeable           = TomlInventoryArchiveV2{}
	_ ConfigInventoryArchiveDelta = TomlInventoryArchiveV3{}
	_ ConfigMutable               = &TomlInventoryArchiveV3{}
	_ SignatureConfigImmutable    = TomlInventoryArchiveV3{}
	_ SelectorConfigImmutable     = TomlInventoryArchiveV3{}
	_ ConfigInstanceIdMintable    = &TomlInventoryArchiveV3{}
)

type DefaultType = TomlV4

func Default() *TypedMutableConfig {
	return &TypedMutableConfig{
		Type: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigVCurrent).TypeStruct,
		Blob: &DefaultType{
			HashBuckets:     DefaultHashBuckets,
			HashTypeId:      HashTypeDefault,
			CompressionType: "zstd",
		},
	}
}

// TypeStructForConfig returns the wire type-id (TypeStruct) that the
// hyphence Coder uses to decode/encode the given Config. Inverts the
// type-id → coder map in coding.go. Panics if the concrete Config
// type is not one of the registered variants — keep this in sync with
// the Coder map when adding a new on-disk config type.
//
// Used by callers that need to wrap a freestanding Config back into a
// TypedBlob for encoding (e.g. info-repo's config-immutable encoder
// per ADR 0005).
func TypeStructForConfig(config Config) ids.TypeStruct {
	var typeId string

	switch config.(type) {
	case *TomlLocalHashBucketedV1, TomlLocalHashBucketedV1:
		typeId = ids.TypeTomlBlobStoreConfigV1
	case *TomlLocalHashBucketedV2, TomlLocalHashBucketedV2:
		typeId = ids.TypeTomlBlobStoreConfigV2
	case *TomlV3, TomlV3:
		typeId = ids.TypeTomlBlobStoreConfigV3
	case *TomlV4, TomlV4:
		typeId = ids.TypeTomlBlobStoreConfigV4
	case *TomlSFTPV0:
		typeId = ids.TypeTomlBlobStoreConfigSftpExplicitV0
	case *TomlSFTPViaSSHConfigV0, TomlSFTPViaSSHConfigV0:
		typeId = ids.TypeTomlBlobStoreConfigSftpViaSSHConfigV0
	case *TomlWebDAVV0:
		typeId = ids.TypeTomlBlobStoreConfigWebdavV0
	case *TomlS3V0:
		typeId = ids.TypeTomlBlobStoreConfigS3V0
	case *TomlPointerV0, TomlPointerV0:
		typeId = ids.TypeTomlBlobStoreConfigPointerV0
	case *TomlPointerV1, TomlPointerV1:
		typeId = ids.TypeTomlBlobStoreConfigPointerV1
	case *TomlMultiV0, TomlMultiV0:
		typeId = ids.TypeTomlBlobStoreConfigMultiV0
	case *TomlInventoryArchiveV0, TomlInventoryArchiveV0:
		typeId = ids.TypeTomlBlobStoreConfigInventoryArchiveV0
	case *TomlInventoryArchiveV1, TomlInventoryArchiveV1:
		typeId = ids.TypeTomlBlobStoreConfigInventoryArchiveV1
	case *TomlInventoryArchiveV2, TomlInventoryArchiveV2:
		typeId = ids.TypeTomlBlobStoreConfigInventoryArchiveV2
	case *TomlSFTPV1:
		typeId = ids.TypeTomlBlobStoreConfigSftpExplicitV1
	case *TomlSFTPViaSSHConfigV1, TomlSFTPViaSSHConfigV1:
		typeId = ids.TypeTomlBlobStoreConfigSftpViaSSHConfigV1
	case *TomlWebDAVV1:
		typeId = ids.TypeTomlBlobStoreConfigWebdavV1
	case *TomlS3V1:
		typeId = ids.TypeTomlBlobStoreConfigS3V1
	case *TomlPointerV2, TomlPointerV2:
		typeId = ids.TypeTomlBlobStoreConfigPointerV2
	case *TomlMultiV1, TomlMultiV1:
		typeId = ids.TypeTomlBlobStoreConfigMultiV1
	case *TomlInventoryArchiveV3, TomlInventoryArchiveV3:
		typeId = ids.TypeTomlBlobStoreConfigInventoryArchiveV3
	default:
		panic(fmt.Sprintf(
			"no wire type-id known for blob store config of type %T",
			config,
		))
	}

	return ids.GetOrPanic(typeId).TypeStruct
}
