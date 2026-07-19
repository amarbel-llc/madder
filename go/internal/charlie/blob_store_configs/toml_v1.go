package blob_store_configs

import (
	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/0/ids"
	"code.linenisgreat.com/madder/go/internal/bravo/plugins"
	"github.com/amarbel-llc/piggy/go/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/values"
)

// TomlLocalHashBucketedV1 is the V1 configuration for the local hash-bucketed blob store.
//
//go:generate tommy generate
type TomlLocalHashBucketedV1 struct {
	HashBuckets values.IntSlice `toml:"hash-buckets"`
	BasePath    string          `toml:"base-path,omitempty"`
	HashTypeId  HashType        `toml:"hash_type-id"`

	// cannot use `omitempty`, as markl.Id's empty value equals its non-empty
	// value due to unexported fields
	Encryption markl.Id `toml:"encryption"`

	CompressionType string `toml:"compression-type"`
}

func (TomlLocalHashBucketedV1) GetBlobStoreType() string {
	return "local"
}

func (blobStoreConfig *TomlLocalHashBucketedV1) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.StringVar(
		&blobStoreConfig.CompressionType,
		"compression-type",
		blobStoreConfig.CompressionType,
		"",
	)

	blobStoreConfig.HashBuckets = DefaultHashBuckets

	flagSet.Var(
		&blobStoreConfig.HashBuckets,
		"hash_buckets",
		"determines hash bucketing directory structure",
	)

	blobStoreConfig.HashTypeId = HashTypeDefault

	flagSet.Var(
		&blobStoreConfig.HashTypeId,
		"hash_type-id",
		"determines the hash type used for new blobs written to the store",
	)

	setEncryptionFlagDefinition(flagSet, &blobStoreConfig.Encryption)
}

func (blobStoreConfig TomlLocalHashBucketedV1) getBasePath() string {
	return blobStoreConfig.BasePath
}

func (blobStoreConfig TomlLocalHashBucketedV1) GetHashBuckets() []int {
	return blobStoreConfig.HashBuckets
}

func (blobStoreConfig TomlLocalHashBucketedV1) GetCompressionType() string {
	return blobStoreConfig.CompressionType
}

func (blobStoreConfig TomlLocalHashBucketedV1) GetBlobCompression() interfaces.IOWrapper {
	ref, err := plugins.LegacyCompressionRef(blobStoreConfig.CompressionType)
	if err != nil {
		// Hand-edited TOML with an unknown compression-type value;
		// fall back to none so the rest of the pipeline reports the
		// misuse via a downstream decode error rather than panicking
		// at config load.
		ref = "madder-codec-none-v1@none"
	}
	plugin, err := plugins.Resolve(ref)
	if err != nil {
		panic(err) // Programming error: registry should always have these.
	}
	return plugin
}

func (blobStoreConfig TomlLocalHashBucketedV1) GetBlobEncryption() domain_interfaces.MarklId {
	return blobStoreConfig.Encryption
}

// GetVerifyOnCollision is always false on v1; the flag was introduced in
// TomlV3. Stores pinned to v1 that want byte-level collision verification
// must upgrade.
func (blobStoreConfig TomlLocalHashBucketedV1) GetVerifyOnCollision() bool {
	return false
}

func (blobStoreConfig TomlLocalHashBucketedV1) SupportsMultiHash() bool {
	return true
}

func (blobStoreConfig TomlLocalHashBucketedV1) GetDefaultHashTypeId() string {
	return string(blobStoreConfig.HashTypeId)
}

func (blobStoreConfig *TomlLocalHashBucketedV1) setBasePath(value string) {
	blobStoreConfig.BasePath = value
}

func (blobStoreConfig TomlLocalHashBucketedV1) Upgrade() (Config, ids.TypeStruct) {
	upgraded := &TomlLocalHashBucketedV2{
		HashBuckets:     blobStoreConfig.HashBuckets,
		BasePath:        blobStoreConfig.BasePath,
		HashTypeId:      HashTypeSha256,
		CompressionType: blobStoreConfig.CompressionType,
	}

	upgraded.Encryption.ResetWithMarklId(blobStoreConfig.Encryption)

	return upgraded, ids.GetOrPanic(ids.TypeTomlBlobStoreConfigV2).TypeStruct
}
