package blob_store_configs

import (
	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/bravo/plugins"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
)

//go:generate tommy generate
type TomlInventoryArchiveV3 struct {
	HashTypeId      HashType    `toml:"hash_type-id"`
	CompressionType string      `toml:"compression-type"`
	Encryption      markl.Id    `toml:"encryption"`
	Delta           DeltaConfig `toml:"delta"`
	MaxPackSize     uint64      `toml:"max-pack-size"`

	// InstanceId is the store's uuidv7 instance identity (FDR-0010),
	// minted once at creation inside EncodeWithDigest. Empty for a legacy
	// config or one upgraded in memory from V2.
	InstanceId markl.Id `toml:"instance-id,omitempty"`
}

func (TomlInventoryArchiveV3) GetBlobStoreType() string {
	return "local-inventory-archive"
}

func (config *TomlInventoryArchiveV3) SetFlagDefinitions(
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

	setEncryptionFlagDefinition(flagSet, &config.Encryption)

	flagSet.BoolVar(
		&config.Delta.Enabled,
		"delta",
		false,
		"enable delta compression",
	)
}

func (config TomlInventoryArchiveV3) getBasePath() string {
	return ""
}

func (config TomlInventoryArchiveV3) SupportsMultiHash() bool {
	return false
}

func (config TomlInventoryArchiveV3) GetDefaultHashTypeId() string {
	return string(config.HashTypeId)
}

func (config TomlInventoryArchiveV3) GetCompressionType() string {
	return config.CompressionType
}

func (config TomlInventoryArchiveV3) GetBlobCompression() interfaces.IOWrapper {
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

func (config TomlInventoryArchiveV3) GetBlobEncryption() domain_interfaces.MarklId {
	return config.Encryption
}

func (config TomlInventoryArchiveV3) GetLooseBlobStoreId() scoped_id.Id {
	var zero scoped_id.Id
	return zero
}

func (config TomlInventoryArchiveV3) GetCompressionRef() string {
	ref, err := plugins.LegacyCompressionRef(config.CompressionType)
	if err != nil {
		return "madder-codec-none-v1@none"
	}
	return ref
}

// DeltaConfigImmutable implementation

func (config TomlInventoryArchiveV3) GetDeltaEnabled() bool {
	return config.Delta.Enabled
}

func (config TomlInventoryArchiveV3) GetDeltaAlgorithm() string {
	return config.Delta.Algorithm
}

func (config TomlInventoryArchiveV3) GetDeltaMinBlobSize() uint64 {
	return config.Delta.MinBlobSize
}

func (config TomlInventoryArchiveV3) GetDeltaMaxBlobSize() uint64 {
	return config.Delta.MaxBlobSize
}

func (config TomlInventoryArchiveV3) GetDeltaSizeRatio() float64 {
	return config.Delta.SizeRatio
}

// SignatureConfigImmutable implementation

func (config TomlInventoryArchiveV3) GetSignatureType() string {
	return config.Delta.Signature.Type
}

func (config TomlInventoryArchiveV3) GetSignatureLen() int {
	return config.Delta.Signature.SignatureLen
}

func (config TomlInventoryArchiveV3) GetAvgChunkSize() int {
	return config.Delta.Signature.AvgChunkSize
}

func (config TomlInventoryArchiveV3) GetMinChunkSize() int {
	return config.Delta.Signature.MinChunkSize
}

func (config TomlInventoryArchiveV3) GetMaxChunkSize() int {
	return config.Delta.Signature.MaxChunkSize
}

// SelectorConfigImmutable implementation

func (config TomlInventoryArchiveV3) GetSelectorType() string {
	return config.Delta.Selector.Type
}

func (config TomlInventoryArchiveV3) GetSelectorBands() int {
	return config.Delta.Selector.Bands
}

func (config TomlInventoryArchiveV3) GetSelectorRowsPerBand() int {
	return config.Delta.Selector.RowsPerBand
}

func (config TomlInventoryArchiveV3) GetSelectorMinBlobSize() uint64 {
	return config.Delta.Selector.MinBlobSize
}

func (config TomlInventoryArchiveV3) GetSelectorMaxBlobSize() uint64 {
	return config.Delta.Selector.MaxBlobSize
}

func (config TomlInventoryArchiveV3) GetMaxPackSize() uint64 {
	return config.MaxPackSize
}

func (config TomlInventoryArchiveV3) GetInstanceId() markl.Id {
	return config.InstanceId
}

func (config *TomlInventoryArchiveV3) SetInstanceId(instanceId markl.Id) {
	config.InstanceId = instanceId
}
