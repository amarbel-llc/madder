package blob_store_env

//go:generate dagnabit export

import (
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/madder/go/internal/bravo/directory_layout"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
)

// BlobStoreEnv bundles env_local.Env + directory_layout.BlobStore +
// the discovered BlobStoreMap with default-store and ordering machinery.
//
// Constructors live alongside (MakeBlobStoreEnv, MakeBlobStoreEnvWithoutStores,
// MakeBlobStoreEnvWithOrder); methods provide default-store, lookup,
// ordering, and "default + remaining" helpers commands typically need.
type BlobStoreEnv struct {
	directory_layout.BlobStore
	env_local.Env

	defaultBlobStoreIdString string
	orderedBlobStoreIds      []string // nil = alphabetical (discovery mode)

	// TODO switch to implementing LocalBlobStore directly and writing to all of
	// the defined blob stores instead of having a default
	// TODO switch to primary blob store and others, and add support for v10
	// directory layout
	blobStores map[string]blob_stores.BlobStoreInitialized
}

func makeBlobStoreEnvBase(
	envLocal env_local.Env,
) (BlobStoreEnv, bool) {
	env := BlobStoreEnv{
		Env: envLocal,
	}

	xdg := envLocal.GetXDGForBlobStores()

	var err error

	if strings.HasSuffix(xdg.UtilityName, "-cache") {
		env.BlobStore, err = directory_layout.MakeBlobStoreCache(xdg)
	} else {
		env.BlobStore, err = directory_layout.MakeBlobStore(xdg)
	}

	if err != nil {
		envLocal.Cancel(err)
		return env, false
	}

	return env, true
}

func MakeBlobStoreEnv(
	envLocal env_local.Env,
) BlobStoreEnv {
	env, ok := makeBlobStoreEnvBase(envLocal)
	if !ok {
		return env
	}

	env.setupStores()

	return env
}

// MakeBlobStoreEnvWithoutStores returns a BlobStoreEnv with the directory
// layout wired up but no blob stores discovered or initialized. Use this from
// commands that operate on the on-disk layout directly and must not trigger
// blob store discovery (e.g. the legacy-config migration command, which needs
// to run before discovery would succeed).
func MakeBlobStoreEnvWithoutStores(
	envLocal env_local.Env,
) BlobStoreEnv {
	env, _ := makeBlobStoreEnvBase(envLocal)
	return env
}

func MakeBlobStoreEnvWithOrder(
	envLocal env_local.Env,
	blobStoreIds []scoped_id.Id,
) BlobStoreEnv {
	env, ok := makeBlobStoreEnvBase(envLocal)
	if !ok {
		return env
	}

	env.setupStores()
	env.SetBlobStoreOrder(blobStoreIds)

	return env
}

func (env *BlobStoreEnv) setupStores() {
	env.blobStores = blob_stores.MakeBlobStores(
		env,
		env,
		env.BlobStore,
	)

	keys := slices.Collect(maps.Keys(env.blobStores))

	if len(keys) == 0 {
		return
	}

	sort.Strings(keys)
	env.defaultBlobStoreIdString = keys[0]
}

func (env *BlobStoreEnv) SetBlobStoreOrder(blobStoreIds []scoped_id.Id) {
	if len(blobStoreIds) == 0 {
		return
	}

	ids := make([]string, len(blobStoreIds))

	for i, id := range blobStoreIds {
		ids[i] = id.String()
	}

	env.orderedBlobStoreIds = ids
	env.defaultBlobStoreIdString = ids[0]
}

func (env BlobStoreEnv) GetDefaultBlobStore() blob_stores.BlobStoreInitialized {
	if len(env.blobStores) == 0 {
		panic(
			errors.Errorf(
				"calling GetDefaultBlobStore without any initialized blob stores: %#v",
				env.BlobStore,
			),
		)
	}

	return env.blobStores[env.defaultBlobStoreIdString]
}

// GetDefaultBlobStoreId returns the local id string of the store that
// GetDefaultBlobStore() resolves to (e.g. ".default"). Returns "" when
// no default has been resolved yet — GetDefaultBlobStore panics in
// that case; this method is the soft-failure peer for callers that
// want to omit a hint rather than abort.
func (env BlobStoreEnv) GetDefaultBlobStoreId() string {
	return env.defaultBlobStoreIdString
}

func (env BlobStoreEnv) GetBlobStores() blob_stores.BlobStoreMap {
	blobStores := maps.Clone(env.blobStores)
	return blobStores
}

func (env BlobStoreEnv) GetBlobStoresSorted() []blob_stores.BlobStoreInitialized {
	if env.orderedBlobStoreIds != nil {
		blobStores := make([]blob_stores.BlobStoreInitialized, 0, len(env.orderedBlobStoreIds))

		for _, id := range env.orderedBlobStoreIds {
			if bs, ok := env.blobStores[id]; ok {
				blobStores = append(blobStores, bs)
			}
		}

		return blobStores
	}

	blobStores := slices.Collect(maps.Values(env.blobStores))
	sort.Slice(blobStores, func(i, j int) bool {
		return blobStores[i].Path.GetId().Less(blobStores[j].Path.GetId())
	})
	return blobStores
}

