package directory_layout

import (
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
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
		stringSliceJoin("blob_stores", targets)...,
	)
}

func (layout v3) GetLocationType() scoped_id.LocationType {
	if layout.xdg.IsOverridden() {
		return scoped_id.LocationTypeCwd
	}

	return scoped_id.LocationTypeXDGUser
}

func (layout v3) cloneUninitialized() uninitializedXDG {
	return &layout
}
