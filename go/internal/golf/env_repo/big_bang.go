package env_repo

import (
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/bravo/ids"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/delta/genesis_configs"
)

// Config used to initialize a repo for the first time
type BigBang struct {
	GenesisConfig        *genesis_configs.TypedConfigPrivateMutable
	TypedBlobStoreConfig *blob_store_configs.TypedMutableConfig

	InventoryListType ids.TypeStruct

	PrivateKey markl.Id

	Yin                       string
	Yang                      string
	ExcludeDefaultType        bool
	ExcludeDefaultConfig      bool
	IncludeDefaultPandocTools bool
	BlobStoreId               blob_store_id.Id
}

func (bigBang *BigBang) SetDefaults() {
	bigBang.GenesisConfig = genesis_configs.Default()
	bigBang.InventoryListType = ids.GetOrPanic(
		ids.TypeInventoryListVCurrent,
	).TypeStruct

	bigBang.TypedBlobStoreConfig = blob_store_configs.Default()
}
