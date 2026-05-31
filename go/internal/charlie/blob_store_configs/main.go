package blob_store_configs

import (
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/0/ids"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/bravo/directory_layout"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/values"
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
		GetLooseBlobStoreId() blob_store_id.Id
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
	// via the Multi primitive. References are typed blob_store_id.Id
	// values, parsed by the hyphence coder at decode time and
	// validated as digest-bearing by Validate() (also at decode), so
	// the accessors never return errors. The store-map factory does
	// only lookup + digest assertion. See FDR-0009.
	ConfigMulti interface {
		Config
		GetMode() string                     // "mirror" | "write_through"
		GetWriteStore() blob_store_id.Id     // write_through; zero otherwise
		GetReadStores() []blob_store_id.Id   // write_through; nil otherwise
		GetMirrorStores() []blob_store_id.Id // mirror; nil otherwise
		GetReadFill() bool                   // defaults true; mirror ignores
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
