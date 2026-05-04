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
cutting-garden) onto the multi-scope env_dir substrate.

## Key Types

- `EnvBlobStore`: futility-command mixin that produces a
  `BlobStoreEnv` per request. `BlobStoreXDGScope` selects which XDG
  scope blob stores live under (empty → calling utility's own scope;
  non-empty → an explicit other scope, e.g. cutting-garden setting
  it to `"madder"`).
- `BlobStoreEnv`: type alias for
  `internal/foxtrot/blob_store_env.BlobStoreEnv`. Internal call sites
  reference `command_components.BlobStoreEnv`; the canonical type
  lives one layer down (and is exposed via `pkgs/blob_store_env` for
  external consumers).

## Key Functions

- `MakeEnvDirForScope(req, xdgScope)`: build a bare env_dir at any
  XDG scope, sharing only ctx + debug.Options with the BlobStoreEnv.
  Use this from wrapper-utility commands that need a SECOND env_dir
  alongside the BlobStoreEnv — e.g. cutting-garden writing a
  cg-scoped audit log under `$XDG_STATE_HOME/cutting-garden/`
  alongside madder-scoped blob writes.

## Multi-scope pattern (madder-side)

A wrapper utility's command (cutting-garden today; potentially
others) holds TWO env_dirs at once:

  - The BlobStoreEnv's env_local (carries the wrapped utility's
    env_dir — e.g. madder for blob ops).
  - A bare env_dir at the wrapper's own scope (for wrapper-only
    state — config, logs, temp).

`env_local.Env` deliberately stays single-scope; the asymmetry is
visible at the command level. Both env_dirs share `errors.Context`
and `debug.Options` but address disjoint XDG paths by construction
— see `env_dir.TestMakeDefault_DistinctScopesAreIndependent` and
the bats `capture_writes_log_entry_at_cg_scope` test pinning the
contract end-to-end.

For external consumers, the same pattern works against the public
pkgs facades: `pkgs/env_dir.MakeDefault` for the env_dir
constructions, `pkgs/madder_env.DefaultEnvVarNames` for honoring
madder's env-var contract on the madder-scoped env_dir, and
`pkgs/blob_store_env.MakeBlobStoreEnv` for the discovery+default-store
machinery — see `docs/plans/2026-05-03-env-dir-multi-scope.md` and
the `#123` resolution comment.

## Features

- Creates environments with blob store access
- Configures directory layout and UI for madder commands
- Inherits standard environment setup with added blob store capabilities
- Supports multi-scope wrapper utilities via `MakeEnvDirForScope`
- Wires the inventory-log `BlobWriteObserver` onto the BlobStoreEnv's
  env_dir per request (gated by the `--no-inventory-log` flag and
  `MADDER_INVENTORY_LOG=0` env var)
