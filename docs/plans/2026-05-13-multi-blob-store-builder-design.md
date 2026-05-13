# Multi blob-store builder + read-through cache fill — design

Date: 2026-05-13
Status: design (approved by user; awaiting implementation plan)

## Goal

Consolidate today's `Multi` (broadcast-write fanout) with a new
write-through-cache mode (one designated write store + N read
sources + auto-copy-on-miss into the write store) into a single
type, constructed via a builder. Export it through
`go/pkgs/blob_stores/` so external consumers (cutting-garden,
future wrappers) can use it as a library.

## Context

`Multi` lives at `go/internal/foxtrot/blob_stores/multi.go`. Today
it: fans out reads (first hit), writes to all children via
`io.MultiWriter`, implements only `BlobAccess` (not the full
`BlobStore`). Its struct fields are lowercase and no exported
constructor exists, so there are zero compile-time call sites in
the repo — consolidation is free of breaking-change concerns.

Adjacent primitives already exported in the same package and
reused by this design:
- `CopyBlobIfNecessary` (`copy.go:12`) — dst-has check, copy
  with verification, optional cross-hash digest mapping via
  `BlobForeignDigestAdder`.
- `CopyReaderToWriter` (`copy.go:147`) — copy with extra-sink
  hook and heartbeat pulse.
- `VerifyBlob` (`verification.go:13`) — full-read digest verify.

`interfaces.ActiveContext.After(FuncActiveContext)` (from dewey)
is the standard deferred-cleanup hook (e.g. `env_dir/construction
.go:157`, `store_remote_sftp.go:273`, `util_ssh.go:90`). `Multi`
already carries `ctx interfaces.ActiveContext` for this purpose.

The CLI today reads stores via `BlobStoreEnv` (`MakeBlobStores`
in `blob_store_env/main.go:143`) and exposes each store
individually; no command wraps stores via `Multi`.

## Design

### Single type, two modes, builder-constructed

One concept, two named modes. The builder commits the caller to
exactly one mode at chain time:

- **Mirror mode** — `.Mirror(stores...)`. Preserves today's
  broadcast-write semantics. Writes via `io.MultiWriter` to all
  children. Reads fan out, first hit wins.
- **WriteTo+Read mode** — `.WriteTo(store).Read(stores...)`. One
  designated write target. N read-only sources. Writes go only
  to the write target. Reads check the write target first, then
  read sources in order. On miss in the write target but hit in
  a read source, a tee-during-read copies bytes into the write
  target; `.ReadFill(false)` disables the tee.

The mode is an internal enum on `Multi`; the exported type is
one. Branches happen inside each method.

### Builder API

```go
builder := blob_stores.NewMulti(ctx)

mirror, err := builder.Mirror(storeA, storeB, storeC).Build()

cache, err := builder.
    WriteTo(local).
    Read(remoteA, remoteB).
    Build()                       // ReadFill defaults to true

cacheNoFill, err := builder.
    WriteTo(local).
    Read(remoteA, remoteB).
    ReadFill(false).
    Build()
```

Build-time validation errors:
- no stores given
- mode confusion (`Mirror` then `Read`; `WriteTo` then `Mirror`)
- write store also appearing in the read list
- `.ReadFill(...)` chained after `.Mirror(...)`

Single-type-with-runtime-check (not two builder types) — matches
the existing repo style (no fluent builders elsewhere).

### Full `BlobStore` interface

`Multi` widens from `BlobAccess` to the full `BlobStore`
contract.

| Method | Mirror | WriteTo+Read |
|---|---|---|
| `HasBlob(id)` | any child true | write store OR any read source true |
| `MakeBlobReader(id)` | fanout, first hit | write store first; on miss, scan read sources; on hit and ReadFill=on, return tee reader; on hit and ReadFill=off, return source reader directly |
| `MakeBlobWriter(h)` | `io.MultiWriter` across all children | write store only |
| `AllBlobs()` | N-way merge of child sequences | same, treating write+read as the merge inputs |
| `GetBlobStoreDescription()` | `"multi/mirror(A,B,C)"` | `"multi/write-through(W=local, R=remoteA, R=remoteB)"` |
| `GetDefaultHashType()` | first child | write store |
| `GetBlobStoreConfig()` | first child | write store |
| `GetBlobIOWrapper()` | first child | write store |

#### AllBlobs merge semantics

N-way merge of child `AllBlobs()` sequences using `iter.Pull` to
convert each `interfaces.SeqError[MarklId]` to pull semantics.
Order: lexicographic byte order of `MarklId` (which embeds the
hash-format tag). Dedupe: yield once when multiple heads match
the current minimum.

