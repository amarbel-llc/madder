# Typed hyphence-formatted blob-write log — design

**Status:** draft
**Date:** 2026-04-26
**References:**
- ADR 0004 — `Per-blob write-log at $XDG_LOG_HOME/madder/ via a BlobWriteObserver`
  (`docs/decisions/0004-blob-write-log-via-observer.md`)
- Current implementation: `go/internal/juliett/write_log/`
  (renaming to `inventory_log/` — see below)
- Native event surface: `go/internal/0/domain_interfaces/blob_write_observer.go`
- Hyphence format: `docs/man.7/hyphence.md`
- TAI primitive: `go/internal/0/ids/tai.go` (ported from dodder
  `go/internal/bravo/ids/tai.go`)
- Ecosystem TAI64N tracking issue: `amarbel-llc/eng#44`

## Context

ADR 0004 introduced `$XDG_LOG_HOME/madder/blob-writes-YYYY-MM-DD.ndjson` —
one NDJSON record per blob publish, fixed schema, fixed disposition enum
(`written`, `exists`, `verify-match`, `verify-mismatch`). The format works
for the audit-trail use case the ADR was scoped to, but two pressures
have accumulated:

1. **Wire-format consistency.** madder's other on-disk artifacts
   (blob-store configs, type definitions, zettels, pack manifests) all
   use the hyphence format defined in `docs/man.7/hyphence.md`. The
   write-log is the one-off — JSON in a sea of hyphence. New readers,
   formatters, and tooling have to special-case it.

2. **Embedding / extensibility.** Madder is a library as well as a CLI.
   Importers (other Go programs that pull `pkgs/blob_stores`) want to
   record entries through the same observer pipeline, but with their
   own event types — domain-specific records that the importer
   understands but madder does not. The current `BlobWriteObserver`
   interface is hard-coded to a single `BlobWriteEvent` shape.

This design wraps the existing NDJSON record stream in a hyphence
envelope and introduces a codec registry so importers can extend the
log with new event types without modifying madder.

## Non-goals

- **Multiple on-disk body formats in v1.** This design only ships
  `madder-inventory_log-ndjson-v1`. The `!` line is structured to admit
  future formats (`-binary-v1`, `-cbor-v1`, etc.) but the dispatcher
  for them is not built in v1.
- **Schema validation across event types.** Each codec owns its own
  payload schema; the registry only knows event type-string → codec.
  Cross-event invariants (ordering, causality) are out of scope.
- **TAI64N timestamps.** Tracked at `amarbel-llc/eng#44`. Madder uses
  `ids.Tai`'s `sec.asec` format on the wire. If TAI64N wins later it
  is one codec change.
- **multilog directory layout** (`current` + `@<tai64n>.{s,u}`). Same
  reason — a future ecosystem-wide decision.
- **Replacing `BlobWriteObserver` at the store-impl call site.** Stores
  keep calling `OnBlobPublished(BlobWriteEvent{…})`. The shift is below
  that interface, in how the observer dispatches the event onto disk.
- **Backwards compatibility with the existing NDJSON files.** Pre-cut
  files keep their `.ndjson` names and stay readable by `jq`; new
  sessions write `.hyphence`. Two formats coexist on disk; conversion
  tooling is out of scope.
- **Concurrent multi-process appends to a single session file.** Each
  CLI invocation owns its own session file; no cross-process append
  contention.

## On-disk format

### Document structure

The session file is **one hyphence document**. The metadata block
identifies the body format. The body is a stream of records in that
format. For v1, the body is NDJSON: one JSON event object per line.

```
---
! madder-inventory_log-ndjson-v1
---

{"type":"blob-write-published-v1", … }
{"type":"blob-write-published-v1", … }
{"type":"myutility-foo-v1",         … }
```

The metadata is intentionally minimal — only the `!` line. No
description, no tags, no blob references. Anything that varies per
session belongs in the body, alongside per-event metadata.

The body format type follows the convention
`madder-inventory_log-<format>-v<N>`. The first (and currently only)
shipped format is `ndjson`. Future formats reuse the prefix and
introduce new dispatch.

The hyphence document layout above is the only sanctioned form. If we
later need session-level metadata (e.g. host identity, correlation
ids), it gets added either:
- as a new body-format type that defines its own envelope, or
- as a special leading event inside the NDJSON body (e.g.
  `"type":"madder-inventory_log-session-v1"`).

We do **not** add ad-hoc `#` / `-` / `@` lines to the hyphence header.

### NDJSON body

Each line is a JSON object with at least a `type` field. The `type`
value matches a registered codec's `Type()` and selects the
deserializer. Codecs for native types ship in this package; importers
register their own for domain-specific events.

