package blob_store_configs

import (
	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/bravo/plugins"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/values"
)

//go:generate tommy generate
type TomlV3 struct {
	HashBuckets values.IntSlice `toml:"hash_buckets"`
	BasePath    string          `toml:"base_path,omitempty"`
	HashTypeId  HashType        `toml:"hash_type-id"`

	Encryption []markl.Id `toml:"encryption"`

	CompressionType string `toml:"compression-type"`

	// VerifyOnCollision opts this store into git-style byte-level
	// collision verification: on EEXIST from the blob-mover's link(2)
	// publish, compare the temp file against the existing blob and fail
	// if they differ. Off by default. See issue #31, ADR 0002, ADR 0003.
	VerifyOnCollision bool `toml:"verify-on-collision"`

	// SingleHash, when true, declares this store is laid out under a
	// flat `<root>/<bucket>/<rest>` structure with no `<HashTypeId>/`
	// parent directory. Defaults to false (multi-hash, the modern
	// shape: `<root>/<HashTypeId>/<bucket>/<rest>`) so existing configs
	// continue to read as multi-hash. Set explicitly when bootstrapping
	// a config for a legacy single-hash store probed by
	// sftp-analyze-and-suggest-configs. See #146 / #148.
	SingleHash bool `toml:"single_hash,omitempty"`
}

func (TomlV3) GetBlobStoreType() string {
	return "local"
}

func (blobStoreConfig *TomlV3) SetFlagDefinitions(
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

	SetMultiEncryptionFlagDefinition(flagSet, &blobStoreConfig.Encryption)

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

func (blobStoreConfig TomlV3) GetCompressionType() string {
	return blobStoreConfig.CompressionType
}

func (blobStoreConfig TomlV3) GetBlobCompression() interfaces.IOWrapper {
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

func (blobStoreConfig TomlV3) GetBlobEncryption() domain_interfaces.MarklId {
	return EncryptionKeys(blobStoreConfig.Encryption)
}

func (blobStoreConfig TomlV3) GetVerifyOnCollision() bool {
	return blobStoreConfig.VerifyOnCollision
}

func (blobStoreConfig TomlV3) SupportsMultiHash() bool {
	return !blobStoreConfig.SingleHash
}

func (blobStoreConfig TomlV3) GetDefaultHashTypeId() string {
	return string(blobStoreConfig.HashTypeId)
}

func (blobStoreConfig *TomlV3) setBasePath(value string) {
	blobStoreConfig.BasePath = value
}
