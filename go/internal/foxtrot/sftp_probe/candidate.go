package sftp_probe

import (
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_io"
)

// Candidate is one (compression, encryption) hypothesis about a
// blob store's encoding. StoreConfig is the on-disk form (suitable
// for hyphence-encoding into a blob_store-config file). IOConfig
// is the reader-pipeline form for verification. Label is for
// display in the TAP output and candidate filenames.
type Candidate struct {
	StoreConfig blob_store_configs.Config
	IOConfig    blob_io.Config
	Label       string
}