Implications:
- Same-hash duplicates across stores collapse to one yielded entry.
- **Different-hash digests are not byte-equal and pass through as
  separate entries.** The same logical blob can appear under
  multiple hash-typed entries — by design.
- Children's `AllBlobs` are expected to be sorted (lexically by
  MarklId byte representation). Filesystem-backed stores satisfy
  this via `filepath.WalkDir`'s lexical ordering. **The sort
  contract should be added to `BlobStore.AllBlobs` doc** — it is
  load-bearing for this merge.

### Tee-during-read with ctx.After completion

When the write store misses, a read source has the blob, and
ReadFill is on:

```go
type teeBlobReader struct {
    src  domain_interfaces.BlobReader
    sink domain_interfaces.BlobWriter
    done atomic.Bool
}

// On every Read, bytes tee to sink.
func (t *teeBlobReader) Read(p []byte) (int, error) {
    n, err := t.src.Read(p)
    if n > 0 {
        _, _ = t.sink.Write(p[:n])  // poisoning handled below
    }
    return n, err
}

// Caller-initiated commit: close both, mark done.
func (t *teeBlobReader) Close() error {
    if t.done.Swap(true) {
        return nil
    }
    err1 := t.src.Close()
    err2 := t.sink.Close()  // commits to write store
    return errors.Join(err1, err2)
}

// Registered via ctx.After at construction.
// Drains the remainder through the tee, then commits.
func (t *teeBlobReader) flushAndCommit(ctx) {
    if t.done.Swap(true) {
        return
    }
    _, _ = io.Copy(io.Discard, t)  // through the tee → sink fills
    _ = t.src.Close()
    _ = t.sink.Close()             // commits to write store
}
```

Key properties:
- `io.Copy(io.Discard, t)` (not `t.src`) — draining through the
  tee is what causes the sink to receive remaining bytes.
- "Remove from the ctx.After flush pool" is implemented as an
  `atomic.Bool` race, not via a context-side cancel handle. The
  repo's `ActiveContext.After` has no `stop` return (confirmed
  in `store_remote_sftp_test.go:229`). The handler fires
  unconditionally at ctx end but no-ops if the caller's Close
  already won.
- Cross-hash digest mapping (`BlobForeignDigestAdder`) is done
  at the moment of commit on whichever path commits first.
  Logic copied from `CopyBlobIfNecessary` lines 119–132.
- Other `BlobReader` methods (`ReadAt`, `Seek`, `WriteTo`,
  `GetMarklId`) delegate to `src`. `WriteTo` also tees.

### Error handling

Default failure mode for the cache fill is silent / best-effort.
The caller's `Read` and `Close` only surface errors that affect
*their* read, not the cache fill.

| Failure | Caller sees | Cache fill |
|---|---|---|
| Sink Write fails mid-tee | normal bytes from source | abandoned (sink poisoned, no commit) |
| Source Read fails mid-tee | read error | abandoned (sink rejects partial) |
| Sink Close fails at commit | joined into caller's Close err | no commit |
| Source Close fails | joined into caller's Close err | commits if sink had all bytes |
| Context cancelled mid-read | read error from source | abandoned via ctx.After |
| Source returned wrong bytes (digest mismatch) | bytes pass through | sink commits under bytes-actual digest; requested id still missing |
| Two callers miss same blob | both succeed | both commit identical bytes; write store dedupes |

Digest mismatch deserves expansion: the write store is
content-addressed, so wrong bytes hash to a different address —
they cannot corrupt the requested id. They land as a junk entry
under the bytes-actual digest. Next read of the requested id
will retry from a read source. This matches the existing pattern
in `CopyBlobIfNecessary` (lines 107–118): detect-and-report, do
not extend `BlobWriter` to take an expected digest. Future
cleanup is option (b) from the brainstorm — not in scope.

### CLI surface

Multi reaches end-users via a `-multi` flag on existing read
commands:

```
madder cat -multi <id>
madder cat -multi -no-read-fill <id>
madder has -multi <id>
madder list -multi
madder fsck -multi
```

Without `-multi`, commands behave exactly as today. With
`-multi`, the command constructs a `Multi` using the default
blob store as the write target and every other configured store
as a read source. `-no-read-fill` disables the tee.

This keeps the change opt-in at the call site. Existing
single-store usage is untouched.

Future direction (out of scope): promote Multi to a "pointer"
blob-store config type (a config entry that resolves to a Multi
over other configured stores by reference). Blocked on
relative-store resolution and other concerns.

