package blob_stores

//go:generate dagnabit export

import (
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/bravo/directory_layout"
	"code.linenisgreat.com/madder/go/internal/charlie/fd"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/madder/go/internal/echo/env_dir"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/ui"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/xdg"
	"golang.org/x/crypto/ssh"
)

var defaultBuckets = []int{2}

type BlobStoreMap = map[string]BlobStoreInitialized

func MakeBlobStoreMap(blobStores ...BlobStoreInitialized) BlobStoreMap {
	output := make(BlobStoreMap, len(blobStores))

	for _, blobStore := range blobStores {
		blobStoreIdString := blobStore.Path.GetId().String()
		output[blobStoreIdString] = blobStore
	}

	return output
}

func makeBlobStoreConfigs(
	ctx interfaces.ActiveContext,
	directoryLayout directory_layout.BlobStore,
) BlobStoreMap {
	configPaths := directory_layout.GetBlobStoreConfigPaths(
		ctx,
		directoryLayout,
	)

	blobStores := make(BlobStoreMap, len(configPaths))

	for _, configPath := range configPaths {
		blobStorePath := directory_layout.GetBlobStorePath(
			directoryLayout,
			fd.DirBaseOnly(configPath),
		)

		blobStoreIdString := blobStorePath.GetId().String()
		blobStore := blobStores[blobStoreIdString]
		blobStore.Path = blobStorePath

		if typedConfig, err := blob_store_configs.DecodeAndVerifyFromFile(
			configPath,
		); err != nil {
			ctx.Cancel(errors.Wrapf(
				err,
				"blob store %q at %q",
				blobStoreIdString,
				configPath,
			))
			return blobStores
		} else {
			blobStore.Config = typedConfig
		}

		blobStores[blobStoreIdString] = blobStore
	}

	return blobStores
}

// Caller is responsible for the IsOverridden() guard — invoking this
// from non-override mode would walk past nothing.
func makeAncestorOverrideStores(
	ctx interfaces.ActiveContext,
	envDir env_dir.Env,
	directoryLayout directory_layout.BlobStore,
) BlobStoreMap {
	utilityName := envDir.GetXDG().UtilityName
	ceilings := xdg.ParseCeilingDirectories(
		os.Getenv(xdg.CeilingEnvVarName(utilityName)),
	)

	ancestors := directory_layout.FindAllCwdOverridePaths(
		envDir.GetCwd(),
		utilityName,
		ceilings,
	)

	blobStores := make(BlobStoreMap)
	nameRank := make(map[string]uint)

	for i, ancestor := range ancestors {
		var layoutForAncestor directory_layout.BlobStore

		if i == 0 {
			// Deepest ancestor — the env's existing layout already
			// resolves there. Reuse rather than re-clone.
			layoutForAncestor = directoryLayout
		} else {
			cloned, err := directory_layout.CloneBlobStoreWithXDG(
				directoryLayout,
				envDir.GetXDGForBlobStoresWithOverridePath(ancestor),
			)
			if err != nil {
				ctx.Cancel(err)
				return blobStores
			}
			layoutForAncestor = cloned
		}

		storesAtAncestor := makeBlobStoreConfigs(ctx, layoutForAncestor)

		for _, store := range storesAtAncestor {
			name := store.Path.GetId().GetName()
			depth := nameRank[name]
			nameRank[name]++

			taggedId := store.Path.GetId().WithCwdDepth(depth)
			store.Path = directory_layout.MakeBlobStorePath(
				taggedId,
				store.Path.GetBase(),
				store.Path.GetConfig(),
			)
			blobStores[taggedId.String()] = store
		}
	}

	return blobStores
}

