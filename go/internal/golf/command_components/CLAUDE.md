# command_components

Environment factory for madder commands with blob store support.

## Purpose

Creates blob-store-enabled environments for madder-family utility
commands (madder, madder-cache, cutting-garden) and for wrapper
utilities that need to operate against another utility's blob stores.

## Key Types

- `EnvBlobStore`: Factory for creating blob-store-bearing environments.
  `BlobStoreXDGScope` selects which XDG scope blob stores live under
  (empty → calling utility's own scope; non-empty → an explicit other
  scope, e.g. cutting-garden setting it to `"madder"`).

## Key Functions

- `MakeEnvDirForScope(req, xdgScope)`: build a bare env_dir at any
  XDG scope, sharing only ctx + debug.Options with the BlobStoreEnv.
  Use this from wrapper-utility commands that need a SECOND env_dir
  alongside the BlobStoreEnv — e.g. cutting-garden writing a
  cg-scoped audit log under `$XDG_STATE_HOME/cutting-garden/`
  alongside madder-scoped blob writes.

## Multi-scope pattern

A wrapper utility (cutting-garden today; potentially others) holds
TWO env_dirs at once:

  - The BlobStoreEnv's env_local (carries the wrapped utility's
    env_dir — e.g. madder for blob ops).
  - A bare env_dir at the wrapper's own scope (for wrapper-only
    state — config, logs, temp).

`env_local.Env` deliberately stays single-scope; the asymmetry is
visible at the command level. Both env_dirs share `errors.Context`
and `debug.Options` but address disjoint XDG paths by construction
— see `env_dir.TestMakeDefault_DistinctScopesAreIndependent`.

## Features

- Creates environments with blob store access
- Configures directory layout and UI for madder commands
- Inherits standard environment setup with added blob store capabilities
- Supports multi-scope wrapper utilities via `MakeEnvDirForScope`
