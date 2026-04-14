package blob_store_id

import (
	internal "github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
)

type (
	Id                 = internal.Id
	LocationType       = internal.LocationType
	LocationTypeGetter = internal.LocationTypeGetter
)

var (
	Make             = internal.Make
	MakeWithLocation = internal.MakeWithLocation

	LocationTypeUnknown   = internal.LocationTypeUnknown
	LocationTypeCwd       = internal.LocationTypeCwd
	LocationTypeXDGUser   = internal.LocationTypeXDGUser
	LocationTypeXDGSystem = internal.LocationTypeXDGSystem
)