// TODO pass in custom UI context for printing
// TODO consolidated envDir and ctx arguments
func MakeBlobStores(
	ctx interfaces.ActiveContext,
	envDir env_dir.Env,
	directoryLayout directory_layout.BlobStore,
) (blobStores BlobStoreMap) {
	if envDir.GetXDG().IsOverridden() {
		blobStores = makeAncestorOverrideStores(ctx, envDir, directoryLayout)

		// User-XDG entries are non-`.`-prefixed, disjoint from the
		// Cwd entries above — the merge below cannot collide.
		if directoryLayoutForUser, err := directory_layout.CloneBlobStoreWithXDG(
			directoryLayout,
			envDir.GetXDGForBlobStoresWithoutOverride(),
		); err != nil {
			ctx.Cancel(err)
			return blobStores
		} else {
			blobStoresForXDG := makeBlobStoreConfigs(ctx, directoryLayoutForUser)
			maps.Insert(blobStores, maps.All(blobStoresForXDG))
		}
	} else {
		blobStores = makeBlobStoreConfigs(ctx, directoryLayout)
	}

	// madder#230 increment 2: also discover XDG-system (`//name`) stores
	// under the fixed system root, keyed by their `//name` id. This is
	// global — independent of the cwd ancestor walk-up above and never
	// tagged with cwd-depth — and `//`-prefixed keys are disjoint from the
	// user (unprefixed) and cwd (`.name`) entries, so the merge can't
	// collide. ok is false when no system root is configured (e.g. a
	// non-madder env), in which case the glob is skipped entirely rather
	// than mis-keying user stores as system. A missing system dir globs to
	// nothing, so this is a no-op on hosts without /var/lib/madder.
	if systemXDG, ok := envDir.GetXDGForSystemBlobStores(); ok {
		if systemLayout, err := directory_layout.MakeBlobStoreSystem(
			systemXDG,
		); err != nil {
			ctx.Cancel(err)
			return blobStores
		} else {
			blobStoresForSystem := makeBlobStoreConfigs(ctx, systemLayout)
			maps.Insert(blobStores, maps.All(blobStoresForSystem))
		}
	}

	// Two-pass initialization: first pass creates stores that have no
	// cross-references (e.g. local hash-bucketed, SFTP), second pass creates
	// stores that depend on other stores (e.g. inventory archives that
	// reference a loose blob store). This is necessary because Go map
	// iteration order is non-deterministic, and inventory archive stores
	// need their referenced loose blob store to be initialized first.
	for blobStoreIdString := range blobStores {
		blobStore := blobStores[blobStoreIdString]

		if _, needsCrossRef := blobStore.Config.Blob.(blob_store_configs.ConfigInventoryArchive); needsCrossRef {
			continue
		}
		if _, isMulti := blobStore.Config.Blob.(blob_store_configs.ConfigMulti); isMulti {
			continue
		}

		var err error

		if blobStore.BlobStore, err = MakeBlobStore(
			envDir,
			blobStore.ConfigNamed,
			blobStores,
		); err != nil {
			ctx.Cancel(err)
			return blobStores
		}

		blobStores[blobStoreIdString] = blobStore
	}

	for blobStoreIdString := range blobStores {
		blobStore := blobStores[blobStoreIdString]

		if blobStore.BlobStore != nil {
			continue
		}
		if _, isMulti := blobStore.Config.Blob.(blob_store_configs.ConfigMulti); isMulti {
			continue
		}

		var err error

		if blobStore.BlobStore, err = MakeBlobStore(
			envDir,
			blobStore.ConfigNamed,
			blobStores,
		); err != nil {
			ctx.Cancel(err)
			return blobStores
		}

		blobStores[blobStoreIdString] = blobStore
	}

	if err := buildMultiStores(ctx, blobStores); err != nil {
		ctx.Cancel(err)
		return blobStores
	}

	return blobStores
}

func MakeRemoteBlobStore(
	envDir env_dir.Env,
	configNamed blob_store_configs.ConfigNamed,
) (blobStore BlobStoreInitialized) {
	blobStore.ConfigNamed = configNamed

	{
		var err error

		if blobStore.BlobStore, err = MakeBlobStore(
			envDir,
			configNamed,
			nil,
		); err != nil {
			envDir.GetActiveContext().Cancel(err)
			return blobStore
		}
	}

	return blobStore
}

// uiErrEnv is the narrow slice of env_ui.Env that blob-store
// construction sniffs for: a per-env err sink that honors env_ui's
// CustomErr / UIFileIsStderr options. env_local.Env and
// blob_store_env.BlobStoreEnv both satisfy it when they pass
// themselves to MakeBlobStore; a bare env_dir.Env does not, and
// chatter falls back to the process-global stderr printer. See #228.
type uiErrEnv interface {
	GetErr() fd.Std
}

// storeChatterPrinter resolves the base printer for blob-store and
// ssh-helper chatter (lazy SFTP dial/host-key/remote-config lines)
// from the env, preferring the env's own err sink over the ui.Err()
// global so consumers can redirect or silence store chatter per-env
// (#228). With no custom sink configured the env's sink IS stderr,
// so default behavior is unchanged.
func storeChatterPrinter(envDir env_dir.Env) ui.Printer {
	if envUI, ok := envDir.(uiErrEnv); ok {
		return envUI.GetErr()
	}

	return ui.Err()
}