### Packaging

Builder + consolidated `Multi` live next to today's `Multi` in
`go/internal/foxtrot/blob_stores/`. The existing
dagnabit-generated facade at `go/pkgs/blob_stores/main.go` picks
up the exported names automatically.

### Rollback

This design is fortunate on rollback dimensions:

- **No existing callers** of `Multi` — zero call sites in
  compiled Go.
- **No wire-format changes.** `Multi` is a pure in-process
  composition layer. Blob bytes on disk are identical to what
  underlying stores produce today.
- **Two modes coexist by design.** Mirror and WriteTo+Read are
  intended siblings, not generations.
- **Caller rollback procedure** (for downstream consumers
  adopting Multi): change the construction line. Replace
  `.WriteTo(local).Read(remoteA).Build()` with the underlying
  local store directly, or `.Mirror(local).Build()`, or
  whatever they had before. No data migration; no on-disk
  state.
- **CLI rollback:** drop the `-multi` flag from the invocation.
  Default behavior is unchanged.

## Testing (TDD)

The user's preference is bats + Go unit tests, target 100%
coverage for the `Multi`/builder types.

### bats coverage

Drives `Multi` via the `-multi` CLI flag on read commands.

| Test | Setup | Assertion |
|---|---|---|
| `cat -multi` from default store | Blob in default store | Cat with `-multi` returns bytes |
| `cat -multi` from read source | 2 stores; blob only in store-2 | Cat with `-multi` returns bytes |
| `cat -multi` fills default store | Same as above | Default store has blob after cat (via direct `has` against the default store) |
| `cat -multi -no-read-fill` skips fill | Same setup | Default store still missing after cat |
| `list -multi` unions stores | Overlapping + distinct blobs | Each unique same-hash digest listed once |
| `list -multi` cross-hash duplicates | Same logical blob under different hash types | Both digests listed |
| `has -multi` checks all stores | Blob only in read source | `-multi` true; bare `has` false |
| `fsck -multi` walks all | Blobs across multiple stores | Completes; reports per-store totals |
| Concurrent `cat -multi` of same missing blob | Two parallel CLI invocations | Both succeed; default store has one copy at end |

### Go unit tests

Mechanics bats can't observe directly:

- Builder error paths: empty stores, mode confusion (Mirror
  then Read, WriteTo then Mirror), write-in-read, ReadFill
  after Mirror.
- `teeBlobReader.Read` byte-tee mechanics; sink poison flag
  transitions.
- Eager-Close vs. ctx.After race: `atomic.Bool` ensures
  exactly-one commit.
- Partial-drain + ctx.After: `io.Copy(io.Discard, tee)` fills
  the sink.
- AllBlobs N-way merge against handcrafted stub sequences:
  same-hash dedupe, cross-hash pass-through, dupes within a
  store.
- Cross-hash `BlobForeignDigestAdder` registration timing.
- Mirror mode: `MultiWriter` writes to all; close error from
  one child surfaces.

### Test infrastructure

Existing helpers:
- `stubBlobStore` in `store_inventory_archive_test.go:168`
- `spyActiveContext` in `store_remote_sftp_test.go:224`

Add:
- A "controllable" stub `BlobReader` (yields bytes step-by-step
  on demand; can be paused/resumed for race tests)
- A spy `BlobWriter` (records `Write`/`Close` order; can be set
  to fail at a given byte offset)

### Coverage target

100% line/branch coverage for the consolidated `Multi` and the
new builder file. Measured via `go test -coverprofile`. CI gate
applies to these two types specifically (not the whole package).

## Unresolved / deferred items

Captured but not blockers:

- **Long-lived ctx resource pressure**: if many tee writers
  accumulate between caller-close and ctx-end on a long-lived
  context, each holds a temp file / fd. Acceptable for v1;
  revisit if profiling shows pressure.
- **`BlobWriter` taking expected digest** (option (b) from
  error-handling brainstorm): cleaner long-term shape than
  detect-after-Close, but not required — the existing pattern
  is sufficient because content-addressing makes wrong-bytes
  commits non-corrupting.
- **Optional `BlobDeleter` / `BlobForeignDigestAdder` on
  `Multi`**: defer to first caller that needs Multi to satisfy
  these interfaces. YAGNI for v1.
- **"Pointer" Multi config-store type**: future direction
  mentioned by user; needs relative-store resolution first.
  Separate design.
- **Documenting `BlobStore.AllBlobs` sort contract**: load-
  bearing for the N-way merge. Should land alongside this work
  (small doc-only change to `domain_interfaces/blob_store.go`).
