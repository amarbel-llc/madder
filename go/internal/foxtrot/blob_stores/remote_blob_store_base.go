package blob_stores

import (
	"sync"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_io"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/ui"
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

	// sealedKey is set by adoptSealedKey when the remote config is a
	// sealed-key config (FDR 0011); nil otherwise.
	sealedKey domain_interfaces.MarklId

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

	// blobCacheComplete is set once the whole store has been listed into
	// blobCache, after which a cache miss means the blob is absent.
	blobCacheComplete bool
}

// BlobPresencePrimer is implemented by stores for which checking blobs one
// at a time is expensive and listing everything once is cheap by
// comparison. After a successful PrimeBlobPresence, HasBlob answers
// without contacting the store.
type BlobPresencePrimer interface {
	PrimeBlobPresence() error
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
		base.blobEncryption(),
	)
}

// blobEncryption is the encryption id blobs are read and written with: the
// cached sealed-key id for a sealed-key store, the remote config's own
// otherwise.
func (base *remoteBlobStoreBase) blobEncryption() domain_interfaces.MarklId {
	if base.sealedKey != nil {
		return base.sealedKey
	}

	return base.blobIOWrapper.GetBlobEncryption()
}

// adoptSealedKey is called by each transport's readRemoteConfig once
// remoteConfig is set. For a sealed-key remote config (FDR 0011) it wires
// in the store key, whose sealed half loadSidecar fetches from the
// blob_store-key file at the remote root on the first blob read. For any
// other config it does nothing.
func (base *remoteBlobStoreBase) adoptSealedKey(
	loadSidecar func() ([]byte, error),
) (err error) {
	sealedKeyConfig, ok := base.remoteConfig.(blob_store_configs.ConfigSealedKey)
	if !ok {
		return nil
	}

	if base.sealedKey, err = makeSealedKeyEncryption(
		base.id.String(),
		sealedKeyConfig,
		loadSidecar,
	); err != nil {
		err = errors.Wrap(err)
		return err
	}

	return nil
}

// sealedKeyUnsupportedLoader is the sidecar loader for transports that
// cannot open a sealed-key store yet: writes still work (they need only the
// public key), reads fail naming the reason.
func sealedKeyUnsupportedLoader(transport string) func() ([]byte, error) {
	return func() ([]byte, error) {
		return nil, errors.Errorf(
			"reading a sealed-key store over %s is not supported yet",
			transport,
		)
	}
}
