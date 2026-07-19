package blob_store_configs

import (
	"path/filepath"

	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/bravo/directory_layout"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

//go:generate tommy generate
type TomlPointerV1 struct {
	BasePath string `toml:"base-path"`
}

func (TomlPointerV1) GetBlobStoreType() string {
	return "local-pointer-v1"
}

func (blobStoreConfig *TomlPointerV1) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.StringVar(
		&blobStoreConfig.BasePath,
		"base-path",
		"",
		"absolute path to another blob store base directory",
	)
}

func (blobStoreConfig TomlPointerV1) GetPath() directory_layout.BlobStorePath {
	return directory_layout.MakeBlobStorePath(
		scoped_id.Id{},
		blobStoreConfig.BasePath,
		filepath.Join(
			blobStoreConfig.BasePath,
			directory_layout.FileNameBlobStoreConfig,
		),
	)
}
