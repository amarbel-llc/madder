package blob_store_configs

import (
	"path/filepath"

	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/bravo/directory_layout"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
)

//go:generate tommy generate
type TomlPointerV2 struct {
	BasePath string `toml:"base-path"`

	// InstanceId is the store's uuidv7 instance identity (FDR-0010),
	// minted once at creation inside EncodeWithDigest. Empty for a legacy
	// config or one upgraded in memory from V1.
	InstanceId markl.Id `toml:"instance-id,omitempty"`
}

func (TomlPointerV2) GetBlobStoreType() string {
	return "local-pointer-v1"
}

func (blobStoreConfig *TomlPointerV2) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.StringVar(
		&blobStoreConfig.BasePath,
		"base-path",
		"",
		"absolute path to another blob store base directory",
	)
}

func (blobStoreConfig TomlPointerV2) GetPath() directory_layout.BlobStorePath {
	return directory_layout.MakeBlobStorePath(
		scoped_id.Id{},
		blobStoreConfig.BasePath,
		filepath.Join(
			blobStoreConfig.BasePath,
			directory_layout.FileNameBlobStoreConfig,
		),
	)
}

func (blobStoreConfig TomlPointerV2) GetInstanceId() markl.Id {
	return blobStoreConfig.InstanceId
}

func (blobStoreConfig *TomlPointerV2) SetInstanceId(instanceId markl.Id) {
	blobStoreConfig.InstanceId = instanceId
}
