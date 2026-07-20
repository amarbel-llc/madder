package blob_store_configs

import (
	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/0/ids"
	"code.linenisgreat.com/madder/go/internal/bravo/plugins"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/values"
)

// TomlLocalHashBucketedV2 is the V2 configuration for the local hash-bucketed blob store.
//
//go:generate tommy generate
type TomlLocalHashBucketedV2 struct {
	HashBuckets values.IntSlice `toml:"hash_buckets"`
	BasePath    string          `toml:"base_path,omitempty"`
	HashTypeId  HashType        `toml:"hash_type-id"`

	// cannot use `omitempty`, as markl.Id's empty value equals its non-empty
	// value due to unexported fields
	Encryption markl.Id `toml:"encryption"`

	CompressionType string `toml:"compression-type"`
}

func (TomlLocalHashBucketedV2) GetBlobStoreType() string {
	return "local"
}

func (blobStoreConfig *TomlLocalHashBucketedV2) SetFlagDefinitions(
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

func (blobStoreConfig TomlLocalHashBucketedV2) getBasePath() string {
	return blobStoreConfig.BasePath
}

func (blobStoreConfig TomlLocalHashBucketedV2) GetHashBuckets() []int {
	return blobStoreConfig.HashBuckets
}

func (blobStoreConfig TomlLocalHashBucketedV2) GetCompressionType() string {
	return blobStoreConfig.CompressionType
}

func (blobStoreConfig TomlLocalHashBucketedV2) GetBlobCompression() interfaces.IOWrapper {
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

func (blobStoreConfig TomlLocalHashBucketedV2) GetBlobEncryption() domain_interfaces.MarklId {
	return blobStoreConfig.Encryption
}

// GetVerifyOnCollision is always false on v2; the flag was introduced in
// TomlV3. Stores pinned to v2 that want byte-level collision verification
// must upgrade.
func (blobStoreConfig TomlLocalHashBucketedV2) GetVerifyOnCollision() bool {
	return false
}

func (blobStoreConfig TomlLocalHashBucketedV2) SupportsMultiHash() bool {
	return true
}

func (blobStoreConfig TomlLocalHashBucketedV2) GetDefaultHashTypeId() string {
	return string(blobStoreConfig.HashTypeId)
}

func (blobStoreConfig *TomlLocalHashBucketedV2) setBasePath(value string) {
	blobStoreConfig.BasePath = value
}

func (blobStoreConfig TomlLocalHashBucketedV2) Upgrade() (Config, ids.TypeStruct) {
	upgraded := &TomlV3{
		HashBuckets:     blobStoreConfig.HashBuckets,
		BasePath:        blobStoreConfig.BasePath,
		HashTypeId:      blobStoreConfig.HashTypeId,
		CompressionType: blobStoreConfig.CompressionType,
	}

	if !blobStoreConfig.Encryption.IsNull() {
		upgraded.Encryption = []markl.Id{blobStoreConfig.Encryption}
	}

	return upgraded, ids.GetOrPanic(ids.TypeTomlBlobStoreConfigV3).TypeStruct
}
