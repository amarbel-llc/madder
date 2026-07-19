# madder

Content-addressable blob storage CLI.

Madder stores opaque byte streams ("blobs") addressed by the
cryptographic digest of their content. You write bytes and get back a
self-describing **markl-id**; you read bytes by handing that id back.
The same content always produces the same id, so storage is
deduplicated and concurrent writes are safe without coordination.

Madder ships one Go module (`code.linenisgreat.com/madder/go`) that
builds the `madder` CLI plus the sibling binaries `madder-cache` and
`madder-mcp`, and `mad`, a short alias for `madder` itself (mirroring
dodder's `der`). It was extracted from
[dodder](https://github.com/amarbel-llc/dodder) in April 2026 and is
consumed as a library by both dodder and
[cutting-garden](https://github.com/amarbel-llc/cutting-garden) via the
public `go/pkgs/` surface.

## Philosophy

Madder is not a graph. From outside the store, every blob is atomic
and fully resolved: `MakeBlobReader(id)` returns the decoded bytes,
the consumer never names a sidecar, and the markl-id is the only
handle a caller ever needs.

Inside the store, plugins MAY use sidecar data (trained dicts,
encryption keys, compression state) to deliver that surface — and
they own the mechanics of fetching, sync-transfer, and lifecycle for
their own data. Self-containedness is an API contract, not a
byte-layout claim.

What madder still doesn't do:

- Expose a graph or relationship layer at the API surface. Tools
  like dodder and cutting-garden compose blobs into graphs above
  madder; the store itself doesn't know about those edges.
- Allow consumers to assemble blobs from foreign references. Every
  decode must be resolvable inside the store the blob lives in,
  using only that store's plugin layer. Cross-store reads require
  sync, not link-following.

When a feature would surface relationships between blobs to
external consumers — references, joins, queries — that feature
belongs in a layer above madder.

## Concepts

- **Blob** — an opaque byte stream stored by content. Encoding
  (compression, encryption) is a property of the store, applied
  transparently on write and reversed on read.
- **markl-id** — a self-describing, checksummed identifier of the form
  `[purpose@]format-data`, e.g.
  `blake2b256-9ft3m74l5t2ppwjrvfg3wp380jqj2zfrm6zevxqx34sdethvey0s5vm9gd`.
  The payload is [blech32](https://en.bitcoin.it/wiki/Bech32)-encoded
  (bech32 with a `-` separator). madder supports `sha256` and
  `blake2b256` digests. See `markl-id(7)`.
- **Blob store** — a content-addressable backend. Several backends
  exist (see below); the default is a local hash-bucketed directory
  tree similar to Git's object store. See `blob-store(7)`.
- **blob-store-id** — a store's address: an optional scope prefix plus
  a name, e.g. `default`, `.archive`, `%scratch`. The prefix selects
  the XDG location:

  | Prefix | Scope | Location |
  |--------|-------|----------|
  | (none) | XDG user | `$XDG_DATA_HOME/madder/blob_stores/` |
  | `.`    | CWD-relative | `$PWD/.madder/local/share/blob_stores/` |
  | `/`    | XDG system | system-wide XDG data dirs |
  | `%`    | XDG cache (purgeable) | `$XDG_CACHE_HOME/madder-cache/blob_stores/` |
  | `_`    | config-determined | resolved from the store's config |

  An optional `@<markl-id>` suffix pins the expected on-disk config
  digest (see Config integrity). See `blob-store(7)`.
- **hyphence** — the `---`-fenced text format madder uses for
  `blob_store-config` files and the inventory log, now consumed as a
  library from [amarbel-llc/hyphence](https://github.com/amarbel-llc/hyphence).
  See `hyphence(7)`.

## Install & build

The build entrypoint is the justfile (see `eng(7)`):

```sh
just build      # nix build → result/bin/{mad,madder,madder-cache,madder-mcp}
just build-go   # plain `go build` of the module
just test       # build + vet analyzers + go tests + bats lanes (also `just`)
```

`version.env` (`MADDER_VERSION`) is the single source of truth for the
release version; `madder version` prints the built version and commit.

## Quick start

```sh
# Create the default (XDG user) blob store.
madder init default

# Write a file; capture the digest from TAP output.
hash=$(madder write -format tap ./notes.md | awk '/^ok/ {print $4}')

# Read it back by digest.
madder cat "$hash"

# Stream bytes from stdin and parse the digest out of NDJSON.
printf 'hello' | madder write -format json - | jq -r '.id'

# Check existence, list stores, list every blob.
madder has "$hash"
madder list
madder cat-ids default
```

Several commands accept inline store switching: a positional argument
that parses as a blob-store-id switches the active store for the
arguments that follow, e.g.
`madder write file1.txt .archive file2.txt` writes `file1.txt` to the
default store and `file2.txt` to `.archive`.

## Commands

Full per-command detail (flags, exit codes, examples) lives in the
generated `madder(1)` man page — run `man madder`, or regenerate with
`just debug-gen_man madder.1`.

**Initialize stores**

| Command | Description |
|---------|-------------|
| `init` | initialize a local blob store |
| `init-inventory-archive` | initialize an inventory archive store (`-v1`, `-v0` variants exist) |
| `init-sftp-explicit` / `init-sftp-ssh_config` | initialize an SFTP store (explicit creds / from `~/.ssh/config`) |
| `init-webdav` | initialize a WebDAV store |
| `init-s3` | initialize an S3 / S3-compatible store |
| `init-pointer` (`init-pointer-v0`) | initialize a pointer store that delegates to another |
| `init-from` | initialize a store from an existing config file |

**Read & write blobs**

| Command | Description |
|---------|-------------|
| `write` | write file(s)/stdin to a store, print digests |
| `read` | read blobs from JSON objects on stdin (programmatic `write`) |
| `cat` | output blob contents by digest |
| `has` | check whether blobs exist |
| `cat-ids` | list all blob digests in a store |
| `encode-ids` | convert hex digests to native markl IDs |

**Inventory archives & packing**

| Command | Description |
|---------|-------------|
| `pack` | pack loose blobs into archive files |
| `pack-blobs` | write files and pack them into an archive |
| `pack-list` | list archive files in inventory archive stores |
| `pack-cat-ids` | list blob digests contained in archive files |

**Manage stores**

| Command | Description |
|---------|-------------|
| `list` | list configured blob stores |
| `info-repo` | display blob store configuration |
| `sync` | synchronize blobs between stores |
| `fsck` | verify blob store integrity |
| `config-pin_digest` | mint or refresh the `@` digest line on `blob_store-config` files |
| `sftp-analyze-and-suggest-configs` | analyze a legacy SFTP store and suggest config candidates |
| `version` / `complete` | print build version / shell completion |

## Store types

All store types present the same `BlobStore` surface; they differ only
in where and how bytes are persisted. See `blob-store(7)` for the full
specification (concurrency, durability, on-disk layout).

- **Local hash-bucketed** — the default. Loose files in a digest-prefix
  directory tree. Crash-safe via temp-file + `link(2)` + `fsync(2)`;
  published blobs are read-only (`0444`).
- **Inventory archive** — packs many small blobs into indexed archive
  files with O(1) fan-out lookup (format versions v0/v1/v2).
- **SFTP** — remote store over SSH/SFTP.
- **WebDAV** — remote store over HTTP/HTTPS WebDAV (Nextcloud, `mod_dav`,
  `rclone serve webdav`, …).
- **S3** — Amazon S3 or any S3-compatible object store (MinIO, Ceph RGW,
  R2, B2, …).
- **Pointer** — delegates reads/writes to another store by reference.
- **Multi** — an in-process composition primitive (mirror, or
  write-through with optional read-fill cache). Today it is a Go library
  type; a config-file wrapper is designed in FDR-0009. See
  `blob-store-multi(7)`.

## Config integrity

Each `blob_store-config` carries an `@ <markl-id>` line recording a
`blake2b256` digest of the config body. On read, madder recomputes and
refuses a config whose body has drifted. Legacy configs without the line
are trusted silently; `madder list` flags them `(unmigrated)` and prints
a copy-pasteable `madder config-pin_digest` command to migrate. A
blob-store-id may additionally carry an `@<digest>` suffix that pins the
expected config digest at resolve time. See FDR-0008 and `blob-store(7)`.

## Sibling binaries

- **mad** — a short alias for `madder` itself (same command tree, same
  XDG scope and man pages), mirroring dodder's `der`.
- **madder-cache** — manages purgeable `%`-prefixed cache stores under
  `$XDG_CACHE_HOME/madder-cache/`. Subset of the madder surface
  (`init`, `list`, `write`, `has`, `cat`, `fsck`, `version`).
- **madder-mcp** — `madder-mcp serve` runs an MCP server over stdio,
  exposing madder operations as tools.
The `hyphence` format CLI (`validate`/`format`/`meta`/`body`) moved to its
own repo, [amarbel-llc/hyphence](https://github.com/amarbel-llc/hyphence),
and is no longer built by madder (madder#253).

## Library use

External Go programs embed madder via the public `go/pkgs/` packages
rather than the internal tree. The documented consumer substrate
(used by cutting-garden) includes `blob_store_env`, `env_dir`,
`madder_env`, `arg_resolver`, `output_format`, and `tap_diagnostics`;
`domain_interfaces` defines the core `BlobStore` contracts. The
per-blob audit log is exposed via the `inventory_log` facade —
see `madder-inventory-log(7)` for wiring patterns. Breaking changes to
`pkgs/` are coordinated with downstream consumers.

## Environment

| Variable | Effect |
|----------|--------|
| `XDG_DATA_HOME` / `XDG_CACHE_HOME` / `XDG_LOG_HOME` | base dirs for user stores, cache stores, and the inventory log (default `$HOME/.local/share`, `$HOME/.cache`, `$HOME/.local/log`) |
| `MADDER_INVENTORY_LOG=0` | suppress the per-blob audit log (same as `--no-inventory-log`) |
| `MADDER_CEILING_DIRECTORIES` | ceiling for the upward `.madder/` walk (mirrors `GIT_CEILING_DIRECTORIES`) |
| `MADDER_XDG_USER_LOCATION_ONLY` | disable the cwd walk-up; use standard XDG resolution only |
| `MADDER_VERIFY_ON_COLLISION=1` | byte-compare on digest collision for the current run (local stores; see `blob-store(7)`) |

## Documentation

**Man pages** (`docs/man.7/`): `blob-store(7)`, `blob-store-multi(7)`,
`markl-id(7)`, `hyphence(7)`, `madder-inventory-log(7)`. The
command-reference pages `madder(1)` / `madder-cache(1)` are generated
from the CLI definitions (`just debug-gen_man`).

**RFCs** (`docs/rfcs/`) — `markl-id` format (0002) is madder's own
normative wire format. The hyphence format (0001) and capture/restore
rules (0003) are redirect stubs to their own repos (`amarbel-llc/hyphence`
and `amarbel-llc/cutting-garden`).

**ADRs** (`docs/decisions/`) — accepted architecture decisions
(0001–0007), e.g. content-addressed overwrite semantics and `link(2)`
publish.

**Feature design records** (`docs/features/`) — grouped by status, so a
design-space or extracted-feature record is not mistaken for current
madder behavior:

- *Shipped in madder* — 0004 blob-encoding plugins, 0008 config digest
  pins, 0009 multi-store config type (primitive shipped; config wrapper
  in progress).
- *Design exploration only* — 0002 remote inventory-archive packing,
  0005 plugin-id build orchestration.
- *Superseded* — 0003 zstd dictionary hints (folded into 0004).
- *cutting-garden (external repo)* — 0001 restore, 0002 diff, 0005
  URI-scheme plugins describe the cutting-garden CLI, which was
  extracted from this repo and now consumes madder as a library.

## History

Madder was extracted from **dodder** (an immutable cryptographic object
graph inspired by Git, Nix, and Zettelkasten) so the blob-store layer
could be built and released on its own cadence; the two are now peers.
Some `dodder-*` strings remain as intentional wire-format identifiers —
see `AGENTS.md` for how to read them. The filesystem-tree capture/restore
tool **cutting-garden** was likewise extracted (madder#216, May 2026) and
now consumes madder's public `pkgs/` surface. Extraction details live in
`docs/plans/`.
