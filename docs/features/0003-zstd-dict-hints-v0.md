---
status: proposed
date: 2026-05-03
promotion-criteria: |
  Promote to `experimental` once `cg capture --zstd-dict` and
  `madder train-zstd-dict` ship behind a real-corpus measurement
  (capture-receipt blobs compressed with vs. without a trained
  dict) demonstrating a meaningful size win. Promote to `accepted`
  after one tagged release with the feature in user hands and no
  surprise correctness reports.
---

# zstd dictionary hints (v0)

## Problem Statement

Madder's blob stores apply a single store-wide zstd encoding to every
blob with no per-write control. For corpora of many small,
structurally-similar blobs — capture-receipt entries, hyphence-wrapped
metadata, future inventory-log records — dictionary-mode zstd
compresses substantially better than dictless zstd, but the producer
that knows which corpus a blob belongs to has no way to supply a
dictionary at write time. v0 carves a minimal slice that lets users
train a dictionary against an existing blob corpus and pass it to
`cutting-garden capture` for a single invocation, without changing the
existing `BlobWriter` factory chain or affecting any other write path.

## Interface

### `cg capture --zstd-dict <value>`

Single new flag on `cutting-garden capture`. `<value>` is resolved
against two namespaces in parallel:

1. **Filesystem path** — `os.Stat(<value>)` succeeds and points at a
   regular file.
2. **Blob id** — `<value>` parses as a markl-id and the active blob
   store reports `HasBlob(...)` true.

Both candidates are opened, and the first 4 bytes are checked against
the zstd dictionary magic number `0xEC30A437` (RFC 8878 §5).
Candidates that fail magic are dropped:

- **Exactly one survivor** — used as the dictionary.
- **Two survivors** (file AND blob both pass magic) — refused as
  ambiguous; both interpretations listed in the diagnostic so the
  user can pick a different value.
- **No survivor** — refused; if any candidates existed but failed
  magic, the diagnostic reports that to distinguish "wrong file"
  from "missing input."

The resolved dictionary is loaded into memory once and reused across
every blob the capture writes (single dict per invocation; no
mid-invocation switching).

### `madder train-zstd-dict`

New top-level subcommand on `madder`. Reads training samples from
stdin as a list of blob ids (one per line; blank lines and lines
starting with `#` ignored). For each id, the command resolves the
blob in the active store and hands its bytes to zstd-go's training
API as a discrete sample, preserving per-sample boundaries.

The trained dictionary is written to the active blob store via the
standard `BlobWriter` path as **raw zstd-dict bytes** — no hyphence
wrapping, no type tag at the blob level. The markl-id of the
resulting blob is printed to stdout.

#### Flags

- `--dict-size <bytes>` — target dictionary size. Defaults to
  zstd-go's recommendation (~110 KB).

### Underlying plumbing

Two additive optional interfaces in `dewey/0/interfaces`. Existing
implementations are unaffected.

```go
type IOWrapperPrototype interface {
    NewInstance() IOWrapper
}

type IOWrapperWithSideData interface {
    SetSideData(data any) error
}
```

`config.GetBlobCompression()` continues to return one value per
config; that value is reinterpreted as a *prototype*. Each store's
`MakeBlobWriter` calls `NewInstance()` on the prototype to obtain a
fresh per-writer instance — for codecs without per-write state, the
instance is the prototype itself; for zstd-with-dict, it's a copy.
The `BlobWriter` forwards `SetSideData` to its instance.

The `BlobWriter` interface, the `BlobWriterFactory.MakeBlobWriter`
signature, and the 14 existing call sites are unchanged. Producers
opt in by type-asserting:

```go
wc, _ := blobStore.MakeBlobWriter(nil)
if setter, ok := wc.(interfaces.IOWrapperWithSideData); ok {
    setter.SetSideData(dictBytes)
}
```

Cross-store sync inherits dict-portability for free: zstd's wire
frame carries `Dictionary_ID` (RFC 8878 §3.1.1.1.1.6), so a blob
written under a dict travels byte-exact to other stores. The
destination needs the dict blob (resolvable by the same markl-id) to
read it; if the destination can't, the existing source-decompress +
destination-recompress path in `madder sync` handles it.

## Examples

Train a dictionary from existing capture-receipt blobs and use it on
the next capture:

    $ madder list --type cutting_garden-capture_receipt-fs-v1 \
        | madder train-zstd-dict
    blake2b256-3pyfgj…
    $ cg capture --zstd-dict blake2b256-3pyfgj… ./project

