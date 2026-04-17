package directory_layout

//go:generate dagnabit export

import (
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/xdg"
)

type (
	XDG = xdg.XDG

	Common interface {
		blob_store_id.LocationTypeGetter
		cloneUninitialized() uninitializedXDG
	}

	BlobStore interface {
		Common
		MakePathBlobStore(...string) interfaces.DirectoryLayoutPath
	}
)

type (
	uninitializedXDG interface {
		BlobStore
		initialize(XDG) error
	}

	blobStoreUninitialized interface {
		BlobStore
		uninitializedXDG
	}
)

func MakeBlobStore(
	xdg XDG,
) (BlobStore, error) {
	var blobStore blobStoreUninitialized = &v3{}

	if err := blobStore.initialize(xdg); err != nil {
		err = errors.Wrap(err)
		return nil, err
	}

	return blobStore, nil
}

func CloneBlobStoreWithXDG(layout BlobStore, xdg XDG) (BlobStore, error) {
	clone := layout.cloneUninitialized()

	if err := clone.initialize(xdg); err != nil {
		err = errors.Wrap(err)
		return nil, err
	}

	return clone, nil
}
