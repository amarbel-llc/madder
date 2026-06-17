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

`Config.RepoName` (madder#240, #241): when set, the env nests both its
metadata XDG (via `GetXDG`, #241) and its blob-store XDG (via the blob
accessors, #240) under `repos/<name>/`, fully isolating a named FDR-0019
repo's layout. The nest (`nestForRepo`) is applied on read off the raw
`env.XDG` field in `GetXDG`, and re-applied as the final step in every
blob-XDG accessor — `GetXDGForBlobStores`,
`GetXDGForBlobStoresWithoutOverride`,
`GetXDGForBlobStoresWithOverridePath`, `GetXDGForBlobStoreId` — because
the dewey XDG clones (`CloneWithUtilityName`/`CloneWithoutOverride`/
`CloneWithOverridePath`) re-derive every category dir and would discard
an `ActualValue` suffix. The blob accessors clone off the raw field
(not via `GetXDG`), so the metadata and blob paths nest independently and
exactly once each — no double-nesting. `TempLocal` is built from the raw
field at construction and stays un-nested (per-pid ephemeral scratch has
no per-repo isolation value). Empty `RepoName` → unchanged shared layout.

#241 (Phase 2 Option 2) makes madder the single owner of the
`repos/<name>/` layout — metadata + blobs — so dodder drops its own
`NestUnderRepoName` and delegates the whole layout here. When env_dir
moves upstream to dewey, this nesting is a candidate to fold into dewey's
XDG so clones preserve it natively.

`Config.SystemRoot` (madder#230): the filesystem root an XDG-system
(`//name`) blob store resolves under. `GetXDGForBlobStoreId`'s
`LocationTypeXDGSystem` case re-roots the XDG category dirs at it via
`rootAtSystem` (the same `ActualValue`-mutation pattern as `nestForRepo`),
so a system store lands at `<SystemRoot>/blob_stores/<name>` (flat,
store-id addressed — never `repos/`-nested). Injected by the caller so
env_dir stays application-agnostic; the madder layer passes
`madder_env.DefaultSystemRoot` (`/var/lib/madder`). Empty `SystemRoot`
disables system-scope resolution (no-op). dewey has no system concept, so
`directory_layout.v3System` hard-codes `LocationTypeXDGSystem` (v3/v3Cache
derive scope from `xdg.IsOverridden`, which can't represent it).

`Config.SystemScoped` (madder#230 follow-up): when set (with `SystemRoot`),
`initializeXDG` applies `rootAtSystem` to the BASE XDG — not just the per-id
store path — so the env's per-pid `TempLocal` also lands under `SystemRoot`.
This colocates a system-store daemon's link(2) staging with the store
(EXDEV-safe; ProtectSystem-safe). Set by `madder serve --store //name` via
`command_components.MakeEnvBlobStoreSystemScoped`; plain (non-daemon)
system-store resolution doesn't need it.

## Features

- XDG directory resolution (with CWD-override and dotenv variants)
- Temporary file management (per-pid tmp under XDG_CACHE_HOME)
- Path resolution and rel/abs helpers
- Directory creation with permissions
- Blob-publish audit observer wiring (per ADR 0004)
- Verify-on-collision override (per ADR 0003 / #31)
