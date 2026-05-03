---
status: proposed
date: 2026-05-03
promotion-criteria: |
  Promote to `experimental` once the plugin registry, V4 config
  schema, and the four built-in plugins (none, gzip, zlib, zstd)
  ship and at least one fresh store can be created and round-
  tripped under V4. Promote to `accepted` after one tagged
  release with V4 stores in user hands and no surprise
  correctness reports.
---

# Blob encoding plugins

## Problem Statement

Madder's blob stores today encode bytes through a fixed pair of
positional slots — a `compression-type` enum sourced from
`github.com/amarbel-llc/purse-first/libs/dewey/delta/compression_type`
and an `encryption` field — both burned into every store config.
Adding a new transform (zstd dictionaries, future encryption
variants, content-addressed re-coding, signing) means coordinating
a dewey release, a config schema bump, and an awkward mapping
between every store's storage backend and the dewey-side enum.
The encoding choice is structurally rigid for something that wants
to grow.

This FDR replaces the rigid enum with a plugin-based abstraction
owned by madder. Each transform — none, gzip, zlib, zstd, future
encryption codecs, future content-addressed transforms — is a
plugin satisfying a single stream-to-stream interface, registered
inside the madder binary against a content-addressed identifier.
Stores reference plugins by `<type-tag>@<builtin-plugin-id>`,
mirroring the existing hyphence `@`-lock convention. Dewey
relinquishes ownership of the compression-type enum.

## Interface

### The plugin abstraction

A plugin is a stream-to-stream transform with optional per-
instance parameters. It satisfies a single interface (Go form;
package path TBD during implementation):

```go
package plugins  // tentative; in madder

type Plugin interface {
    // WrapWriter returns a WriteCloser whose Close flushes the
    // transform's tail. Bytes written to the WriteCloser are
    // transformed and emitted to w.
    WrapWriter(w io.Writer) (io.WriteCloser, error)

    // WrapReader returns a ReadCloser that yields the original
    // bytes when the underlying reader yields the transformed
    // bytes.
    WrapReader(r io.Reader) (io.ReadCloser, error)
}

// Factory constructs a plugin instance from a configuration blob.
// For non-parametric plugins (none, gzip, zlib, plain zstd), the
// factory ignores params. Parametric plugins (a future
// zstd-with-dict; future encryption with a key) read params as
// raw bytes whose interpretation is plugin-defined.
type Factory func(params []byte) (Plugin, error)
```

`io.Reader` in, `io.Reader` out. Identical contract to today's
`interfaces.IOWrapper` but owned by madder rather than dewey.

### Plugin reference syntax

Each plugin instance is referenced by:

    <type-tag>@<builtin-plugin-id>

- **`<type-tag>`** is a stable string identifier for the plugin's
  *interface*, following madder's
  `<utility>-<artifact>-<subtype>-<version>` convention. Examples:
  `madder-codec-none-v1`, `madder-codec-gzip-v1`,
  `madder-codec-zstd-v1`. The type-tag is what semantic consumers
  care about ("I want a zstd codec"); two builtin-plugin-ids that
  share a type-tag are interchangeable from the outside.

