package command_components

import (
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/bravo/directory_layout"
	"code.linenisgreat.com/madder/go/internal/charlie/fd"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_stores"
	"code.linenisgreat.com/madder/go/internal/futility"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

type BlobStore struct{}

func (cmd *BlobStore) MakeBlobStoreFromConfigPath(
	envBlobStore BlobStoreEnv,
	basePath string,
	configPath string,
) (blobStore blob_stores.BlobStoreInitialized) {
	var typedConfig blob_store_configs.TypedConfig

	{
		var err error

		if typedConfig, err = blob_store_configs.DecodeAndVerifyFromFile(
			configPath,
		); err != nil {
			envBlobStore.Cancel(errors.Wrapf(
				err,
				"blob store config at %q",
				configPath,
			))
			return blobStore
		}
	}

	blobStore.Config = typedConfig

	blobStore.Path = directory_layout.GetBlobStorePathForCustomPath(
		fd.DirBaseOnly(basePath),
		basePath,
		configPath,
	)

	{
		var err error

		if blobStore.BlobStore, err = blob_stores.MakeBlobStore(
			envBlobStore,
			blobStore.ConfigNamed,
			nil,
		); err != nil {
			envBlobStore.Cancel(err)
			return blobStore
		}
	}

	return blobStore
}

// layoutForId resolves the directory_layout for a blob-store-id, honoring
// its scope: a Cwd (`.`) id roots in the *current* dir (not the deepest
// ancestor `.<utility>/` override); a system (`//`) id uses the v3System
// layout (madder#230); every other scope clones the env's layout with the
// id's XDG (the same mapping discovery uses). Shared by init (InitBlobStore)
// and single-store open (MakeBlobStoreByScopedId) so both resolve a store
// to the SAME path.
func layoutForId(
	envBlobStore BlobStoreEnv,
	id scoped_id.Id,
) (directory_layout.BlobStore, error) {
	switch id.GetLocationType() {
	case scoped_id.LocationTypeCwd:
		return directory_layout.CloneBlobStoreWithXDG(
			envBlobStore,
			envBlobStore.GetXDGForBlobStoresWithOverridePath(
				envBlobStore.GetCwd(),
			),
		)

	case scoped_id.LocationTypeXDGSystem:
		return directory_layout.MakeBlobStoreSystem(
			envBlobStore.GetXDGForBlobStoreId(id),
		)

	default:
		return directory_layout.CloneBlobStoreWithXDG(
			envBlobStore,
			envBlobStore.GetXDGForBlobStoreId(id),
		)
	}
}

// MakeBlobStoreByScopedId opens a single blob store addressed by its
// scoped id, reading the on-disk blob_store-config WITHOUT discovery — so
// an XDG-system (`//name`) store resolves via #230's v3System even though
// system-store discovery is unbuilt (#230 inc-2). Resolves the layout
// identically to init (layoutForId), so the served store path matches what
// `madder init` created. The nil cross-ref map suits a plain local store
// (the system `//default` case); a system store that itself composes others
// (multi/inventory-archive) is out of scope. Used by `madder serve --store`.
func (cmd *BlobStore) MakeBlobStoreByScopedId(
	envBlobStore BlobStoreEnv,
	id scoped_id.Id,
) (blobStore blob_stores.BlobStoreInitialized) {
	layout, err := layoutForId(envBlobStore, id)
	if err != nil {
		envBlobStore.Cancel(errors.Wrap(err))
		return blobStore
	}

	blobStore.Path = directory_layout.GetBlobStorePath(layout, id.GetName())

	{
		var err error

		if blobStore.Config, err = blob_store_configs.DecodeAndVerifyFromFile(
			blobStore.Path.GetConfig(),
		); err != nil {
			envBlobStore.Cancel(errors.Wrapf(
				err,
				"blob store %q config at %q",
				id,
				blobStore.Path.GetConfig(),
			))
			return blobStore
		}
	}

	{
		var err error

		if blobStore.BlobStore, err = blob_stores.MakeBlobStore(
			envBlobStore,
			blobStore.ConfigNamed,
			nil,
		); err != nil {
			envBlobStore.Cancel(err)
			return blobStore
		}
	}

	return blobStore
}

func (cmd *BlobStore) MakeBlobStoreFromIdOrConfigPath(
	envBlobStore BlobStoreEnv,
	basePath string,
	blobStoreIndexOrConfigPath string,
) (blobStore blob_stores.BlobStoreInitialized) {
	if blobStoreIndexOrConfigPath == "" {
		goto tryDefaultBlobStore
	}

	{
		configPath := blobStoreIndexOrConfigPath
		var typedConfig blob_store_configs.TypedConfig

		{
			var err error

			if typedConfig, err = blob_store_configs.DecodeAndVerifyFromFile(
				configPath,
			); err != nil {
				if errors.IsNotExist(err) {
					err = nil
					goto tryBlobStoreId
				} else {
					envBlobStore.Cancel(errors.Wrapf(
						err,
						"blob store config at %q",
						configPath,
					))
					return blobStore
				}
			}
		}

		blobStore.Config = typedConfig

		blobStore.Path = directory_layout.GetBlobStorePathForCustomPath(
			blobStoreIndexOrConfigPath,
			basePath,
			blobStoreIndexOrConfigPath,
		)

		{
			var err error

			if blobStore.BlobStore, err = blob_stores.MakeBlobStore(
				envBlobStore,
				blobStore.ConfigNamed,
				nil,
			); err != nil {
				envBlobStore.Cancel(err)
				return blobStore
			}
		}

		return blobStore
	}

tryBlobStoreId:
	return cmd.MakeBlobStoreFromIdString(envBlobStore, blobStoreIndexOrConfigPath)

tryDefaultBlobStore:
	return envBlobStore.GetDefaultBlobStore()
}

func (cmd *BlobStore) MakeBlobStoreFromIdString(
	envBlobStore BlobStoreEnv,
	blobStoreIdString string,
) (blobStore blob_stores.BlobStoreInitialized) {
	var blobStoreId scoped_id.Id

	if err := blobStoreId.Set(blobStoreIdString); err != nil {
		envBlobStore.Cancel(err)
		return blobStore
	}

	return envBlobStore.GetBlobStore(blobStoreId)
}

// BlobStoreIds returns every configured blob-store-id as a slice, suitable
// for passing to arg_resolver.DetectShadow.
func BlobStoreIds(m blob_stores.BlobStoreMap) []scoped_id.Id {
	ids := make([]scoped_id.Id, 0, len(m))
	for _, s := range m {
		ids = append(ids, s.Path.GetId())
	}
	return ids
}

func (cmd BlobStore) MakeBlobStoresFromIdsOrAll(
	req futility.Request,
	envBlobStore BlobStoreEnv,
) blob_stores.BlobStoreMap {
	blobStores := make(
		blob_stores.BlobStoreMap,
		req.RemainingArgCount(),
	)

	if req.RemainingArgCount() == 0 {
		return envBlobStore.GetBlobStores()
	}

	for range req.RemainingArgCount() {
		blobStoreId := futility.PopRequestArg[scoped_id.Id](
			req,
			"blob-store-id",
		)

		blobStores[blobStoreId.String()] = envBlobStore.GetBlobStore(
			*blobStoreId,
		)
	}

	return blobStores
}

func (cmd BlobStore) MakeSourceAndDestinationBlobStoresFromIdsOrAll(
	req futility.Request,
	envBlobStore BlobStoreEnv,
) (source blob_stores.BlobStoreInitialized, destinations blob_stores.BlobStoreMap) {
	destinations = make(
		blob_stores.BlobStoreMap,
		req.RemainingArgCount(),
	)

	if req.RemainingArgCount() == 0 {
		return envBlobStore.GetDefaultBlobStoreAndRemaining()
	}

	sourceBlobStoreId := futility.PopRequestArg[scoped_id.Id](
		req,
		"source blob-store-id",
	)

	source = envBlobStore.GetBlobStore(*sourceBlobStoreId)

	for range req.RemainingArgCount() {
		blobStoreId := futility.PopRequestArg[scoped_id.Id](
			req,
			"destination blob-store-id",
		)

		destinations[blobStoreId.String()] = envBlobStore.GetBlobStore(
			*blobStoreId,
		)
	}

	return
}