(`madder list --type ...` is illustrative; the workflow works with
any source of blob ids.)

Pass a dictionary by filesystem path:

    $ cg capture --zstd-dict ./capture-receipt.dict ./project

Ambiguity diagnostic when both a file and a blob with the same name
exist and both pass the magic check:

    cutting-garden: capture: --zstd-dict foo: ambiguous (file 'foo'
        and blob 'blake2b256-…' both look like zstd dictionaries;
        pass an unambiguous form)

Wrong-content diagnostic when the input exists but isn't a zstd
dictionary:

    cutting-garden: capture: --zstd-dict /etc/passwd: not a zstd
        dictionary (expected magic 0xEC30A437, got 0x726f6f74)

## Limitations

v0 deliberately ships the smallest useful slice. The list below
records what is **explicitly out of scope**, with rationale, so the
boundary is visible to anyone picking up the next slice.

- **`--zstd-dict` on other write-family commands.** `madder write`,
  `madder cache-write`, `madder pack-blobs`, and `madder sync`
  remain dictless. v0 limits adoption to `cg capture` because that
  command has the strongest type-context (capture-receipts are a
  homogeneous corpus); broader rollout waits until one real-corpus
  example has shipped and is delivering a measurable win.
- **Registry config (`type-tag → dict-blob-markl-id`).** Deferred
  until multiple typed corpora want auto-default dicts. Today only
  capture-receipts qualify; building a registry for one entry is
  premature.
- **Default-dict-by-type-tag CLI sugar.** `cg capture` does not
  auto-resolve a registry default when `--zstd-dict` is absent.
  v0 stays opt-in only; explicit flag, no surprise behavior.
- **`--no-dict` override flag.** Only meaningful once registry-based
  defaults exist, which they don't in v0.
- **Mid-invocation dict switch (per-blob context).** Single dict per
  invocation. A future producer that wants per-blob dict selection
  (e.g. capture writing typed-entry blobs and a typed receipt with
  different dicts) would grow a per-blob hint mechanism on top of
  the v0 plumbing.
- **Per-sample training input mode.** v0 takes blob ids on stdin
  precisely so per-sample boundaries fall out of the blob structure
  itself. Length-prefixed or NDJSON-wrapped raw input is unnecessary
  for v0 and not provided.
- **Inventory-log writer adoption.** The inventory log is currently
  file-based, not blob-store-based; adopting `IOWrapperWithSideData`
  there is conditional on either moving the log into the blob-store
  framework or growing its own dict-aware compression knob. Either
  is a separate piece of work.
- **`pack-blobs`-time dict re-encoding semantics.** `pack-blobs`
  builds inventory archives. Whether a dict applies to the archive's
  per-entry compression, the archive-level frame, or both is a real
  design question that touches the inventory-archive wire format.
  Deferred until a concrete need surfaces.
- **Auto-trained dictionaries from rolling samples.** Static-registry
  path must be solid first; auto-training adds rotation and migration
  questions that are easier to answer once the static path is
  shipping.
- **Encoder template caching micro-optimization.**
  `zstd.NewWriter(out, WithEncoderDict(d))` re-parses the dict per
  call, costing tens of microseconds per write. Trivial; deferred
  until measured.

### Out-of-scope dependencies

- **Encryption layering.** The dict applies to plaintext (before
  envelope encryption). The prototype/instance pattern preserves
  this layering — encryption sits above the IOWrapper — but a
  reviewer should confirm the assumption holds when the first
  encrypted store adopts a dict.
- **Small-blob performance.** Some type-tags may regress with
  dict-mode (per-blob startup cost dominates for very small blobs).
  v0 does not auto-apply dicts, so users opt in informed; the
  registry layer (deferred) is the right place to encode "this
  type-tag isn't worth dictifying" once it exists.

## More Information

- madder#133 — exploration thread; the issue body captures the open
  questions this FDR resolves and the design comment captures the
  prototype/instance + `IOWrapperWithSideData` architecture in full.
- RFC 8878 — zstd format specification. §3.1.1.1.1.6 covers
  `Dictionary_ID` in the frame header (the wire mechanism that makes
  cross-store sync work without re-encoding); §5 specifies the
  dictionary magic number used by the `cg capture --zstd-dict`
  validation rule.
- `go/internal/0/domain_interfaces/blob_store.go` —
  `BlobWriter`/`BlobWriterFactory` definitions; the chain that v0
  deliberately does not modify.
