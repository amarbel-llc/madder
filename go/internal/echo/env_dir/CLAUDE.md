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
- `RelativePath`
- `ErrTempAlreadyExists`

The location-only `RepoId` selector was removed under FDR-0019 (June
2026); its role is subsumed by `scoped_id.Id` (name + scope + cwd-depth),
which `MakeDefaultAndInitialize` now takes directly.

`Config.RepoName` (madder#240): when set, the env's blob-store XDG nests
under `repos/<name>/`, giving a named FDR-0019 repo an isolated blob pool.
The nest (`nestForRepo`) is applied as the final step in every blob-XDG
accessor — `GetXDGForBlobStores`, `GetXDGForBlobStoresWithoutOverride`,
`GetXDGForBlobStoresWithOverridePath`, `GetXDGForBlobStoreId` — because
the dewey XDG clones (`CloneWithUtilityName`/`CloneWithoutOverride`/
`CloneWithOverridePath`) re-derive every category dir and would discard
an `ActualValue` suffix. Empty `RepoName` → unchanged shared layout.
Phase 2 is blob-only; metadata nesting stays with the caller (dodder)
until the Option-2 consolidation. When env_dir moves upstream to dewey,
this nesting is a candidate to fold into dewey's XDG so clones preserve
it natively.

## Features

- XDG directory resolution (with CWD-override and dotenv variants)
- Temporary file management (per-pid tmp under XDG_CACHE_HOME)
- Path resolution and rel/abs helpers
- Directory creation with permissions
- Blob-publish audit observer wiring (per ADR 0004)
- Verify-on-collision override (per ADR 0003 / #31)
