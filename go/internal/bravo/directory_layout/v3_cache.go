package directory_layout

import (
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
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
		stringSliceJoin("blob_stores", targets)...,
	)
}

func (layout v3Cache) GetLocationType() scoped_id.LocationType {
	if layout.xdg.IsOverridden() {
		return scoped_id.LocationTypeCwd
	}

	return scoped_id.LocationTypeXDGCache
}

func (layout v3Cache) cloneUninitialized() uninitializedXDG {
	return &layout
}
