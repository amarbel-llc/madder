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
