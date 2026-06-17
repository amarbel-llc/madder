package directory_layout

import (
	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

// v3System is the XDG-system (`//name`) blob-store layout (madder#230). It
// is structurally identical to v3 — paths nest under the XDG Data dir's
// `blob_stores/` — but hard-codes GetLocationType() to XDGSystem rather
// than deriving it from xdg.IsOverridden() (which only distinguishes
// Cwd vs XDGUser). The system root is baked into the XDG's category
// ActualValues by env_dir.rootAtSystem before this layout sees it, so a
// system store resolves to `<system-root>/blob_stores/<name>` (e.g.
// `/var/lib/madder/blob_stores/shared`).
type v3System struct {
	xdg XDG
}

func (layout *v3System) initialize(
	xdg XDG,
) (err error) {
	layout.xdg = xdg
	return err
}

func (layout v3System) MakePathBlobStore(
	targets ...string,
) interfaces.DirectoryLayoutPath {
	return layout.xdg.GetDirData().MakePath(
		stringSliceJoin("blob_stores", targets)...,
	)
}

func (layout v3System) GetLocationType() scoped_id.LocationType {
	return scoped_id.LocationTypeXDGSystem
}

func (layout v3System) cloneUninitialized() uninitializedXDG {
	return &layout
}
