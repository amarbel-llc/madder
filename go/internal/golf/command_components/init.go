package command_components

import (
	"io"
	"path/filepath"

	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/madder/go/internal/bravo/directory_layout"
	"github.com/amarbel-llc/madder/go/internal/charlie/files"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

type Init struct{}

func (cmd Init) InitBlobStore(
	ctx interfaces.ActiveContext,
	envBlobStore BlobStoreEnv,
	id scoped_id.Id,
	config *blob_store_configs.TypedConfig,
) (path directory_layout.BlobStorePath) {
	// The id's location prefix selects the XDG scope (blob-store(7)),
	// so the layout must be re-derived from the id rather than taken
	// from the env as-is: the env's XDG may be overridden by an
	// ancestor `.<utility>/` walk-up, which is only the right root for
	// explicit Cwd ids. Before #227 an unprefixed (XDG user) id was
	// silently created inside the ancestor override — where discovery
	// (and therefore `write`) would never resolve it.
	var xdgForId directory_layout.XDG

	if id.GetLocationType() == scoped_id.LocationTypeCwd {
		// Explicit `.`-prefix: root the store in the *current*
		// directory, not the deepest ancestor override.
		xdgForId = envBlobStore.GetXDGForBlobStoresWithOverridePath(
			envBlobStore.GetCwd(),
		)
	} else {
		// Non-Cwd scopes drop any ancestor override (XDG user/cache);
		// see env_dir.GetXDGForBlobStoreId — the same mapping
		// discovery uses to resolve these ids.
		xdgForId = envBlobStore.GetXDGForBlobStoreId(id)
	}

	layout, err := directory_layout.CloneBlobStoreWithXDG(
		envBlobStore,
		xdgForId,
	)
	if err != nil {
		err = errors.Wrap(err)
		envBlobStore.Cancel(err)
		return path
	}

	// The registered id's location comes from the layout
	// (getBlobStorePath), so a scope the layout cannot represent would
	// create the store under a DIFFERENT id than the user named —
	// reject instead (#230). Under the user-data layout this covers
	// '%' (XDG cache — owned by madder-cache(1)), '/' (XDG system —
	// not implemented), and '_' (Unknown — root comes from
	// configuration, not a name).
	if id.GetLocationType() != layout.GetLocationType() {
		envBlobStore.Cancel(errors.BadRequestf(
			"blob-store-id %q selects the %v scope, which this "+
				"utility's store layout (%v) cannot represent; "+
				"see blob-store(7) for scope prefixes (#230)",
			id,
			id.GetLocationType(),
			layout.GetLocationType(),
		))
		return path
	}

	path = directory_layout.GetBlobStorePath(
		layout,
		id.GetName(),
	)

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
