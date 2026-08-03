package blob_store_configs

import (
	// madder#278: activate madder's markl format/purpose registrations at
	// the store-config funnel. Every store-facing public package
	// (pkgs/blob_store_env, blob_stores, blob_io, madder_env,
	// inventory_archive, blob_store_configs) reaches THIS package when it
	// resolves an encryption format (… → delta/blob_store_configs → here),
	// so this single blank import makes an external in-process consumer of
	// those packages activate the age/pivy format swaps transitively — no
	// separate `_ pkgs/markl_registrations` blank import needed, closing the
	// invisible runtime `unknown format id: "age_x25519_sec"` footgun.
	//
	// Deliberate trade-off (madder#278, option A): this makes importing this
	// package non-pure and links piggy's age/pivy deps for every store
	// consumer, even a plaintext-only one — accepted to eliminate the
	// footgun at the single funnel. Safe under Go's init-once semantics even
	// if a consumer ALSO blank-imports pkgs/markl_registrations. No import
	// cycle: markl_registrations imports only piggy, never blob_store_configs.
	_ "code.linenisgreat.com/madder/go/internal/charlie/markl_registrations"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/0/ids"
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/bravo/directory_layout"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/values"
)

const DefaultHashTypeId = string(HashTypeSha256)

var DefaultHashType markl.FormatHash = markl.FormatHashSha256

type (
	Config = domain_interfaces.BlobStoreConfig

	ConfigUpgradeable interface {
		Config
		Upgrade() (Config, ids.TypeStruct)
	}

	ConfigMutable interface {
		Config
		interfaces.CommandComponentWriter
	}

	// ConfigInstanceId is implemented by config versions that carry a
	// uuidv7 instance identity (FDR-0010). Empty markl.Id means none
	// (legacy config, or one upgraded in memory from an older version).
	ConfigInstanceId interface {
		GetInstanceId() markl.Id
	}

	// ConfigInstanceIdMintable is implemented by config versions whose
	// instance identity can be minted at store creation. InitBlobStore
	// mints a fresh uuidv7 via SetInstanceId before the config is
	// digest-stamped and written; never called on read (FDR-0010's
	// no-lazy-mint rule).
	ConfigInstanceIdMintable interface {
		ConfigInstanceId
		SetInstanceId(markl.Id)
	}

	ConfigHashType interface {
		SupportsMultiHash() bool
		GetDefaultHashTypeId() string
	}

	ConfigCompressionType interface {
		// GetCompressionType returns the raw on-disk compression-type
		// string field (e.g. "zstd", "gzip", "" for the v1/v2 default).
		// For info-repo's `compression-type` key rendering and any other
		// consumer that needs the on-disk form rather than the resolved
		// plugin instance.
		GetCompressionType() string
	}

	configLocal interface {
		Config
		getBasePath() string
	}

	configLocalMutable interface {
		configLocal
		setBasePath(string)
	}

	ConfigLocalMutable interface {
		configLocalMutable
	}

	ConfigLocalHashBucketed interface {
		configLocal
		ConfigHashType
		domain_interfaces.BlobIOWrapper
		GetHashBuckets() []int
		GetVerifyOnCollision() bool
	}

	ConfigInventoryArchive interface {
		configLocal
		ConfigHashType
		domain_interfaces.BlobIOWrapper
		GetLooseBlobStoreId() scoped_id.Id
		GetCompressionRef() string
		GetMaxPackSize() uint64
	}

	DeltaConfigImmutable interface {
		GetDeltaEnabled() bool
		GetDeltaAlgorithm() string
		GetDeltaMinBlobSize() uint64
		GetDeltaMaxBlobSize() uint64
		GetDeltaSizeRatio() float64
	}

	SignatureConfigImmutable interface {
		GetSignatureType() string
		GetSignatureLen() int
		GetAvgChunkSize() int
		GetMinChunkSize() int
		GetMaxChunkSize() int
	}

	SelectorConfigImmutable interface {
		GetSelectorType() string
		GetSelectorBands() int
		GetSelectorRowsPerBand() int
		GetSelectorMinBlobSize() uint64
		GetSelectorMaxBlobSize() uint64
	}

	ConfigInventoryArchiveDelta interface {
		ConfigInventoryArchive
		DeltaConfigImmutable
	}

	ConfigPointer interface {
		Config
		GetPath() directory_layout.BlobStorePath
	}

	// ConfigMulti is a blob_store-config that composes other stores
	// via the Multi primitive. References are typed scoped_id.Id
	// values, parsed by the hyphence coder at decode time and
	// validated as digest-bearing by Validate() (also at decode), so
	// the accessors never return errors. The store-map factory does
	// only lookup + digest assertion. See FDR-0009.
	ConfigMulti interface {
		Config
		GetMode() string                 // "mirror" | "write_through"
		GetWriteStore() scoped_id.Id     // write_through; zero otherwise
		GetReadStores() []scoped_id.Id   // write_through; nil otherwise
		GetMirrorStores() []scoped_id.Id // mirror; nil otherwise
		GetReadFill() bool               // defaults true; mirror ignores
	}

	ConfigSFTPRemotePath interface {
		Config
		GetRemotePath() string
		GetKnownHostsFile() string
	}

	ConfigSFTPUri interface {
		ConfigSFTPRemotePath

		GetUri() values.Uri
	}

	ConfigSFTPConfigExplicit interface {
		ConfigSFTPRemotePath

		GetHost() string
		GetPort() int
		GetUser() string
		GetPassword() string
		GetPrivateKeyPath() string
	}

	ConfigWebDAV interface {
		Config
		GetURL() string
		GetUser() string
		GetPassword() string
		GetBearerToken() string
		GetTLSClientCertPath() string
		GetTLSClientKeyPath() string
		GetTLSCAPath() string
		GetTLSServerName() string
		GetTLSInsecureSkipVerify() bool
	}

	ConfigS3 interface {
		Config
		GetEndpoint() string
		GetRegion() string
		GetBucket() string
		GetPrefix() string
		GetAccessKeyId() string
		GetSecretAccessKey() string
		GetSessionToken() string
		GetUsePathStyle() bool
		GetInsecureSkipVerify() bool
	}
)

var DefaultHashBuckets []int = []int{2}
