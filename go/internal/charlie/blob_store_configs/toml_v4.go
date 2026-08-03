package blob_store_configs

import (
	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/bravo/plugins"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/values"
)

// TomlV4 is TomlV3 plus an InstanceId: a uuidv7 markl-id minted once at
// store creation (FDR-0010). Because InstanceId is a config-body field, the
// FDR-0008 config digest covers it, making the digest instance-unique — so
// a `name@digest` reference identifies a specific store instance, not just a
// configuration. Minted at init (golf/command_components InitBlobStore) via
// ConfigInstanceIdMintable; never on read (FDR-0010's no-lazy-mint rule), so
// legacy V1–V3 configs and upgraded-in-memory V4s carry an empty InstanceId
// until a store is copy-migrated.
//
//go:generate tommy generate
type TomlV4 struct {
	HashBuckets values.IntSlice `toml:"hash_buckets"`
	BasePath    string          `toml:"base_path,omitempty"`
	HashTypeId  HashType        `toml:"hash_type-id"`

	Encryption []markl.Id `toml:"encryption"`

	CompressionType string `toml:"compression-type"`

	VerifyOnCollision bool `toml:"verify-on-collision"`

	SingleHash bool `toml:"single_hash,omitempty"`

	// InstanceId is the store's uuidv7 instance identity (FDR-0010),
	// rendered `uuidv7-<blech32>`. Empty for a config that predates the
	// mint or was upgraded in memory from an older version.
	InstanceId markl.Id `toml:"instance-id,omitempty"`
}

func (TomlV4) GetBlobStoreType() string {
	return "local"
}

func (blobStoreConfig *TomlV4) SetFlagDefinitions(
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

func (blobStoreConfig TomlV4) getBasePath() string {
	return blobStoreConfig.BasePath
}

func (blobStoreConfig TomlV4) GetHashBuckets() []int {
	return blobStoreConfig.HashBuckets
}

func (blobStoreConfig TomlV4) GetCompressionType() string {
	return blobStoreConfig.CompressionType
}

func (blobStoreConfig TomlV4) GetBlobCompression() interfaces.IOWrapper {
	ref, err := plugins.LegacyCompressionRef(blobStoreConfig.CompressionType)
	if err != nil {
		ref = "madder-codec-none-v1@none"
	}
	plugin, err := plugins.Resolve(ref)
	if err != nil {
		panic(err) // Programming error: registry should always have these.
	}
	return plugin
}

func (blobStoreConfig TomlV4) GetBlobEncryption() domain_interfaces.MarklId {
	return EncryptionKeys(blobStoreConfig.Encryption)
}

func (blobStoreConfig TomlV4) GetVerifyOnCollision() bool {
	return blobStoreConfig.VerifyOnCollision
}

func (blobStoreConfig TomlV4) SupportsMultiHash() bool {
	return !blobStoreConfig.SingleHash
}

func (blobStoreConfig TomlV4) GetDefaultHashTypeId() string {
	return string(blobStoreConfig.HashTypeId)
}

func (blobStoreConfig *TomlV4) setBasePath(value string) {
	blobStoreConfig.BasePath = value
}

// GetInstanceId returns the store's uuidv7 instance identity (FDR-0010),
// or the empty markl.Id when the config carries none.
func (blobStoreConfig TomlV4) GetInstanceId() markl.Id {
	return blobStoreConfig.InstanceId
}

// SetInstanceId sets the store's instance identity. Called ONCE at store
// creation (InitBlobStore) with a freshly minted uuidv7, before the config
// is digest-stamped and written immutably. Never called on read.
func (blobStoreConfig *TomlV4) SetInstanceId(instanceId markl.Id) {
	blobStoreConfig.InstanceId = instanceId
}
