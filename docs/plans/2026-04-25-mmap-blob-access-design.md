# mmap'd `[]byte` access to local blobs — design

**Status:** approved
**Date:** 2026-04-25
**References:**
- POC: `zz-pocs/0001-userfaultfd-mmap/` (validates the lazy-decompression
  follow-up; not on v1's path)
- Existing surface: `go/internal/0/domain_interfaces/blob_store.go`
  (`BlobReader`, `BlobIOWrapper`); `go/internal/echo/env_dir/blob_reader.go`
  (the concrete reader that holds the `*os.File`)

## Context

Madder callers that handle large blobs — concretely, GGUF model files
multi-GB in size — want a `[]byte` view they can hand to a downstream
library (llama.cpp, parsers, image decoders) without buffering the
whole logical blob through `io.Reader`. They also want random access
into the blob without `Seek`-ing through `BlobReader`.

`BlobReader` already exposes `io.ReaderAt` + `io.Seeker`, but today's
implementation has a gotcha: `(*blobReader).ReadAt` delegates to the
*decrypter*, not the decompressor, so for any blob with a non-`None`
compression configured, the offsets it returns are bytes inside the
still-compressed stream — not the logical blob bytes. Callers are
unlikely to notice until they read at non-zero offsets and get
garbage.

Standard file-backed `mmap(2)` cannot transform bytes on page-fault —
the kernel reads the literal file. So a true zero-copy `[]byte` view
of the logical blob is only possible when the bytes on disk equal the
logical bytes. (See the platform survey below for the lazy-decompress
options that *do* exist on Linux but are out of scope for v1.)

## Non-goals

- **Windows.** Antigoal. Madder is Unix-only and stays that way.
- **Lazy decompression on page-fault** for v1 (compressed and/or
  encrypted blobs serving as `[]byte`). The userfaultfd POC at
  `zz-pocs/0001-userfaultfd-mmap/` validates the mechanism on Linux
  for a future iteration; macOS has no in-process equivalent. Tracked
  separately as a follow-up issue.
- **Pack files, inventory archives, SFTP** as mmap sources for v1.
  Tracer-bullet scope is local hash-bucketed only.
- **mmap on the write path.** Blob writes are streaming; size is
  unknown until hashed.

## API shape

Two new exported things plus one new capability interface. Stores stay
unchanged; mmap is a *promotion* of an existing `BlobReader`.

```go
// New package: go/internal/foxtrot/mmap_blob/

type MmapBlob interface {
    Bytes() []byte
    GetMarklId() domain_interfaces.MarklId
    Verify() error  // opt-in digest check; not run by Bytes() or Close()
    io.Closer       // unmaps; idempotent
}

var (
    ErrMmapUnsupported = errors.New("blob is not mmap-able")
    ErrDigestMismatch  = errors.New("mmap'd bytes do not match recorded digest")
)

// MakeMmapBlobFromBlobReader inspects reader. If the reader backs a
// contiguous on-disk range whose bytes equal the logical blob bytes,
// returns an MmapBlob mapping that range. Otherwise returns
// ErrMmapUnsupported. On success, ownership of the underlying file
// transfers to the MmapBlob: caller MUST NOT also Close reader. On
// failure, reader is unchanged and remains the caller's to Close.
func MakeMmapBlobFromBlobReader(reader domain_interfaces.BlobReader) (MmapBlob, error)
```

Capability discovery uses one new interface that only mmap-able store
readers implement:

```go
// New in go/internal/0/domain_interfaces/blob_store.go
type MmapSource interface {
    // MmapSource returns the file region whose bytes are byte-identical
    // to the logical blob bytes. ok=false means "not mappable" (wrong
    // store, or wrappers preclude it); MakeMmapBlobFromBlobReader
    // translates that into ErrMmapUnsupported.
    //
    // On ok=true, the returned file handle's ownership transfers to
    // the caller. The caller is responsible for closing it (typically
    // the MmapBlob does this in its Close()).
    MmapSource() (file *os.File, offset int64, length int64, ok bool, err error)
}
```

Only `env_dir.blobReader` implements `MmapSource` in v1.

## Wrapper-compat policy (strict v1)

`MmapSource()` on `env_dir.blobReader` returns `ok=true` only when
**all three** of these hold:

1. `readSeeker` type-asserts to `*os.File` (excludes SFTP-routed
   readers, stdin, in-memory readers, etc.).
2. `Config.GetBlobCompression() == compression_type.CompressionTypeNone`.
3. `Config.GetBlobEncryption()` is the nil/none encryption.

Any other combination returns `ok=false`. Future random-access-compatible
wrappers (e.g. AES-CTR with a chunked nonce scheme, seekable zstd) can
opt in later by also producing `ok=true` for their case; that's a
per-wrapper decision tracked separately when each is ready.

To support wrapper inspection, `env_dir.blobReader` gains a `config`
field holding the original `Config` it was constructed with. The
existing `NewReader` already receives this `Config`; today it just
extracts the wrappers and discards the rest.

## Read-only mapping

`PROT_READ` + `MAP_SHARED`. Content-addressable blobs are immutable;
`MAP_SHARED` shares the OS page cache with anything else reading the
file (e.g. a concurrent SFTP push reading the same blob, a `cat`
running outside the process). No `mlock`, no read-ahead hints in v1.

## Verification

The default fast path skips digest verification. `BlobReader`'s
existing tee-through-digester pattern only fires on full sequential
read; mmap'd random access has no analog and adding one defeats the
zero-copy goal.

`MmapBlob.Verify()` is opt-in: callers who care can pay the cost.
Implementation walks `Bytes()` through the recorded MarklId's hash
algorithm and returns `ErrDigestMismatch` on mismatch. For multi-GB
GGUF blobs, callers are expected to call `Verify()` once at startup
or never.

## Code placement

```
go/internal/0/domain_interfaces/blob_store.go
    + interface MmapSource

go/internal/echo/env_dir/blob_reader.go
    + blobReader stashes the original Config (one new field)
    + (*blobReader).MmapSource() implementation

go/internal/foxtrot/mmap_blob/    (new package)
    blob.go              — MmapBlob interface + sentinel errors
    promote.go           — MakeMmapBlobFromBlobReader
    mmap_unix.go         //go:build unix — file mmap glue + MmapBlob impl
    promote_test.go      — positive + 4 negative cases
    verify_test.go       — Verify() positive + tampered-file negative
```

`foxtrot` already imports `env_dir` and the blob_stores; `mmap_blob`
sitting alongside `blob_stores` is the natural layer. The `MmapSource`
interface lives in layer 0 so callers don't have to import foxtrot
just to type-assert.

## Errors

- `ErrMmapUnsupported` — promotion refused for any reason
  (non-`*os.File` reader, non-`None` compression, non-nil encryption).
  Returned from `MakeMmapBlobFromBlobReader` only.
- `ErrDigestMismatch` — returned only from `MmapBlob.Verify()` when
  the recomputed digest does not match the recorded MarklId.
- Wrapped syscall errors — `unix.Mmap` / `unix.Munmap` failures wrap
  via `dewey/bravo/errors.Wrap` and surface from
  `MakeMmapBlobFromBlobReader` and `MmapBlob.Close()`.

## Concurrency

Multiple `MmapBlob`s for the same MarklId are safe — `MAP_SHARED` is
kernel-managed, page cache is shared. No explicit refcount in our
type. `MmapBlob.Close()` is idempotent (a `sync.Once` guard around
`unix.Munmap` and `*os.File.Close()`).

Concurrent reads from the same `MmapBlob` are safe — `[]byte` reads
are racy only against writes, and we never write.

## Cross-platform

Linux + macOS via `unix.Mmap` (`PROT_READ`, `MAP_SHARED`). Files in
`foxtrot/mmap_blob/` are tagged `//go:build unix`. Windows is
explicitly out of scope and there is no stub. Aligns with madder's
broader Unix-only posture — the existing tree uses
`golang.org/x/sys/unix` throughout without `_windows.go` shims.

## Testing strategy

**Unit tests** in `go/internal/foxtrot/mmap_blob/`:

- **Positive:** local hash-bucketed store + nil wrappers → promotion
  succeeds, `Bytes()` length matches expected blob size, content
  matches written bytes byte-for-byte.
- **Negative ×4:**
  - `ConfigCompressionTypeZstd` → `ErrMmapUnsupported`
  - non-nil encryption configured → `ErrMmapUnsupported`
  - SFTP-routed reader → `ErrMmapUnsupported`
  - stdin-backed (`bytes.Reader`) reader → `ErrMmapUnsupported`
- **Ownership transfer:** after promotion, calling `BlobReader.Close()`
  must be a no-op for the file (no double-close errno); `MmapBlob.Close()`
  releases.
- **`Verify()`:** positive for an untouched blob; manually corrupt the
  on-disk file and assert `ErrDigestMismatch`.

**Go integration test** in `foxtrot/blob_stores/`:
- One scenario that opens a `localHashBucketed` store (constructed
  in-package with `makeTestStore`), writes a payload via
  `store.MakeBlobWriter`, reads it back via `store.MakeBlobReader`,
  promotes the resulting `BlobReader` via
  `mmap_blob.MakeMmapBlobFromBlobReader`, and asserts `Bytes()` matches
  the written payload byte-for-byte. The CLI is the wrong harness for
  an embedding-driven feature whose value is `[]byte` access from
  library callers — a bats-driven CLI test would have to invent a
  synthetic helper binary just to surface a public API that already
  has Go callers in tree. The integration value here is exercising
  the public store API end-to-end through to `MmapBlob`, in the same
  package and language as the consumers.

## Rollback

Purely additive. No wire-format changes, no on-disk migration,
existing `BlobReader` semantics unchanged.

- **Dual-architecture period:** automatic. Streaming `BlobReader` reads
  remain the default; `MmapBlob` is opt-in per call site.
- **Promotion criterion:** 14 days of in-tree use without callers
  reporting unexpected `ErrMmapUnsupported`s on blobs they expected
  to be mappable, and no `ErrDigestMismatch` triggered by storage
  bugs (as opposed to deliberate tampering).
- **Rollback procedure:** delete `go/internal/foxtrot/mmap_blob/` and
  the `MmapSource` method on `env_dir.blobReader`. Remove the
  `MmapSource` interface from layer 0. Two-commit revert at most.
  Callers fall back to `BlobReader` streaming reads.

## Out-of-scope follow-ups (file as separate issues after this lands)

- **Userfaultfd lazy decompression** — POC validated at
  `zz-pocs/0001-userfaultfd-mmap/`. Linux-only, would let
  compressed/encrypted-with-random-access blobs serve as `[]byte`
  via on-fault chunk decompression. macOS would need eager fill
  or FSKit, both expensive.
- **Pack files (`pack_v0` / `pack_v1`)** — sub-range mmap of a
  packfile is feasible; needs the same wrapper-compat check applied
  to the entry's stored bytes.
- **Inventory archive (`store_inventory_archive*`)** — same idea,
  needs the archive's internal index inspection.
- **SFTP transparent local-cache mmap** — materialize a local cache
  file then mmap that. Real but outside the "zero-copy of an existing
  local blob" framing.
- **Random-access-compatible compression / encryption schemes** —
  e.g. seekable zstd (Facebook's `zstd_seekable` extension), AES-CTR
  with a chunked nonce. Each is a per-wrapper decision.
