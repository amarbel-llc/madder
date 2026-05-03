# env_dir

XDG-scoped directory environment for madder commands. Owns
construction-time XDG resolution, temp-FS lifecycle, and the audit
toggles (`GetVerifyOnCollisionOverride`, `GetBlobWriteObserver`)
that blob stores pull off the ambient env at construction.

The blob-IO machinery (Config, Writer, Reader, Mover, collision
check, hash-bucket path helpers) was extracted to
`internal/foxtrot/blob_io` in May 2026 to free the `Config` name
in env_dir for the env-construction `Config` proposed in [#123].

## Key Types

- `Env` — interface for directory env operations (Cwd, XDG getters,
  temp-FS, common-env builders, observer/verify-override accessors)
- `EnvVarNames` — bundle of env-var names env_dir reads/writes
  (`BIN_*`, `*_XDG_UTILITY_OVERRIDE`, `*_VERIFY_ON_COLLISION`)
- `TemporaryFS` — per-process tempfs anchor (handed to blob_io.MoveOptions)
- `RepoId`, `RelativePath`
- `ErrTempAlreadyExists`

## Features

- XDG directory resolution (with CWD-override and dotenv variants)
- Temporary file management (per-pid tmp under XDG_CACHE_HOME)
- Path resolution and rel/abs helpers
- Directory creation with permissions
- Blob-publish audit observer wiring (per ADR 0004)
- Verify-on-collision override (per ADR 0003 / #31)