The on-disk shape of the native blob-write record stays close to ADR
0004's existing JSON, with one new `type` field added for dispatch:

```json
{
  "type":        "blob-write-published-v1",
  "ts":          "<sec.asec>",
  "utility":     "madder",
  "pid":         12345,
  "store_id":    "<id>",
  "markl_id":    "<digest>",
  "size":        12345,
  "op":          "written",
  "description": "<--log-description, omitted when empty>"
}
```

The `ts`, `utility`, `pid`, `store_id`, `markl_id`, `size`, `op`, and
`description` fields preserve the existing `Record` schema in
`go/internal/juliett/write_log/record.go`. The only schema change is
the addition of `type`.

### File location, sharding, and session-id format

```
$XDG_LOG_HOME/madder/inventory_log/YYYY-MM-DD/<session-id>.hyphence
```

- **Subdirectory:** `inventory_log/`, matching the package and the
  `!` type prefix.
- **Sharding:** flat `YYYY-MM-DD/` directories, one per UTC date. Date
  is the session's start-time UTC date — a session running across
  midnight stays in its start-day directory.
- **File extension:** `.hyphence`.
- **Session id:** TAI `sec.asec` + `-` + 4 hex chars of CSPRNG entropy.
  Example: `2052403200.123456-7f3a`. The TAI prefix sorts files by
  start time within the day directory; the 4-hex suffix prevents
  collisions for sub-attosecond-aligned starts (and for clock-rewound
  starts) without bulking up the filename.

### Atomicity and concurrency

