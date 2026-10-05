package blob_store_configs

import (
	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/bravo/plugins"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/values"
)

// KeyHolderProcess is the only key-custody holder FDR 0011 defines: the
// unsealed store key lives in the madder process that opened it.
const KeyHolderProcess = "process"

// KeyCustody names who holds a store's unsealed key at run time. It is a
// table, not a bare field, so a later holder (a key-holding agent, a
// virtual card) can carry its own settings without another config
// version. See FDR 0011.
//
//go:generate tommy generate
type KeyCustody struct {
	Holder string `toml:"holder"`
}

// TomlV5 is TomlV4 for a store whose key is sealed in a pigpen document
// (FDR 0011). It is NOT the default config version: only stores created
// against a pigpen recipient set use it, so every other new store stays
// readable by a madder that predates this type.
//
// Differences from TomlV4, all enforced by Validate:
//   - Encryption holds exactly one id, the store's PUBLIC key
//     (age_x25519_pub). No secret is ever written to this config, which is
//     what lets it live on a remote (madder#296).
//   - KeyCustody.Holder names who holds the unsealed key.
//
// The sealed secret half lives in a mutable sidecar next to this config,
// because it changes when recipients change and this config must not.
//
//go:generate tommy generate
type TomlV5 struct {
	HashBuckets values.IntSlice `toml:"hash_buckets"`
	BasePath    string          `toml:"base_path,omitempty"`
	HashTypeId  HashType        `toml:"hash_type-id"`

	Encryption []markl.Id `toml:"encryption"`

	CompressionType string `toml:"compression-type"`

	VerifyOnCollision bool `toml:"verify-on-collision"`

	SingleHash bool `toml:"single_hash,omitempty"`

	InstanceId markl.Id `toml:"instance-id,omitempty"`

	KeyCustody KeyCustody `toml:"key-custody"`
}

func (TomlV5) GetBlobStoreType() string {
	return "local"
}

// Validate rejects a config that could not safely be used as a sealed-key
// store. A config naming a holder this madder does not know is refused
// outright: guessing would mean reading the store key from the wrong
// place.
func (blobStoreConfig TomlV5) Validate() error {
	// The generated decoder validates the blank document the coder
	// builds before filling it in for an encode, so the untouched zero
	// value has to pass. Anything a real file would carry (a hash type,
	// at the least) takes the checks below.
	if blobStoreConfig.KeyCustody.Holder == "" &&
		len(blobStoreConfig.Encryption) == 0 &&
		blobStoreConfig.HashTypeId == "" {
		return nil
	}

	if blobStoreConfig.KeyCustody.Holder != KeyHolderProcess {
		return errors.Errorf(
			"unsupported key-custody holder %q (this madder supports %q)",
			blobStoreConfig.KeyCustody.Holder,
			KeyHolderProcess,
		)
	}

	if len(blobStoreConfig.Encryption) != 1 {
		return errors.Errorf(
			"a sealed-key store has exactly one encryption id, its public key; got %d",
			len(blobStoreConfig.Encryption),
		)
	}

	format := blobStoreConfig.Encryption[0].GetMarklFormat()

	if format == nil || format.GetMarklFormatId() != markl.FormatIdAgeX25519Pub {
		return errors.Errorf(
			"a sealed-key store's encryption id must be an %s public key, got %q",
			markl.FormatIdAgeX25519Pub,
			blobStoreConfig.Encryption[0],
		)
	}

	return nil
}

// SetFlagDefinitions registers the flags shared with TomlV4, minus
// -encryption: a sealed-key store's key is minted and sealed by init, not
// supplied on the command line.
func (blobStoreConfig *TomlV5) SetFlagDefinitions(
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

	flagSet.BoolVar(
		&blobStoreConfig.VerifyOnCollision,
		"verify-on-collision",
		blobStoreConfig.VerifyOnCollision,
		"byte-compare on EEXIST during publish to catch hash collisions",
	)
}

func (blobStoreConfig TomlV5) getBasePath() string {
	return blobStoreConfig.BasePath
}

func (blobStoreConfig TomlV5) GetHashBuckets() []int {
	return blobStoreConfig.HashBuckets
}

func (blobStoreConfig TomlV5) GetCompressionType() string {
	return blobStoreConfig.CompressionType
}

func (blobStoreConfig TomlV5) GetBlobCompression() interfaces.IOWrapper {
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

// GetBlobEncryption returns the store's public key. On its own it can
// only encrypt; a store builds its read path from the sealed sidecar via
// the store_key package.
func (blobStoreConfig TomlV5) GetBlobEncryption() domain_interfaces.MarklId {
	return EncryptionKeys(blobStoreConfig.Encryption)
}

// GetStorePublicKey returns the store's public key id, or the empty id
// for a config that fails Validate.
func (blobStoreConfig TomlV5) GetStorePublicKey() markl.Id {
	if len(blobStoreConfig.Encryption) != 1 {
		return markl.Id{}
	}

	return blobStoreConfig.Encryption[0]
}

func (blobStoreConfig TomlV5) GetKeyCustodyHolder() string {
	return blobStoreConfig.KeyCustody.Holder
}

func (blobStoreConfig TomlV5) GetVerifyOnCollision() bool {
	return blobStoreConfig.VerifyOnCollision
}

func (blobStoreConfig TomlV5) SupportsMultiHash() bool {
	return !blobStoreConfig.SingleHash
}

func (blobStoreConfig TomlV5) GetDefaultHashTypeId() string {
	return string(blobStoreConfig.HashTypeId)
}

func (blobStoreConfig *TomlV5) setBasePath(value string) {
	blobStoreConfig.BasePath = value
}

func (blobStoreConfig TomlV5) GetInstanceId() markl.Id {
	return blobStoreConfig.InstanceId
}

func (blobStoreConfig *TomlV5) SetInstanceId(instanceId markl.Id) {
	blobStoreConfig.InstanceId = instanceId
}
