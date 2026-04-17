package directory_layout

import (
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
)

type v3Cache struct {
	xdg XDG
}

func (layout *v3Cache) initialize(
	xdg XDG,
) (err error) {
	layout.xdg = xdg
	return err
}

func (layout v3Cache) MakePathBlobStore(
	targets ...string,
) interfaces.DirectoryLayoutPath {
	return layout.xdg.GetDirCache().MakePath(
		stringSliceJoin("blob_stores", targets)...)
}

func (layout v3Cache) GetLocationType() blob_store_id.LocationType {
	if layout.xdg.IsOverridden() {
		return blob_store_id.LocationTypeCwd
	}

	return blob_store_id.LocationTypeXDGCache
}

func (layout v3Cache) cloneUninitialized() uninitializedXDG {
	return &layout
}
