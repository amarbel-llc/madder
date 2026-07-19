package command_components

import "code.linenisgreat.com/madder/go/internal/foxtrot/blob_store_env"

// BlobStoreEnv is the composition layer that bundles env_local.Env +
// directory_layout.BlobStore + the discovered BlobStoreMap. The
// canonical definition lives in `internal/foxtrot/blob_store_env`
// (exposed via `pkgs/blob_store_env`); this alias preserves madder's
// internal call sites that reference `command_components.BlobStoreEnv`
// without churn. New external consumers should import the new package
// directly.
type BlobStoreEnv = blob_store_env.BlobStoreEnv
