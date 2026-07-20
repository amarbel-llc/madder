package blob_store_configs

import (
	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/0/ids"
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/bravo/plugins"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
)

//go:generate tommy generate
type TomlInventoryArchiveV0 struct {
	HashTypeId       HashType     `toml:"hash_type-id"`
	CompressionType  string       `toml:"compression-type"`
	LooseBlobStoreId scoped_id.Id `toml:"loose-blob-store-id"`
	Encryption       markl.Id     `toml:"encryption"`
}

func (TomlInventoryArchiveV0) GetBlobStoreType() string {
	return "local-inventory-archive"
}

func (config *TomlInventoryArchiveV0) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.StringVar(
		&config.CompressionType,
		"compression-type",
		config.CompressionType,
		"",
	)

	config.HashTypeId = HashTypeDefault

	flagSet.Var(
		&config.HashTypeId,
		"hash_type-id",
		"hash type for archive checksums and blob hashes",
	)

	flagSet.Var(
		&config.LooseBlobStoreId,
		"loose-blob-store-id",
		"id of the loose blob store to read from and write to",
	)
}

func (config TomlInventoryArchiveV0) getBasePath() string {
	return ""
}

func (config TomlInventoryArchiveV0) SupportsMultiHash() bool {
	return false
}

func (config TomlInventoryArchiveV0) GetDefaultHashTypeId() string {
	return string(config.HashTypeId)
}

func (config TomlInventoryArchiveV0) GetCompressionType() string {
	return config.CompressionType
}

func (config TomlInventoryArchiveV0) GetBlobCompression() interfaces.IOWrapper {
	ref, err := plugins.LegacyCompressionRef(config.CompressionType)
	if err != nil {
		ref = "madder-codec-none-v1@none"
	}
	plugin, err := plugins.Resolve(ref)
	if err != nil {
		panic(err) // Programming error: registry should always have these.
	}
	return plugin
}

func (config TomlInventoryArchiveV0) GetBlobEncryption() domain_interfaces.MarklId {
	return config.Encryption
}

func (config TomlInventoryArchiveV0) GetLooseBlobStoreId() scoped_id.Id {
	return config.LooseBlobStoreId
}

func (config TomlInventoryArchiveV0) GetCompressionRef() string {
	ref, err := plugins.LegacyCompressionRef(config.CompressionType)
	if err != nil {
		return "madder-codec-none-v1@none"
	}
	return ref
}

func (config TomlInventoryArchiveV0) GetMaxPackSize() uint64 {
	return 536870912 // 512 MiB default for V0
}

func (config TomlInventoryArchiveV0) Upgrade() (Config, ids.TypeStruct) {
	upgraded := &TomlInventoryArchiveV1{
		HashTypeId:       config.HashTypeId,
		CompressionType:  config.CompressionType,
		LooseBlobStoreId: config.LooseBlobStoreId,
		Delta: DeltaConfig{
			Enabled:     false,
			Algorithm:   "bsdiff",
			MinBlobSize: 256,
			MaxBlobSize: 10485760,
			SizeRatio:   2.0,
		},
		MaxPackSize: 536870912,
	}

	upgraded.Encryption.ResetWithMarklId(config.Encryption)

	return upgraded, ids.GetOrPanic(
		ids.TypeTomlBlobStoreConfigInventoryArchiveV1,
	).TypeStruct
}
