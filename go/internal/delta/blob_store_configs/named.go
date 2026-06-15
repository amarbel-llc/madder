package blob_store_configs

import (
	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/madder/go/internal/bravo/directory_layout"
)

type ConfigNamed struct {
	Path   directory_layout.BlobStorePath
	Config TypedConfig
}

func (configNamed ConfigNamed) GetId() scoped_id.Id {
	return configNamed.Path.GetId()
}
