package directory_layout

import (
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
)

type v3 struct {
	xdg XDG
}

func (layout *v3) initialize(
	xdg XDG,
) (err error) {
	layout.xdg = xdg
	return err
}

func (layout v3) MakePathBlobStore(
	targets ...string,
) interfaces.DirectoryLayoutPath {
	return layout.xdg.GetDirData().MakePath(
		stringSliceJoin("blob_stores", targets)...)
}

func (layout v3) GetLocationType() blob_store_id.LocationType {
	if layout.xdg.IsOverridden() {
		return blob_store_id.LocationTypeCwd
	}

	return blob_store_id.LocationTypeXDGUser
}

func (layout v3) cloneUninitialized() uninitializedXDG {
	return &layout
}