Each session writes to a unique file. No cross-process append race.
Within a session, writes are sequential (single goroutine holds the
observer's mutex), so no intra-process contention.

A crash mid-write produces a truncated final line in the body. Readers
that walk the NDJSON stream MUST tolerate a truncated trailing line
(return the events parsed so far, surface the truncation as a
warning).

## API shape

The package is renamed to `inventory_log` to match the wire-format type.
File path:
`go/internal/juliett/inventory_log/` (slot stays at juliett — no layer
move, just a rename).

Three small interfaces, one type-bound helper, one registry.

```go
// Package go/internal/juliett/inventory_log/

// LogEvent is what callers Emit. The discriminator method tells the
// Observer which codec to dispatch through. Implemented by every event
// type, native or importer-supplied.
type LogEvent interface {
    LogType() string // matches the JSON "type" field for this event
}

// Codec serializes one LogEvent shape to and from one NDJSON line.
// Constructed via MakeCodec[E]; the registry holds the type-erased form.
// One codec per event type-string. Native types are reserved.
type Codec interface {
    Type() string

    // Encode marshals event to a single JSON object including a
    // top-level "type" field. The Observer adds a trailing newline
    // before writing to the body.
    Encode(event LogEvent) ([]byte, error)

    // Decode unmarshals one NDJSON line into the typed payload.
    // Used by readers/tests.
    Decode(line []byte) (LogEvent, error)
}

// MakeCodec binds a concrete event type E to a type-string and returns
// the type-erased Codec the registry stores. Type assertion happens
// once inside Encode; callers see typed encode/decode signatures.
func MakeCodec[E LogEvent](
    typeStr string,
    encode func(E) ([]byte, error),
    decode func([]byte) (E, error),
) Codec
```

### Registry

```go
// Registry maps event type-string to Codec. Native types are reserved
// at init time; both Global and per-Observer registration of a
// reserved type panics. Importers register only new types.
type Registry interface {
    Register(c Codec)                       // panics on reserved or duplicate
    Lookup(typeStr string) (Codec, bool)
}

// Global is the package-level registry. Importers MAY register codecs
// from init(); FileObserver consults Global at Emit time.
var Global Registry

// Reserved type-strings owned by inventory_log:
var reservedTypes = map[string]struct{}{
    "blob-write-published-v1": {},
}
```

Reserved-type policy follows the user feedback memorialized in
`feedback_codec_registration_stability.md`: native codecs are immutable
contracts. Customization happens at the Observer layer, not by
replacing serialization.

### Observer

```go
// Observer is the pluggable sink. FileObserver and NopObserver
// implement it. Custom test/library observers implement it directly.
type Observer interface {
    // Emit dispatches event through the Observer's effective codec set
    // (per-Observer overrides for *non-reserved* types, then Global).
    // Reserved types always dispatch through the registered native
    // codec. No codec found for a non-reserved type → best-effort
    // drop + one-shot debug.Options warning.
    Emit(event LogEvent)

    // RegisterCodec installs a per-Observer codec for a non-reserved
    // type. Reserved types panic. Returns the codec it displaced (nil
    // if none) so importers can wrap.
    RegisterCodec(c Codec) (previous Codec)
}
```

`FileObserver`'s output is the hyphence document described above:
emits the `! madder-inventory_log-ndjson-v1` header on first Emit,
then writes one NDJSON line per Emit. Closes deterministically when
its Close method is called (CLI exit, library teardown).

### Adapter for the existing BlobWriteObserver interface

Stores keep calling `OnBlobPublished`. A small adapter forwards to the
Observer:

```go
// AsBlobWriteObserver wraps an Observer so it satisfies the existing
// domain_interfaces.BlobWriteObserver contract. Lets us swap the
// underlying log machinery without touching every store impl.
func AsBlobWriteObserver(o Observer) domain_interfaces.BlobWriteObserver {
    return blobWriteAdapter{o: o}
}

type blobWriteAdapter struct{ o Observer }

func (a blobWriteAdapter) OnBlobPublished(ev domain_interfaces.BlobWriteEvent) {
    a.o.Emit(ev) // BlobWriteEvent gains a LogType() method below
}
```

`BlobWriteEvent` grows one method:

```go
// In go/internal/0/domain_interfaces/blob_write_observer.go:
func (BlobWriteEvent) LogType() string { return "blob-write-published-v1" }
```

This is the only change to the layer-0 interface package.

## Importer extension model

```go
type MyEvent struct {
    Foo string `json:"foo"`
    Bar int    `json:"bar"`
}

func (MyEvent) LogType() string { return "myutility-foo-v1" }

var myCodec = inventory_log.MakeCodec[MyEvent](
    "myutility-foo-v1",
    func(e MyEvent) ([]byte, error) {
        // marshal `e` as JSON with a top-level "type" field
        ...
    },
    func(line []byte) (MyEvent, error) { ... },
)

// Choice 1: register globally (every Observer in the process emits
// MyEvent through this codec). Recommended for stable, library-level
// event types.
func init() { inventory_log.Global.Register(myCodec) }

// Choice 2: register per-Observer (only this observer emits MyEvent).
obs := inventory_log.NewFileObserver(path)
obs.RegisterCodec(myCodec)
obs.Emit(MyEvent{Foo: "x", Bar: 1})
```

Neither call could substitute for the native blob-write codec —
attempting `Global.Register` or `obs.RegisterCodec` with `Type() ==
"blob-write-published-v1"` panics.

### Custom Observer for test capture

Test code that wants to assert on emitted events does *not* swap
codecs; it implements its own `Observer`:

```go
type CapturingObserver struct{ Events []inventory_log.LogEvent }

func (c *CapturingObserver) Emit(e inventory_log.LogEvent) {
    c.Events = append(c.Events, e)
}
func (c *CapturingObserver) RegisterCodec(inventory_log.Codec) inventory_log.Codec {
    return nil
}

cap := &CapturingObserver{}
adapter := inventory_log.AsBlobWriteObserver(cap)
store := blob_stores.NewLocal(dir, adapter)
// drive writes...
// assert on cap.Events (typed LogEvents, no hyphence parsing required)
```

The `Codec` layer is for wire format only. Runtime behavior — including
test capture, multiplexing, redaction, sampling — lives at the Observer
layer.

## Migration from the current write_log

| Today | After |
| --- | --- |
| Package `go/internal/juliett/write_log/` | Renamed to `inventory_log/` |
| NDJSON file `blob-writes-YYYY-MM-DD.ndjson` per day, shared across processes | Hyphence file (path TBD) per session, body is NDJSON |
| Fixed `Record` struct in `record.go` | `Record` deleted; replaced by the `blob-write-published-v1` codec which marshals `BlobWriteEvent` directly |
| `BlobWriteOp` enum hard-coded in layer 0 | Unchanged. `Op` becomes the `op` JSON field, same string values |
| `O_APPEND` cross-process atomicity | Per-session files, no cross-process contention |
| `SetDescription` on FileObserver | Unchanged. Description is stamped into emitted `BlobWriteEvent.Description` and serialized by the native codec |
| `--no-write-log` / `MADDER_WRITE_LOG=0` | Renamed to `--no-inventory-log` / `MADDER_INVENTORY_LOG=0`. Breaking change for shell rc users; documented in the changelog and the man page |

The `Op` constants in `domain_interfaces/blob_write_observer.go` stay
as they are — they're a Go-level type, separate from the wire format.

## Confirmation criteria

- **Native unit test.** Round-trip a small set of `BlobWriteEvent`
  instances through the native codec; assert the produced JSON line
  matches a golden fixture and that `Decode → Encode → Decode` is
  idempotent.
- **Reserved-type panic test.** `Global.Register` and
  `(*FileObserver).RegisterCodec` both panic with informative messages
  when handed a codec whose `Type()` is reserved.
- **Importer extension test.** A test `MyEvent` + codec registered via
  `Global.Register` round-trips through a `FileObserver` + reader.
- **Custom-observer test.** `CapturingObserver` collects native events
  emitted from a real store-publish call site without producing any
  files on disk.
- **Hyphence wrapper test.** A reader that opens a session file
  parses the hyphence metadata, asserts the `!` type is
  `madder-inventory_log-ndjson-v1`, and walks the body via NDJSON.
- **Integration test.** Drive `madder write` against a fixture, parse
  the resulting session file, assert one event per publish with the
  correct disposition.
- **Reevaluation triggers.** Same as ADR 0004:
  - Record volume outgrows hyphence-NDJSON-in-a-file ergonomics →
    consider multilog-style rotation (tracked at `amarbel-llc/eng#44`).
  - A store impl gains an asynchronous publish model → observer call
    site has to move or grow.

## Resolved decisions

- **File path layout** — `$XDG_LOG_HOME/madder/inventory_log/YYYY-MM-DD/<session-id>.hyphence`. Flat per-day directories. (Captured in "File location" above.)
- **Session-id format** — TAI sec.asec + 4-hex CSPRNG suffix, e.g. `2052403200.123456-7f3a`. (Captured above.)
- **`LogEvent` interface placement** — `go/internal/0/domain_interfaces/`, paired with `LogType()` on `BlobWriteEvent`. (Captured under "API shape" / "Adapter…")
- **Env var rename** — `MADDER_WRITE_LOG` → `MADDER_INVENTORY_LOG`. Breaking change accepted; called out in the changelog and the man page when this lands. (Captured in the migration table.)
- **Hyphence body adapter** — the existing `hyphence.Writer{Metadata, Blob}` API already separates header from body; no changes needed in the hyphence package. The adapter that bridges async `Emit` calls to the synchronous `Blob.WriteTo` lives privately in `inventory_log` for now. It is the structural mirror of dewey's `ohio.MakePipedReaderFrom` (async writes → sync `ReaderFrom` consumer); the mirror primitive (async writes → sync `WriterTo` producer) is tracked at [amarbel-llc/purse-first#64](https://github.com/amarbel-llc/purse-first/issues/64). The private helper carries a `TODO(#64)` comment so we migrate to dewey's primitive when it lands.

### Body adapter sketch

```go
// In go/internal/juliett/inventory_log/, private to the package:

// pipedWriterTo is a private mirror of dewey/ohio.PipedReader for the
// producer direction: it bridges async Emit-driven writes to the
// synchronous io.WriterTo handed to hyphence.Writer's Blob slot. The
// hyphence Writer call blocks for the lifetime of the session, copying
// pipe bytes into the destination file; Emit writes encoded NDJSON
// lines into the pipe; Close signals EOF and unblocks WriteTo.
//
// TODO(amarbel-llc/purse-first#64) replace with dewey/ohio's
// MakePipedWriterTo once that primitive lands. The internals here are
// expected to migrate verbatim.
type pipedWriterTo struct {
    *io.PipeWriter
    pr *io.PipeReader
}

func newPipedWriterTo() *pipedWriterTo {
    pr, pw := io.Pipe()
    return &pipedWriterTo{PipeWriter: pw, pr: pr}
}

func (p *pipedWriterTo) WriteTo(out io.Writer) (int64, error) {
    return io.Copy(out, p.pr)
}
```

`FileObserver` owns one `*pipedWriterTo` per session; its `Emit` writes
encoded NDJSON lines to the embedded `*io.PipeWriter`; its `Close`
calls `Close()` on the writer side and waits for the goroutine that
runs `hyphence.Writer{Metadata: …, Blob: pipedWriterTo}.WriteTo(file)`.

## Plan

1. Rename `go/internal/juliett/write_log/` →
   `go/internal/juliett/inventory_log/`. Mechanical commit, no behavior
   change.
2. Land `LogEvent` interface and the `LogType()` method on
   `BlobWriteEvent`. (Layer-0 mechanical change.)
3. Scaffold `inventory_log`'s typed surface: `Codec`, `Registry`,
   reserved-type policy, `Observer` interface, the new `FileObserver`
   that writes the hyphence header + NDJSON body, and the
   `AsBlobWriteObserver` adapter.
4. Native `blob-write-published-v1` codec + golden-line unit test.
5. Wire the adapter at the existing observer construction site
   (`env_dir`'s observer wiring), keep `--no-write-log` semantics
   intact.
6. Importer-extension and custom-observer tests as described under
   "Confirmation criteria".
7. Delete the old NDJSON `Record` struct, `recordFromEvent`, and the
   day-rollover logic. (Files written before the cutover stay on
   disk; no migration tooling.)
8. Update ADR 0004 with a "Superseded by" pointer to this design once
   landed.
