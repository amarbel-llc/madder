package blob_stores

import (
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

// resolveMultiRef looks a reference up in the map by its bare key and
// asserts the reference's digest against the resolved store's Phase-1
// config digest. References arrive already parsed and validated as
// digest-bearing by decode-time Validate() (Task 2/3), so this does
// not re-parse or re-check the format.
//
// Returns ErrMultiRefNotReady when the named store exists but has not
// been built yet (BlobStore == nil) — the construction loop uses this
// to defer. Returns a hard error for a dangling ref (name not in the
// map), a legacy/undigested target, or a digest mismatch.
func resolveMultiRef(
	refId scoped_id.Id,
	blobStores BlobStoreMap,
) (BlobStoreInitialized, error) {
	resolved, ok := blobStores[refId.String()]
	if !ok {
		return BlobStoreInitialized{}, errors.BadRequestf(
			"multi store references %q which is not present in any "+
				"configured XDG scope", refId.Canonical(),
		)
	}
	if resolved.BlobStore == nil {
		return BlobStoreInitialized{}, ErrMultiRefNotReady{Ref: refId.Canonical()}
	}

	configDigest := resolved.Config.BlobDigest
	if configDigest.IsNull() {
		return BlobStoreInitialized{}, errors.BadRequestf(
			"multi reference %q targets an unmigrated config (no "+
				"digest); run `madder config-pin_digest %s` first",
			refId.Canonical(), refId.String(),
		)
	}

	idDigest := refId.GetDigest()
	if err := markl.AssertEqual(&idDigest, &configDigest); err != nil {
		return BlobStoreInitialized{}, errors.Wrapf(err,
			"multi reference %q digest does not match resolved store",
			refId.Canonical())
	}

	return resolved, nil
}

// makeMultiStore builds a Multi from a ConfigMulti and a populated
// store map. Every reference must already be built; resolveMultiRef
// surfaces ErrMultiRefNotReady otherwise so the caller can defer.
func makeMultiStore(
	ctx interfaces.ActiveContext,
	config blob_store_configs.ConfigMulti,
	blobStores BlobStoreMap,
) (store domain_interfaces.BlobStore, err error) {
	builder := NewMulti(ctx)

	switch config.GetMode() {
	case "mirror":
		refs := config.GetMirrorStores()
		if len(refs) == 0 {
			return store, errors.BadRequestf(
				"multi mirror mode requires at least one mirror-store",
			)
		}
		mirrors := make([]BlobStoreInitialized, 0, len(refs))
		for _, refId := range refs {
			resolved, e := resolveMultiRef(refId, blobStores)
			if e != nil {
				return store, e
			}
			mirrors = append(mirrors, resolved)
		}
		builder = builder.Mirror(mirrors...)

	case "write_through":
		writeId := config.GetWriteStore()
		if writeId.IsEmpty() {
			return store, errors.BadRequestf(
				"multi write_through mode requires a write-store",
			)
		}
		writeStore, e := resolveMultiRef(writeId, blobStores)
		if e != nil {
			return store, e
		}
		builder = builder.WriteTo(writeStore)

		reads := make([]BlobStoreInitialized, 0, len(config.GetReadStores()))
		for _, refId := range config.GetReadStores() {
			resolved, e := resolveMultiRef(refId, blobStores)
			if e != nil {
				return store, e
			}
			reads = append(reads, resolved)
		}
		builder = builder.Read(reads...).ReadFill(config.GetReadFill())

	default:
		return store, errors.BadRequestf(
			"multi store has invalid mode %q (want mirror or "+
				"write_through)", config.GetMode(),
		)
	}

	built, err := builder.Build()
	if err != nil {
		return store, errors.Wrap(err)
	}

	return built, nil
}

// buildMultiStores materializes every ConfigMulti in blobStores in
// dependency order. Each iteration builds any multi whose references
// are all resolved; it loops until an iteration makes no progress.
// Because digest-bearing references form a Merkle DAG, a no-progress
// iteration with unbuilt multis remaining means a dangling reference
// (cycles are unrepresentable). The aggregated deferral errors name
// the offending references.
func buildMultiStores(
	ctx interfaces.ActiveContext,
	blobStores BlobStoreMap,
) error {
	for {
		progressed := false
		var deferred []error

		for key := range blobStores {
			blobStore := blobStores[key]
			if blobStore.BlobStore != nil {
				continue
			}
			config, isMulti := blobStore.Config.Blob.(blob_store_configs.ConfigMulti)
			if !isMulti {
				continue
			}

			built, err := makeMultiStore(ctx, config, blobStores)
			if err != nil {
				if errors.Is(err, ErrMultiRefNotReady{}) {
					deferred = append(deferred, errors.Wrapf(err,
						"multi %q", key))
					continue
				}
				return errors.Wrapf(err, "multi %q", key)
			}

			blobStore.BlobStore = built
			blobStores[key] = blobStore
			progressed = true
		}

		if len(deferred) == 0 {
			return nil // all multis built
		}
		if !progressed {
			// No multi advanced this pass and some remain unbuilt.
			// Non-multi leaves are always built before this runs (the
			// pass-1/2 construction in MakeBlobStores cancels — and
			// thus panics — on any leaf-build failure), and
			// digest-bearing references make cycles unrepresentable.
			// So the only remaining cause is a dangling reference (a
			// ref to a name not present in the map, transitively). The
			// aggregated deferral errors name the offenders.
			return errors.Join(deferred...)
		}
	}
}

// ErrMultiRefNotReady signals that a referenced store exists in the
// map but has not been built yet. The store-map construction loop
// treats it as "defer to the next iteration", not a hard failure.
type ErrMultiRefNotReady struct {
	Ref string
}

func (e ErrMultiRefNotReady) Error() string {
	return "multi reference not yet built: " + e.Ref
}

func (e ErrMultiRefNotReady) Is(target error) bool {
	_, ok := target.(ErrMultiRefNotReady)
	return ok
}
