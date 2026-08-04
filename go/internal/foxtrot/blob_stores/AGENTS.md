# blob_stores

Factory and management layer for content-addressable blob storage backends.

## Key Types

- `BlobStoreInitialized`: Combines blob store config with initialized BlobStore interface
- `BlobStoreMap`: Map of blob store ID strings to initialized stores
- `CopyResult`: Result of blob copy operation with state tracking

## Key Functions

- `MakeBlobStores`: Creates all blob stores from directory layout and
  config. Also discovers XDG-system (`//name`) stores under the env's
  fixed system root and merges them into the map under their `//name`
  keys (madder#230 increment 2), gated on
  `env_dir.Env.GetXDGForSystemBlobStores` reporting a configured
  `SystemRoot`
- `MakeBlobStore`: Factory for individual blob stores (local, SFTP, pointer)
- `CopyBlobIfNecessary`: Smart blob copying with existence checking
- `MakeRemoteBlobStore`: Creates remote blob store from config

## Features

- Supports local hash-bucketed and remote SFTP blob stores
- Pointer-based blob store references for indirection
- Multi-store management with XDG override support
- Copy verification and state tracking

## Resolution & degradation notes

- A `multi` store's CWD-scoped member refs (`.name`, `..name`) resolve
  relative to the multi **config's own** discovered cwd-depth, not the
  process cwd (`resolveMultiRef` offsets via `scoped_id.Id.ResolveFrom`).
  So an ancestor multi discovered at `..notes` resolves its `.local`
  member to `..local`. See FDR-0010 "Config-relative resolution".
- A store discovered during `MakeBlobStores` that fails to **build** is
  skipped with a diagnostic and its error stashed on
  `BlobStoreInitialized.BuildErr` (backend left nil), instead of aborting
  the whole map — one broken/foreign ancestor store can't kill an
  unrelated repo's construction. The error surfaces as a hard failure
  only when the store is explicitly addressed
  (`blob_store_env.GetBlobStore` / `SetBlobStoreOrder`). Decode/discovery
  failures (legacy configs) still hard-abort.
