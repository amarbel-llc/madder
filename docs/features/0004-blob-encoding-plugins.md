---
status: experimental
date: 2026-05-03
promotion-criteria: |
  Promote to `accepted` after one tagged release with the refactor
  in user hands and no surprise correctness reports.

  Promotion to `experimental` (this status) was reached on 2026-05-04
  when the plugin registry, the four built-in plugins (none, gzip,
  zlib, zstd), and the legacy-config-translation path shipped, and
  the existing test/bats suites passed green against madder with the
  `dewey/delta/compression_type` import dropped.
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

### Store configs in v0

**No new on-disk config version.** Existing TOML store configs
(V1, V2, V3) stay exactly as they are. There's no V4 schema, no
`madder init` change, no new flags. v0 is a pure under-the-hood
refactor: the only change is what madder builds in memory when
it loads any of those configs.

### Reading legacy stores

V1, V2, and V3 store configs keep their on-disk shape
unchanged:

```toml
# V1/V2/V3 (existing on-disk)
[blob-store]
compression-type = "zstd"
encryption = "..."
```

At load time the legacy `compression-type` string is mapped to
a plugin reference via a small in-binary table:

| Legacy `compression-type` | Plugin reference                  |
|---------------------------|-----------------------------------|
| `""` or `"none"`          | `madder-codec-none-v1@none`       |
| `"gzip"`                  | `madder-codec-gzip-v1@gzip`       |
| `"zlib"`                  | `madder-codec-zlib-v1@zlib`       |
| `"zstd"`                  | `madder-codec-zstd-v1@zstd`       |

The legacy `encryption` field is unchanged — it's a markl-id of
a key blob, owned by madder, not by dewey. Translation only
applies to compression.

V1/V2/V3 configs stay V1/V2/V3 on disk; the plugin
representation is purely in-memory. Migrating an existing
store's config to V4 is a separate user-facing operation
(`madder migrate-blob-store-config` or similar; out of scope
for this FDR). The runtime code path is unified — every store
flows through the plugin abstraction; the legacy
`compression-type` field is just an alternate serialization
that resolves to the same plugin references.

The result is that **madder drops its import of
`github.com/amarbel-llc/purse-first/libs/dewey/delta/compression_type`
entirely in v0**. The dewey package may stay alive for its
other consumers; madder simply doesn't depend on it for any
code path, new or legacy.

## Examples

v0 has no user-visible CLI surface — `madder init`, `madder
write`, `madder cat`, `cg capture` all behave identically to
before. The only observable difference is internal: madder no
longer imports `dewey/delta/compression_type`. From the user's
perspective, an existing store reads and writes exactly as it
did before:

    $ madder cat <id>@.legacy_v3_store
    (decoded via the in-memory plugin
     `madder-codec-zstd-v1@zstd`, which the V3 config's
     `compression-type = "zstd"` translates to at load time)

Initializing a new store still produces a V3 config:

    $ madder init -encryption none .scratch
    $ madder cat blob-store-config@.scratch
    ---
    ! toml-blob_store_config-v3
    ---
    [blob-store]
    hash_type-id = "blake2b256"
    hash_buckets = [256, 256]
    compression-type = "zstd"
    encryption = "..."

Internally, the runtime instantiates `madder-codec-zstd-v1@zstd`
when this config is loaded; the user never sees a plugin
reference.

## Limitations

- **No dict support.** zstd-with-dict is a separate future FDR
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
  store's config (translated to a plugin reference) and decode
  every blob through it. Mixing encodings inside a single store
  is not possible in v0; "different chain" maps to "different
  store." This preserves backward compatibility with all
  existing data.

- **No new on-disk config version in v0.** V1/V2/V3 store
  configs stay on disk exactly as they are. There is no V4
  schema, no `madder init` flag changes, no migration command.
  A future FDR may introduce a V4 schema that surfaces the
  plugin-chain field directly in the TOML, but that's
  orthogonal to this v0 — the plugin runtime is fully usable
  without it.

- **Dewey ownership.** Madder drops its import of
  `dewey/delta/compression_type` entirely in v0 — both new V4
  stores and legacy V1/V2/V3 stores route through the plugin
  registry. Dewey's `compression_type` package may stay alive
  for other consumers; madder no longer depends on it.

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

- `README.md` §Philosophy — frames blob self-containedness as an
  external API contract (`MakeBlobReader(id)` returns fully decoded
  bytes; the consumer never names a sidecar) rather than a
  byte-layout claim. Plugins MAY use sidecar data inside the store
  to deliver that surface. v0's four plugins are stateless and
  don't exercise this latitude; future plugins (notably zstd-with-
  dict) will, and the abstraction here is forward-compatible with
  that.
- `docs/features/0003-zstd-dict-hints-v0.md` — superseded by
  this FDR. The v0 dict-hint design lived above the IOWrapper
  layer; the plugin architecture subsumes that approach by making
  dict-awareness a property of a specific plugin
  (`madder-codec-zstd-with-dict-v1`, deferred to a future FDR).
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
