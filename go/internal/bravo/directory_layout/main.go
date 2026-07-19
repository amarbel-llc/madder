package directory_layout

//go:generate dagnabit export

import (
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/xdg"
)

type (
	XDG = xdg.XDG

	Common interface {
		scoped_id.LocationTypeGetter
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

func MakeBlobStoreCache(
	xdg XDG,
) (BlobStore, error) {
	var blobStore blobStoreUninitialized = &v3Cache{}

	if err := blobStore.initialize(xdg); err != nil {
		err = errors.Wrap(err)
		return nil, err
	}

	return blobStore, nil
}

// MakeBlobStoreSystem builds the XDG-system (`//name`) layout (madder#230).
// The xdg must already be rooted at the system path (env_dir.rootAtSystem);
// the layout reports LocationTypeXDGSystem so init/discovery accept `//name`
// ids against it.
func MakeBlobStoreSystem(
	xdg XDG,
) (BlobStore, error) {
	var blobStore blobStoreUninitialized = &v3System{}

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
