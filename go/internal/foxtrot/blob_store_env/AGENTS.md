# blob_store_env

Composition layer that bundles `env_local.Env` (which is `env_ui.Env +
env_dir.Env`) with a `directory_layout.BlobStore` and the discovered
`blob_stores.BlobStoreMap`. Provides default-store, ordering, and
lookup helpers that command code (madder's, dodder's, future
wrappers') typically wants on top of the raw blob_stores primitives.

Extracted from `internal/golf/command_components/env_repo.go` in May
2026 so external consumers (dodder today; future wrappers) can use it
via the dagnabit-generated `pkgs/blob_store_env` facade without
having to import madder's CLI-specific `command_components`.
command_components keeps a type alias (`BlobStoreEnv =
blob_store_env.BlobStoreEnv`) so madder's existing internal call
sites continue to work unchanged.

## Key types

- `BlobStoreEnv` — embedded `env_local.Env` + `directory_layout.BlobStore`,
  plus a discovered `BlobStoreMap` and default-store-id machinery.
- `Make` constructors: `MakeBlobStoreEnv` (with discovery),
  `MakeBlobStoreEnvWithoutStores` (skip discovery), and
  `MakeBlobStoreEnvWithOrder` (with explicit ordering).

## Layering

Sits at foxtrot, alongside `blob_stores`, `mmap_blob`, `env_local`.
Imports only from layers ≤ foxtrot. command_components (golf) imports
this; nothing imports back the other way.

## Consumers

- `internal/golf/command_components` (madder's CLI mixin layer)
- External: dodder consumes via `pkgs/blob_store_env` + `pkgs/env_local`
  to drop their own near-verbatim copies of these types
