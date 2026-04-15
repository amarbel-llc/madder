package command_components_madder

import (
	"path/filepath"

	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/bravo/directory_layout"
	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
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

	if err := hyphence.EncodeToFile(
		blob_store_configs.Coder,
		config,
		path.GetConfig(),
	); err != nil {
		envBlobStore.Cancel(err)
		return path
	}

	return path
}
