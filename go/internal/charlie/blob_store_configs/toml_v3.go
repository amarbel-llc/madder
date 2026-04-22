package blob_store_configs

import (
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
	"github.com/amarbel-llc/purse-first/libs/dewey/delta/compression_type"
)

//go:generate tommy generate
type TomlV3 struct {
	HashBuckets values.IntSlice `toml:"hash_buckets"`
	BasePath    string          `toml:"base_path,omitempty"`
	HashTypeId  HashType        `toml:"hash_type-id"`

	Encryption []markl.Id `toml:"encryption"`

	CompressionType compression_type.CompressionType `toml:"compression-type"`

	// VerifyOnCollision opts this store into git-style byte-level
	// collision verification: on EEXIST from the blob-mover's link(2)
	// publish, compare the temp file against the existing blob and fail
	// if they differ. Off by default. See issue #31, ADR 0002, ADR 0003.
	VerifyOnCollision bool `toml:"verify-on-collision"`
}

func (TomlV3) GetBlobStoreType() string {
	return "local"
}

func (blobStoreConfig *TomlV3) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	blobStoreConfig.CompressionType.SetFlagDefinitions(flagSet)

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

	setMultiEncryptionFlagDefinition(flagSet, &blobStoreConfig.Encryption)

	flagSet.BoolVar(
		&blobStoreConfig.VerifyOnCollision,
		"verify-on-collision",
		blobStoreConfig.VerifyOnCollision,
		"byte-compare on EEXIST during publish to catch hash collisions",
	)
}

func (blobStoreConfig TomlV3) getBasePath() string {
	return blobStoreConfig.BasePath
}

func (blobStoreConfig TomlV3) GetHashBuckets() []int {
	return blobStoreConfig.HashBuckets
}

func (blobStoreConfig TomlV3) GetBlobCompression() interfaces.IOWrapper {
	return &blobStoreConfig.CompressionType
}

func (blobStoreConfig TomlV3) GetBlobEncryption() domain_interfaces.MarklId {
	return EncryptionKeys(blobStoreConfig.Encryption)
}

func (blobStoreConfig TomlV3) GetVerifyOnCollision() bool {
	return blobStoreConfig.VerifyOnCollision
}

func (blobStoreConfig TomlV3) SupportsMultiHash() bool {
	return true
}

func (blobStoreConfig TomlV3) GetDefaultHashTypeId() string {
	return string(blobStoreConfig.HashTypeId)
}

func (blobStoreConfig *TomlV3) setBasePath(value string) {
	blobStoreConfig.BasePath = value
}