// NOTE: blobStores parameter added to support inventory archive's
// loose-blob-store-id resolution. This couples MakeBlobStore to the
// store map, which may not scale well if more store types need
// cross-references. If this becomes a problem, switch to two-pass
// initialization: first pass creates all stores without cross-refs,
// second pass wires them up.
//
// TODO describe base path agnostically
func MakeBlobStore(
	envDir env_dir.Env,
	configNamed blob_store_configs.ConfigNamed,
	blobStores BlobStoreMap,
) (store domain_interfaces.BlobStore, err error) {
	// Errors from the construction helpers (hash type validation, SFTP
	// connection, archive open, pointer resolution) are otherwise opaque
	// — a bare "unsupported hash type" tells the caller nothing about
	// which blob-store-id or config file is misconfigured.
	defer func() {
		if err != nil {
			err = errors.Wrapf(
				err,
				"blob store %q at %q",
				configNamed.Path.GetId(),
				configNamed.Path.GetConfig(),
			)
		}
	}()

	printer := ui.MakePrefixPrinter(
		storeChatterPrinter(envDir),
		fmt.Sprintf("# (blob_store: %s) ", configNamed.Path.GetId()),
	)

	configBlob := configNamed.Config.Blob

	switch config := configBlob.(type) {
	case blob_store_configs.ConfigSFTPUri:
		return makeSftpStore(
			envDir.GetActiveContext(),
			printer,
			configNamed.GetId(),
			config,
			func() (*ssh.Client, error) {
				return MakeSSHClientFromSSHConfig(
					envDir.GetActiveContext(),
					printer,
					config,
				)
			},
			envDir.GetBlobWriteObserver(),
		)

	case blob_store_configs.ConfigSFTPConfigExplicit:
		return makeSftpStore(
			envDir.GetActiveContext(),
			printer,
			configNamed.GetId(),
			config,
			func() (*ssh.Client, error) {
				return MakeSSHClientForExplicitConfig(
					envDir.GetActiveContext(),
					printer,
					config,
				)
			},
			envDir.GetBlobWriteObserver(),
		)

	case blob_store_configs.ConfigWebDAV:
		return makeWebdavStore(
			envDir.GetActiveContext(),
			printer,
			configNamed.GetId(),
			config,
			func() (*http.Client, error) {
				return MakeHTTPClientForWebDAVConfig(
					envDir.GetActiveContext(),
					printer,
					config,
				)
			},
			envDir.GetBlobWriteObserver(),
		)

	case blob_store_configs.ConfigS3:
		return makeS3Store(
			envDir.GetActiveContext(),
			printer,
			configNamed.GetId(),
			config,
			envDir.GetBlobWriteObserver(),
		)

	case blob_store_configs.ConfigLocalHashBucketed:
		return makeLocalHashBucketed(
			envDir,
			configNamed.GetId(),
			configNamed.Path.GetBase(),
			config,
		)

	case blob_store_configs.ConfigInventoryArchiveDelta:
		var looseBlobStore domain_interfaces.BlobStore

		if config.GetLooseBlobStoreId().IsEmpty() {
			loosePath := filepath.Join(configNamed.Path.GetBase(), "blobs")

			embeddedConfig := &blob_store_configs.DefaultType{
				HashTypeId:      blob_store_configs.HashType(config.GetDefaultHashTypeId()),
				HashBuckets:     blob_store_configs.DefaultHashBuckets,
				CompressionType: config.GetCompressionRef(),
			}

			if looseBlobStore, err = makeLocalHashBucketed(
				envDir,
				configNamed.GetId(),
				loosePath,
				embeddedConfig,
			); err != nil {
				return store, err
			}
		} else if blobStores != nil {
			looseBlobStoreId := config.GetLooseBlobStoreId().String()
			if initialized, ok := blobStores[looseBlobStoreId]; ok {
				looseBlobStore = initialized.BlobStore
			}
		}

		if looseBlobStore == nil {
			err = errors.BadRequestf(
				"inventory archive store requires loose-blob-store-id %q but it was not found",
				config.GetLooseBlobStoreId(),
			)
			return store, err
		}

		return makeInventoryArchiveV1(
			envDir,
			configNamed.Path.GetBase(),
			configNamed.Path.GetId(),
			config,
			looseBlobStore,
		)

	case blob_store_configs.ConfigInventoryArchive:
		var looseBlobStore domain_interfaces.BlobStore

		if blobStores != nil {
			looseBlobStoreId := config.GetLooseBlobStoreId().String()
			if initialized, ok := blobStores[looseBlobStoreId]; ok {
				looseBlobStore = initialized.BlobStore
			}
		}

		if looseBlobStore == nil {
			err = errors.BadRequestf(
				"inventory archive store requires loose-blob-store-id %q but it was not found",
				config.GetLooseBlobStoreId(),
			)
			return store, err
		}

		return makeInventoryArchiveV0(
			envDir,
			configNamed.Path.GetBase(),
			configNamed.Path.GetId(),
			config,
			looseBlobStore,
		)

	case blob_store_configs.ConfigPointer:
		var typedConfig blob_store_configs.TypedConfig
		otherStorePath := config.GetPath()

		if typedConfig, err = blob_store_configs.DecodeAndVerifyFromFile(
			otherStorePath.GetConfig(),
		); err != nil {
			err = errors.Wrapf(
				err,
				"pointer target at %q",
				otherStorePath.GetConfig(),
			)
			return store, err
		}

		configNamed.Config = typedConfig
		configNamed.Path = directory_layout.MakeBlobStorePath(
			configNamed.Path.GetId(),
			otherStorePath.GetBase(),
			otherStorePath.GetConfig(),
		)

		return MakeBlobStore(envDir, configNamed, blobStores)

	case blob_store_configs.ConfigMulti:
		return makeMultiStore(
			envDir.GetActiveContext(),
			config,
			blobStores,
		)

	default:
		err = errors.BadRequestf(
			"unsupported blob store type %q:%T",
			configBlob.GetBlobStoreType(),
			configBlob,
		)

		return store, err
	}
}
