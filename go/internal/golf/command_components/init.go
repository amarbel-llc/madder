package command_components

import (
	"io"
	"path/filepath"

	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/bravo/directory_layout"
	"github.com/amarbel-llc/madder/go/internal/charlie/files"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

type Init struct{}

func (cmd Init) InitBlobStore(
	ctx interfaces.ActiveContext,
	envBlobStore BlobStoreEnv,
	id blob_store_id.Id,
	config *blob_store_configs.TypedConfig,
) (path directory_layout.BlobStorePath) {
	var layout directory_layout.BlobStore = envBlobStore

	if id.GetLocationType() == blob_store_id.LocationTypeCwd {
		xdgForCwd := envBlobStore.GetXDGForBlobStores().CloneWithOverridePath(
			envBlobStore.GetCwd(),
		)

		var err error

		if layout, err = directory_layout.CloneBlobStoreWithXDG(
			envBlobStore,
			xdgForCwd,
		); err != nil {
			err = errors.Wrap(err)
			envBlobStore.Cancel(err)
			return path
		}
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
			_, err := blob_store_configs.Coder.EncodeTo(config, w)
			return err
		},
	); err != nil {
		envBlobStore.Cancel(err)
		return path
	}

	return path
}
