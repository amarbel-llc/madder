package command_components

import (
	"os"
	"path/filepath"

	"code.linenisgreat.com/madder/go/internal/0/ids"
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/bravo/directory_layout"
	"code.linenisgreat.com/madder/go/internal/charlie/store_key"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_stores"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
)

// MakeSealedKeyConfig turns the flag-populated local config into a
// sealed-key (TomlV5) config for a store sealed to the recipients of a
// pigpen (FDR 0011). pigpenKind is one of store_key.SourceKind*; pigpen is
// a file path, or for the papi kind an identity domain. It mints the store
// key and returns the config to write plus the sidecar that must be
// written next to it.
//
// Nothing is written here, so a bad pigpen fails before any store exists.
func MakeSealedKeyConfig(
	local *blob_store_configs.DefaultType,
	pigpenKind string,
	pigpen string,
) (
	typedConfig *blob_store_configs.TypedConfig,
	sidecar []byte,
	err error,
) {
	if len(local.Encryption) > 0 {
		err = errors.BadRequestf(
			"-pigpen cannot be combined with -encryption: a sealed-key " +
				"store mints its own key and seals it to the pigpen's recipients",
		)

		return typedConfig, sidecar, err
	}

	var source store_key.Source

	if source, err = store_key.MakeSource(pigpenKind, pigpen); err != nil {
		err = errors.BadRequest(err)
		return typedConfig, sidecar, err
	}

	var recipients []markl.Id

	// Live: the key is about to be sealed to this answer, so it must be
	// the pigpen as it is now, not a cached copy.
	if recipients, err = source.Recipients(true); err != nil {
		err = errors.BadRequest(err)
		return typedConfig, sidecar, err
	}

	var public markl.Id
	var sealed []byte

	if public, sealed, err = store_key.Mint(recipients); err != nil {
		err = errors.Wrap(err)
		return typedConfig, sidecar, err
	}

	var storeKey blob_store_configs.StoreKey

	if storeKey, err = blob_store_configs.MakeStoreKey(
		source,
		recipients,
		sealed,
	); err != nil {
		err = errors.Wrap(err)
		return typedConfig, sidecar, err
	}

	if sidecar, err = blob_store_configs.EncodeStoreKey(storeKey); err != nil {
		err = errors.Wrap(err)
		return typedConfig, sidecar, err
	}

	typedConfig = &blob_store_configs.TypedConfig{
		Type: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigV5).TypeStruct,
		Blob: &blob_store_configs.TomlV5{
			HashBuckets:       local.HashBuckets,
			BasePath:          local.BasePath,
			HashTypeId:        local.HashTypeId,
			Encryption:        []markl.Id{public},
			CompressionType:   local.CompressionType,
			VerifyOnCollision: local.VerifyOnCollision,
			SingleHash:        local.SingleHash,
			KeyCustody: blob_store_configs.KeyCustody{
				Holder: blob_store_configs.KeyHolderProcess,
			},
		},
	}

	return typedConfig, sidecar, err
}

// InitSealedKeyBlobStore creates a local sealed-key store: the sidecar
// first, then the config. In that order an interrupted init leaves a
// stray sidecar and no store, which a re-run overwrites; the reverse
// would leave a store whose key is gone.
func (cmd Init) InitSealedKeyBlobStore(
	ctx interfaces.ActiveContext,
	envBlobStore BlobStoreEnv,
	id scoped_id.Id,
	typedConfig *blob_store_configs.TypedConfig,
	sidecar []byte,
) (path directory_layout.BlobStorePath) {
	var ok bool
	if path, ok = cmd.ResolveBlobStorePath(envBlobStore, id); !ok {
		return path
	}

	// A store that already exists keeps its key: InitBlobStore below will
	// refuse the config write, and it must do so before the sidecar that
	// opens the existing blobs is replaced.
	if _, err := os.Stat(path.GetConfig()); err == nil {
		envBlobStore.Cancel(errors.BadRequestf(
			"blob store %q already exists at %s",
			id,
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

	if err := blob_stores.WriteLocalStoreKeySidecar(
		filepath.Join(path.GetBase(), directory_layout.FileNameBlobStoreKey),
		sidecar,
	); err != nil {
		envBlobStore.Cancel(err)
		return path
	}

	return cmd.InitBlobStore(ctx, envBlobStore, id, typedConfig)
}