func (env BlobStoreEnv) GetBlobStore(
	blobStoreId scoped_id.Id,
) blob_stores.BlobStoreInitialized {
	// FDR-0008 Phase 2 note: the map is keyed by the bare String()
	// form. Discovery never produces digest-bearing IDs, and String()
	// always returns the bare form even when blobStoreId carries a
	// digest. Lookup is unchanged from Phase 1.
	key := blobStoreId.String()

	blobStore, ok := env.blobStores[key]
	if !ok {
		available := slices.Collect(maps.Keys(env.blobStores))
		sort.Strings(available)

		errors.ContextCancelWithBadRequestf(
			env,
			"blob store not found: %q (available: %v)",
			key,
			available,
		)

		return blob_stores.BlobStoreInitialized{}
	}

	// FDR-0008 Phase 2: if the ID carries a digest, assert it against
	// the resolved config's Phase 1 digest. A legacy config
	// (BlobDigest is null) with an ID-supplied digest is a hard typed
	// error pointing the user at `madder config-pin_digest`.
	if blobStoreId.HasDigest() {
		configDigest := blobStore.Config.BlobDigest
		if configDigest.IsNull() {
			errors.ContextCancelWithBadRequestError(
				env,
				scoped_id.ErrIdDigestVsLegacyConfig{Id: key},
			)
			return blob_stores.BlobStoreInitialized{}
		}
		idDigest := blobStoreId.GetDigest()
		if err := markl.AssertEqual(&idDigest, &configDigest); err != nil {
			errors.ContextCancelWithBadRequestError(
				env,
				errors.Wrapf(
					err,
					"blob-store-id digest does not match resolved store %q",
					key,
				),
			)
			return blob_stores.BlobStoreInitialized{}
		}
	}

	return blobStore
}

func (env BlobStoreEnv) GetDefaultBlobStoreAndRemaining() (blob_stores.BlobStoreInitialized, blob_stores.BlobStoreMap) {
	defaultBlobStore := env.GetDefaultBlobStore()

	if env.orderedBlobStoreIds != nil {
		remaining := make(blob_stores.BlobStoreMap, len(env.orderedBlobStoreIds)-1)

		for _, id := range env.orderedBlobStoreIds[1:] {
			if bs, ok := env.blobStores[id]; ok {
				remaining[id] = bs
			}
		}

		return defaultBlobStore, remaining
	}

	remaining := env.GetBlobStores()

	maps.DeleteFunc(
		remaining,
		func(idString string, _ blob_stores.BlobStoreInitialized) bool {
			return idString == env.defaultBlobStoreIdString
		},
	)

	return defaultBlobStore, remaining
}

// OpenBlob returns a reader for the blob, searching the default store
// first and then the remaining configured stores in order, returning the
// first store that reports HasBlob. A store backend that panics while
// being asked (e.g. an unreachable SFTP store, #134) is skipped rather
// than failing the lookup, so one broken backend can't block reads of
// blobs other backends can serve. Returns a not-found error when no store
// has the blob.
func (env BlobStoreEnv) OpenBlob(
	id domain_interfaces.MarklId,
) (domain_interfaces.BlobReader, error) {
	def, remaining := env.GetDefaultBlobStoreAndRemaining()

	if reader, ok, err := tryOpenInStore(def, id); ok {
		return reader, err
	}

	for _, s := range remaining {
		if reader, ok, err := tryOpenInStore(s, id); ok {
			return reader, err
		}
	}

	return nil, errors.MakeErrNotFoundString(
		"blob not found in any blob store: " + id.String(),
	)
}

// HasBlobInAnyStore reports whether any configured store holds the blob,
// searching default-then-remaining with the same per-store panic
// tolerance as OpenBlob.
func (env BlobStoreEnv) HasBlobInAnyStore(id domain_interfaces.MarklId) bool {
	def, remaining := env.GetDefaultBlobStoreAndRemaining()

	if storeHasBlob(def, id) {
		return true
	}

	for _, s := range remaining {
		if storeHasBlob(s, id) {
			return true
		}
	}

	return false
}

// tryOpenInStore checks one store and returns a reader if it has the
// blob. A per-store panic is converted to a skip (ok=false, no error) so
// callers iterate cleanly regardless of a misbehaving backend.
func tryOpenInStore(
	store blob_stores.BlobStoreInitialized,
	id domain_interfaces.MarklId,
) (reader domain_interfaces.BlobReader, ok bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
			reader = nil
			err = nil
		}
	}()

	if !store.HasBlob(id) {
		return nil, false, nil
	}

	reader, err = store.MakeBlobReader(id)
	return reader, true, err
}

// storeHasBlob is the existence-only peer of tryOpenInStore: a per-store
// panic is treated as "couldn't ask this store" and yields false.
func storeHasBlob(
	store blob_stores.BlobStoreInitialized,
	id domain_interfaces.MarklId,
) (has bool) {
	defer func() {
		if r := recover(); r != nil {
			has = false
		}
	}()

	return store.HasBlob(id)
}
