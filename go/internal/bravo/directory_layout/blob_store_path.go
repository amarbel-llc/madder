package directory_layout

import (
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
)

type BlobStorePath interface {
	GetBase() string
	GetConfig() string
	GetId() scoped_id.Id
}

type blobStorePath struct {
	base   string
	config string
	id     scoped_id.Id
}

var _ BlobStorePath = blobStorePath{}

func MakeBlobStorePath(id scoped_id.Id, base, config string) blobStorePath {
	return blobStorePath{
		id:     id,
		base:   base,
		config: config,
	}
}

func (path blobStorePath) GetBase() string {
	return path.base
}

func (path blobStorePath) GetConfig() string {
	return path.config
}

func (path blobStorePath) GetId() scoped_id.Id {
	return path.id
}

func GetDefaultBlobStore(
	directoryLayout BlobStore,
) BlobStorePath {
	return GetBlobStorePath(
		directoryLayout,
		"default",
	)
}

func getBlobStorePath(
	directoryLayout BlobStore,
	idString string,
) BlobStorePath {
	return MakeBlobStorePath(
		scoped_id.MakeWithLocation(
			idString,
			directoryLayout.GetLocationType(),
		),
		DirBlobStore(directoryLayout, idString),
		DirBlobStore(directoryLayout, idString, FileNameBlobStoreConfig),
	)
}

func GetBlobStorePath(
	directoryLayout BlobStore,
	idString string,
) BlobStorePath {
	return getBlobStorePath(
		directoryLayout,
		idString,
	)
}

func GetBlobStorePathForCustomPath(
	idString,
	basePath string,
	configPath string,
) BlobStorePath {
	return MakeBlobStorePath(
		scoped_id.MakeWithLocation(
			idString,
			scoped_id.LocationTypeUnknown,
		),
		basePath,
		configPath,
	)
}