- **`<builtin-plugin-id>`** is an opaque identifier for a specific
  baked-in implementation, intended to be content-addressed (a
  digest of the plugin's compiled code or its source-tree nix
  store path) so that horizontal versioning falls out of content
  equality. Two binaries with bit-for-bit identical plugin code
  produce the same builtin-plugin-id; binaries with divergent code
  produce different ones. A future evolution lifts builtin-plugin-
  id from "opaque in-binary" to "markl-id of a fetched plugin
  blob," at which point references like `madder-codec-zstd-v1@blake
  2b256-…` become resolvable from arbitrary blob stores. The
  bootstrapping problem (how to read the plugin blob that decodes
  the plugin blob) defers that evolution beyond v0.

For v0 the right side is treated opaquely: madder's plugin
registry maps `<type-tag>@<builtin-plugin-id>` strings to
factories baked in at build time. The convention is **the Go
package name housing the plugin's factory**, e.g.
`madder-codec-zstd-v1@zstd`, where the package's leaf name (`zstd`)
is the opaque id. This gives v0 a stable mechanical rule for
generating ids without designing the build-orchestration story up
front; the FDR 0005 explores the eventual content-addressed
mechanism (nix-store-path digest, source-tree hash, or compiled-
object hash). When that lands, the on-disk surface becomes
`madder-codec-zstd-v1@<digest>` without changing this FDR's
abstractions — only what the right side resolves to.

### Plugin chain

Each store has a single plugin slot in its config — not a list,
not separate compression and encryption fields. Composing
multiple transforms (zstd then encryption) is the responsibility
of a *pipeline plugin* whose factory receives the chain of
sub-plugins as parameters and applies them in order on write,
reverse on read. This keeps the store-config surface flat: one
plugin reference per store, no per-store list management.

V0 ships only single-plugin chains; pipeline plugins are
additive future work.

### Builtin plugins (v0)

Madder's initial plugin registry has four entries, each baked in
at compile time:

| Type-tag                  | Builtin-plugin-id (v0)   | Behavior                              |
|---------------------------|--------------------------|---------------------------------------|
| `madder-codec-none-v1`    | `none`                   | Identity (passthrough). Used by      |
|                           |                          | stores that store raw bytes.          |
| `madder-codec-gzip-v1`    | `gzip`                   | gzip via `compress/gzip`.             |
| `madder-codec-zlib-v1`    | `zlib`                   | zlib via `compress/zlib`.             |
| `madder-codec-zstd-v1`    | `zstd`                   | zstd via the current zstd library.    |

Each builtin-plugin-id matches the leaf name of the Go package
housing the plugin's factory. So `madder-codec-zstd-v1@zstd` is
the v0 reference for the bundled zstd plugin. FDR 0005 explores
the eventual content-addressed replacement; until then the
package-name convention is stable enough to ship.

### V4 store config schema

Existing TOML store configs (V1, V2, V3) carry separate
`compression-type` and `encryption` fields. V4 replaces both with
a single `plugin-chain` field:

```toml
# V4 schema (proposed)
[blob-store]
hash_type-id = "blake2b256"
hash_buckets = [256, 256]
plugin-chain = "madder-codec-zstd-v1@zstd-v1"
verify-on-collision = false
```

The legacy `compression-type` and `encryption` fields are absent
from V4. V3 and earlier stores remain readable; their
`compression-type` and `encryption` slots are translated at
load-time into equivalent plugin references for the duration of
the process. New stores produced by `madder init` write V4 by
default.

### Reading legacy stores

V3 store config:

```toml
# V3 (existing)
[blob-store]
compression-type = "zstd"
encryption = "..."
```

is translated at load-time into the V4 plugin-chain
`madder-codec-zstd-v1@zstd-v1`. The translation is one-way at
read time; V3 configs stay V3 on disk unless the user explicitly
migrates them via a future `madder migrate-blob-store-config`
command (out of scope for this FDR).

## Examples

Inspecting a freshly initialized V4 store:

    $ madder init -encryption none .scratch
    $ madder cat blob-store-config@.scratch
    ---
    ! toml-blob_store_config-v4
    ---
    [blob-store]
    hash_type-id = "blake2b256"
    hash_buckets = [256, 256]
    plugin-chain = "madder-codec-zstd-v1@zstd"
    verify-on-collision = false

Switching to gzip at init time (assuming a flag like `-codec`):

    $ madder init -codec madder-codec-gzip-v1@gzip .archive

A legacy V3 store reads transparently — no user-visible change:

    $ madder cat <id>@.legacy_v3_store
    (decoded via translated plugin-chain
     "madder-codec-zstd-v1@zstd-v1")

## Limitations

- **No dict support.** zstd-with-dict is a separate FDR (0005)
  built on top of this architecture. v0 ships plain zstd only.

- **No out-of-tree plugins.** All plugins are compiled into
  madder; the registry is populated at link time. Out-of-tree
  plugins (process-subcommand-style or Go plugin loading) are
  deliberately not designed yet — the door stays open via the
  reference syntax (`<type-tag>@<builtin-plugin-id>` could
  become `<type-tag>@<markl-id>` later) but no mechanism is
  shipped.

- **No content-addressed builtin-plugin-ids in v0.** The
  builtin-plugin-id is the Go package leaf name housing the
  plugin's factory (e.g. `zstd` for the bundled zstd plugin).
  This is a stop-gap; the eventual mechanism (nix-store-path
  digest, source-tree hash, or compiled-object hash) is the
  subject of FDR 0005. The on-disk and runtime APIs are
  forward-compatible with content-addressed ids — only what the
  right side resolves to changes.

- **No pipeline plugins in v0.** A store has one plugin. A
  future pipeline plugin can compose multiple transforms; v0
  doesn't ship one. This means encryption-on-top-of-compression
  isn't expressible until either (a) a pipeline plugin lands, or
  (b) a single combined plugin (e.g.
  `madder-codec-zstd-then-age-v1`) is registered.

- **No per-blob plugin marker.** The wire format on disk does
  not carry a per-blob plugin reference. Reads consult the
  store's config chain (or its V3 translation) and decode every
  blob through it. Mixing encodings inside a single store is not
  possible in v0; "different chain" maps to "different store."
  This preserves backward compatibility with all existing data.

- **Dewey ownership.** Once V4 stores are the norm, dewey's
  `compression_type` package can be deprecated or thinned. v0
  doesn't remove anything from dewey — madder simply stops
  importing the enum where it's been replaced.

### Out-of-scope dependencies

- **Encryption migration.** Today's `encryption` field
  references a markl-id of an encryption key. Translating this
  into a plugin reference requires picking how key references
  flow through the plugin instance's parameters; a v4-encryption-
  plugin design is its own piece of work. v0 supports encryption
  through the V3-translation path only.

- **inventory_archive's per-entry compression byte.** The
  inventory-archive on-disk format records a per-entry
  compression byte (`CompressionByteNone`, `CompressionByteGzip`,
  …). Once V4 ships, that byte becomes a translation surface from
  the per-entry encoding to a plugin reference. Functioning
  faithfully within today's wire format; a future archive format
  could embed plugin references directly. v0 keeps the byte and
  translates.

## More Information

- `docs/features/0003-zstd-dict-hints-v0.md` — superseded by
  this FDR. The v0 dict-hint design lived above the IOWrapper
  layer; the plugin architecture subsumes that approach by making
  dict-awareness a property of a specific plugin
  (`madder-codec-zstd-with-dict-v1`, deferred to FDR 0005).
- FDR 0005 (forthcoming) — explores the build-orchestration
  mechanism for content-addressed builtin-plugin-ids. The v0
  package-name convention is a stop-gap; FDR 0005 picks the
  eventual replacement.
- FDR 0006 (forthcoming) — adds the `zstd-with-dict` plugin and
  the `cg capture --zstd-dict` / `madder train-zstd-dict` user-
  facing surfaces on top of this architecture, replacing the
  superseded FDR 0003 user-surface design.
- `github.com/amarbel-llc/purse-first/libs/dewey/delta/compression_type` —
  the dewey package this architecture displaces. Stays available
  to dewey's own consumers; madder stops depending on it for new
  V4 stores.
- `go/internal/0/domain_interfaces/blob_store.go` —
  `BlobIOWrapper` interface (`GetBlobCompression`,
  `GetBlobEncryption`). v4 stores satisfy this by returning a
  plugin-derived wrapper; v3 stores satisfy it via the existing
  paths.
- `go/internal/foxtrot/blob_stores/store_inventory_archive.go` and
  `_v1.go` — the per-entry compression byte that needs to remain
  faithful when its host store is V4.
