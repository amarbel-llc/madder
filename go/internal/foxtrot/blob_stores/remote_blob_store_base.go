package blob_stores

import (
	"sync"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_io"
	"github.com/amarbel-llc/piggy/go/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/ui"
)

// remoteBlobStoreBase holds the state and behavior common to the three
// remote hash-bucketed blob stores (remoteSftp, remoteS3, remoteWebdav).
// It is embedded ANONYMOUSLY so its fields and methods promote to the
// outer store — every store method still reads blobStore.ctx,
// blobStore.multiHash, blobStore.makeEnvDirConfig(...), etc. unchanged.
// Only the transport differs per store (the typed `config`, the client
// handle, and any protocol-specific path caches), which stays on the
// outer struct. See #263; extracted after the #261/#262 hash-type fix
// showed the three stores were structural twins.
type remoteBlobStoreBase struct {
	ctx       interfaces.ActiveContext
	uiPrinter ui.Printer
	once      sync.Once

	id scoped_id.Id

	buckets []int

	// remoteConfig is the authoritative blob-store-properties config
	// decoded from the remote `blob_store-config` per ADR 0005; the
	// per-store local `config` is transport only. nil before
	// initializeOnce runs.
	remoteConfig blob_store_configs.Config

	multiHash       bool
	defaultHashType markl.FormatHash

	// blobIOWrapper holds the remote config's compression / encryption
	// view per ADR 0005. Populated by readRemoteConfig; nil before
	// initializeOnce runs.
	blobIOWrapper domain_interfaces.BlobIOWrapper

	// initErr is the sticky error captured by initializeOnce when
	// initialize() fails. sync.Once does not re-run f after a panic, so
	// the wrapped error is cached here and re-surfaced on each
	// subsequent call rather than proceeding against a half-initialized
	// store (see issue #134).
	initErr error

	// observer receives one BlobWriteEvent per successful upload, or is
	// nil when audit logging is disabled (the movers' emitWriteEvent
	// absorbs the nil case).
	observer domain_interfaces.BlobWriteObserver

	blobCacheLock sync.RWMutex
	blobCache     map[string]struct{}
}

// makeEnvDirConfig builds the blob_io.Config for a read or write,
// digesting under hashFormat (nil falls back to the store default) and
// wiring in the remote config's compression / encryption per ADR 0005.
func (base *remoteBlobStoreBase) makeEnvDirConfig(
	hashFormat domain_interfaces.FormatHash,
) blob_io.Config {
	if hashFormat == nil {
		hashFormat = base.defaultHashType
	}

	return blob_io.MakeConfig(
		hashFormat,
		blob_io.MakeHashBucketPathJoinFunc(base.buckets),
		base.blobIOWrapper.GetBlobCompression(),
		base.blobIOWrapper.GetBlobEncryption(),
	)
}
