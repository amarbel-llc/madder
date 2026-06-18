# command_components

Madder's CLI-flavored composition layer. Wires `futility` commands
into env_dir, env_local, env_ui, blob_store_env, and the inventory-log
observer. Stays internal — its `EnvBlobStore` mixin couples to
`futility.Request` and the inventory-log wiring is madder-specific.
External consumers (dodder, future wrappers) compose their own
equivalents over the public substrate (`pkgs/env_dir`,
`pkgs/madder_env`, `pkgs/blob_store_env`, `pkgs/env_local`,
`pkgs/env_ui`).

## Purpose

Glues madder-family utility commands (madder, madder-cache,
madder-mcp) onto the env_dir substrate.

## Key Types

- `EnvBlobStore`: futility-command mixin that produces a
  `BlobStoreEnv` per request. `BlobStoreXDGScope` selects which XDG
  scope blob stores live under (empty → calling utility's own scope;
  non-empty → an explicit other scope, e.g. madder-mcp setting it to
  `"madder"` so the MCP server reads from madder's stores).
- `BlobStoreEnv`: type alias for
  `internal/foxtrot/blob_store_env.BlobStoreEnv`. Internal call sites
  reference `command_components.BlobStoreEnv`; the canonical type
  lives one layer down (and is exposed via `pkgs/blob_store_env` for
  external consumers).

## Multi-scope pattern (external consumers)

A wrapper utility (e.g. `amarbel-llc/cutting-garden`) that holds two
env_dirs at once — its own scope for wrapper-local state plus madder's
scope for blob ops — composes against the public pkgs facades:
`pkgs/env_dir.MakeDefault` for the env_dir constructions,
`pkgs/madder_env.DefaultEnvVarNames` for honoring madder's env-var
contract on the madder-scoped env_dir, and
`pkgs/blob_store_env.MakeBlobStoreEnv` for the discovery+default-store
machinery. See `docs/plans/2026-05-03-env-dir-multi-scope.md` and the
`#123` resolution comment.

## Features

- Creates environments with blob store access
- Configures directory layout and UI for madder commands
- Inherits standard environment setup with added blob store capabilities
- Wires the inventory-log `BlobWriteObserver` onto the BlobStoreEnv's
  env_dir per request (gated by the `--no-inventory-log` flag and
  `MADDER_INVENTORY_LOG=0` env var)
