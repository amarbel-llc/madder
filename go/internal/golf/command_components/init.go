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
	// The id's location prefix selects the XDG scope (blob-store(7)), so
	// the layout is re-derived from the id rather than taken from the env
	// as-is: a Cwd id roots in the *current* dir, a system id uses v3System
	// (madder#230), and an XDG-user id drops any ancestor `.<utility>/`
	// walk-up override (the pre-#227 bug, where such a store was created
	// inside the override and discovery/`write` could never resolve it).
	// Shared with `serve --store` via layoutForId so both resolve a store
	// to the SAME path.
	layout, err := layoutForId(envBlobStore, id)
	if err != nil {
		envBlobStore.Cancel(errors.Wrap(err))
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
