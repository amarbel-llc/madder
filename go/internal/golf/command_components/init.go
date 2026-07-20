package command_components

import (
	"io"
	"path/filepath"

	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/bravo/directory_layout"
	"code.linenisgreat.com/madder/go/internal/charlie/files"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

type Init struct{}

// ResolveBlobStorePath resolves and validates the on-disk config path for a
// blob-store-id WITHOUT creating anything. The id's location prefix selects
// the XDG scope (blob-store(7)), so the layout is re-derived from the id
// rather than taken from the env as-is: a Cwd id roots in the *current* dir,
// a system id uses v3System (madder#230), and an XDG-user id drops any
// ancestor `.<utility>/` walk-up override (the pre-#227 bug, where such a
// store was created inside the override and discovery/`write` could never
// resolve it). Shared with `serve --store` via layoutForId so both resolve a
// store to the SAME path.
//
// A scope the layout cannot represent is rejected (#230) rather than silently
// retargeted, since the registered id's location comes from the layout — a
// mismatch would create the store under a DIFFERENT id than the user named.
// Under the user-data layout this covers '%' (XDG cache — madder-cache(1)),
// '/' (remote-first), and '_' (Unknown — root comes from configuration).
//
// ok=false means the env was cancelled (bad id/scope); the caller should
// return. Used by InitBlobStore, EnsureBlobStoreVerbatim, and the
// --if-not-exists existence check in the command layer.
func (cmd Init) ResolveBlobStorePath(
	envBlobStore BlobStoreEnv,
	id scoped_id.Id,
) (path directory_layout.BlobStorePath, ok bool) {
	layout, err := layoutForId(envBlobStore, id)
	if err != nil {
		envBlobStore.Cancel(errors.Wrap(err))
		return path, false
	}

	if id.GetLocationType() != layout.GetLocationType() {
		envBlobStore.Cancel(errors.BadRequestf(
			"blob-store-id %q selects the %v scope, which this "+
				"utility's store layout (%v) cannot represent; "+
				"see blob-store(7) for scope prefixes (#230)",
			id,
			id.GetLocationType(),
			layout.GetLocationType(),
		))
		return path, false
	}

	return directory_layout.GetBlobStorePath(layout, id.GetName()), true
}

func (cmd Init) InitBlobStore(
	ctx interfaces.ActiveContext,
	envBlobStore BlobStoreEnv,
	id scoped_id.Id,
	config *blob_store_configs.TypedConfig,
) (path directory_layout.BlobStorePath) {
	var ok bool
	if path, ok = cmd.ResolveBlobStorePath(envBlobStore, id); !ok {
		return path
	}

	if err := envBlobStore.MakeDirs(
		filepath.Dir(path.GetBase()),
		filepath.Dir(path.GetConfig()),
	); err != nil {
		envBlobStore.Cancel(err)
		return path
	}

	// Per ADR 0005 / #65, blob_store-config is immutable per store
	// identity: write read-only via the atomic tmp+chmod+rename helper.
	if err := files.WriteImmutable(
		path.GetConfig(),
		func(w io.Writer) error {
			_, err := blob_store_configs.EncodeWithDigest(config, w)
			return err
		},
	); err != nil {
		envBlobStore.Cancel(err)
		return path
	}

	return path
}

// EnsureBlobStoreVerbatim idempotently installs a digest-pinned
// blob_store-config artifact (from `config-gen`) at id's path, keyed by
// digest:
//
//   - absent  → write configBytes VERBATIM. Verbatim (not re-encode) is
//     load-bearing: EncodeWithDigest is non-deterministic across calls for
//     some config types (randomized encryption keys in inventory_archive
//     variants — see delta/blob_store_configs/digest.go), so re-encoding a
//     decoded artifact could shift the on-disk digest away from
//     intendedDigest.
//   - present → assert the on-disk config's digest equals intendedDigest; a
//     match is an idempotent no-op, a mismatch is a hard drift error.
//
// intendedDigest is the artifact's own BlobDigest, which the caller has
// already asserted against any id `@digest` pin. The verbatim write makes
// on-disk digest == artifact digest == pin by construction. Used by the
// pinned path of `init-from`.
func (cmd Init) EnsureBlobStoreVerbatim(
	ctx interfaces.ActiveContext,
	envBlobStore BlobStoreEnv,
	id scoped_id.Id,
	configBytes []byte,
	intendedDigest markl.Id,
) (path directory_layout.BlobStorePath) {
	var ok bool
	if path, ok = cmd.ResolveBlobStorePath(envBlobStore, id); !ok {
		return path
	}

	if existing, err := blob_store_configs.DecodeAndVerifyFromFile(
		path.GetConfig(),
	); err == nil {
		// Already installed: idempotent iff it is the same config by
		// digest, otherwise a genuine drift conflict.
		if assertErr := markl.AssertEqual(
			&existing.BlobDigest,
			&intendedDigest,
		); assertErr != nil {
			envBlobStore.Cancel(errors.BadRequestf(
				"blob store %q already exists with a different config: "+
					"on-disk digest %s does not match requested %s",
				id,
				existing.BlobDigest,
				intendedDigest,
			))
		}

		return path
	} else if !errors.IsNotExist(err) {
		envBlobStore.Cancel(errors.Wrapf(
			err,
			"reading existing blob store config at %q",
			path.GetConfig(),
		))
		return path
	}

	if err := envBlobStore.MakeDirs(
		filepath.Dir(path.GetBase()),
		filepath.Dir(path.GetConfig()),
	); err != nil {
		envBlobStore.Cancel(err)
		return path
	}

	// Verbatim, immutable write: the artifact is already a validated,
	// digest-stamped blob_store-config; re-encoding it is pointless and
	// (for some config types) digest-shifting.
	if err := files.WriteImmutable(
		path.GetConfig(),
		func(w io.Writer) error {
			_, err := w.Write(configBytes)
			return err
		},
	); err != nil {
		envBlobStore.Cancel(err)
		return path
	}

	return path
}
