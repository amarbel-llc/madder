package blob_store_configs

import (
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/bravo/directory_layout"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
)

//go:generate tommy generate
type TomlPointerV0 struct {
	Id         scoped_id.Id `toml:"id"`
	BasePath   string       `toml:"base-path"`
	ConfigPath string       `toml:"config-path"`
}

func (TomlPointerV0) GetBlobStoreType() string {
	return "local-pointer"
}

func (blobStoreConfig *TomlPointerV0) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.Var(
		&blobStoreConfig.Id,
		"id",
		"another blob store's id",
	)

	flagSet.StringVar(
		&blobStoreConfig.BasePath,
		"base-path",
		"",
		"path to another blob store base directory",
	)

	flagSet.StringVar(
		&blobStoreConfig.ConfigPath,
		"config-path",
		"",
		"path to another blob store config file",
	)
}

func (blobStoreConfig TomlPointerV0) GetPath() directory_layout.BlobStorePath {
	return directory_layout.MakeBlobStorePath(
		blobStoreConfig.Id,
		blobStoreConfig.BasePath,
		blobStoreConfig.ConfigPath,
	)
}
