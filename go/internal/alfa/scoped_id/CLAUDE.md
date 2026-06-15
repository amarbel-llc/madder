# scoped_id

Identifier type for a location-prefixed, optionally-named addressable
root — a madder **blob store** or a dodder **repo**. Renamed from
`blob_store_id` under FDR-0019 so the same parser serves both consumers;
dodder addresses repos through the `pkgs/scoped_id` facade rather than
mirroring the grammar. `pkgs/blob_store_id` remains as a deprecated alias
for one release.

## Key Types

- `Id`: scoped identifier — location + name + cwd-depth + optional digest
  (+ the FDR-0019 `remoteFirst` system-scope spelling marker)
- `LocationType`: enum for location types (XDG user, CWD, system, etc.)

## Features

- Text marshaling/unmarshaling support (wire-format via `Canonical`)
- Location-based prefixing for ID strings

## ID Format

A scoped ID has an optional location prefix followed by a name string.
Unprefixed IDs default to XDG user location.

| Prefix     | Location / scope          | Example     | Filesystem root                              |
|------------|---------------------------|-------------|----------------------------------------------|
| *(none)*   | XDG user                  | `default`   | `$XDG_DATA_HOME/madder/blob_stores/`         |
| `.`/`..`   | CWD (+ dot-depth)         | `.archive`  | `$PWD/.madder/local/share/blob_stores/`      |
| `//`       | XDG system                | `//system`  | system data dir                              |
| `/`        | remote-first (FDR-0019)   | `/origin`   | remote, else system-scoped `name`            |
| `_`        | Unknown                   | `_custom`   | (custom path)                                |
| `%`        | XDG cache                 | `%scratch`  | `$XDG_CACHE_HOME/madder-cache/blob_stores/`  |
| `~`        | *(backward compat)*       | `~default`  | same as unprefixed (parsed as XDG user)      |

FDR-0019 split the single-slash system prefix: `//name` forces the system
scope, while `/name` is *remote-first* — a consuming repo resolves a
remote named `name` first, falling back to the system-scoped `name`. Both
parse to `LocationTypeXDGSystem`; `IsRemoteFirst()` distinguishes them.
madder has no remote transport, so it resolves both to the system scope
and ignores the marker; dodder reads it. `//name` was previously rejected
by the name charset, so adopting it is purely additive.

`Set()` enforces the name charset `[a-zA-Z0-9_-]+` (blob-store(7)); it
rejects anything else (#227 — path-shaped values used to parse and then
mangle init's store paths). Direct construction via `Make`/
`MakeWithLocation` is not validated. `String()` omits the prefix for XDG
user IDs and renders system as `//name` (or `/name` when remote-first);
`Canonical()` (the wire form) collapses cwd-depth to a single dot (#145)
and appends `@<markl-id>` when a digest is pinned (FDR-0008). `~` is
accepted on parse but never emitted.

Two IDs with the same name but different locations (e.g. `default` vs
`.default`) refer to **different roots** at different filesystem
locations. CWD stores (`.` prefix) resolve relative to `$PWD`.
