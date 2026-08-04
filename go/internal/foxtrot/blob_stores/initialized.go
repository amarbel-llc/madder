package blob_stores

import (
	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
)

type BlobStoreInitialized struct {
	blob_store_configs.ConfigNamed
	domain_interfaces.BlobStore

	// BuildErr records why a DISCOVERED store failed to build. When it is
	// non-nil, BlobStore is nil: MakeBlobStores skipped the store with a
	// diagnostic instead of aborting the whole map, so a broken or foreign
	// ancestor store cannot kill an unrelated repo's construction. The
	// error stays latent until the store is explicitly addressed
	// (blob_store_env.GetBlobStore) or read from, at which point it is
	// surfaced as a hard error. A store the caller explicitly names still
	// fails hard (MakeRemoteBlobStore, MakeBlobStore).
	BuildErr error
}

func (blobStoreInitialized BlobStoreInitialized) GetBlobStore() domain_interfaces.BlobStore {
	return blobStoreInitialized.BlobStore
}
