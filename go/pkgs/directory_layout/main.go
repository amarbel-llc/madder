package directory_layout

import (
	internal "github.com/amarbel-llc/madder/go/internal/bravo/directory_layout"
)

type (
	XDG           = internal.XDG
	Common        = internal.Common
	BlobStore     = internal.BlobStore
	Repo          = internal.Repo
	Mutable       = internal.Mutable
	RepoMutable   = internal.RepoMutable
	BlobStorePath = internal.BlobStorePath
)

const FileNameBlobStoreConfig = internal.FileNameBlobStoreConfig

var (
	MakeRepo              = internal.MakeRepo
	MakeBlobStore         = internal.MakeBlobStore
	CloneBlobStoreWithXDG = internal.CloneBlobStoreWithXDG

	GetBlobStoreConfigPaths       = internal.GetBlobStoreConfigPaths
	PathBlobStore                 = internal.PathBlobStore
	DirBlobStore                  = internal.DirBlobStore
	MakeBlobStorePath             = internal.MakeBlobStorePath
	GetDefaultBlobStore           = internal.GetDefaultBlobStore
	GetBlobStorePath              = internal.GetBlobStorePath
	GetBlobStorePathForCustomPath = internal.GetBlobStorePathForCustomPath
)
