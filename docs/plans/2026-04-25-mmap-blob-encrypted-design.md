# Encrypted-blob mmap via userfaultfd — design (draft)

**Status:** draft — not yet approved
**Date:** 2026-04-25
**References:**
- Builds on: `docs/plans/2026-04-25-mmap-blob-access-design.md` (v1, strict-wrapper)
- POC: `zz-pocs/0001-userfaultfd-mmap/` (validated the userfaultfd mechanism on Linux)
- Existing wrapper plumbing: `go/internal/echo/env_dir/blob_config.go`,
  `go/internal/echo/env_dir/blob_reader.go`,
  upstream `purse-first/libs/dewey/{charlie/ohio,delta/compression_type}`

## Context

The v1 mmap-blob path refuses any blob with non-nil encryption — the
file-on-disk bytes are ciphertext, but consumers want plaintext via
`[]byte`. Standard file-backed `mmap(2)` cannot transform pages on
fault, so the v1 design (`docs/plans/2026-04-25-mmap-blob-access-design.md`)
gates promotion on `HasIdentityWrappers()` and returns
`ErrMmapUnsupported` for everything else.

The driving use case for relaxing this gate: **encrypted GGUF models**
stored in madder. A user encrypts a multi-GB model file (privacy on
shared disk, recipient-restricted distribution), and a downstream
consumer (llama.cpp, ollama, a Go inference library) wants the same
zero-copy random-access `[]byte` they'd get for a clear-text blob.

The mechanism is the userfaultfd path validated by POC 0001: when a
caller touches a page in the user mapping, a userspace fault handler
decrypts the relevant ciphertext block and `UFFDIO_COPY`s plaintext
into the page. The requirement is that the encryption scheme is
**chunked AEAD with random-access decryption** — given the file key
and an offset, you can decrypt the chunk containing that offset
without touching any other chunk.

Madder's two encryption schemes — **age** and **pivy ebox** — are both
theoretically streamable in this way (header + chunked-AEAD payload).
Concrete chunk layout, AEAD scheme, and tag size for each is **TBD —
see open questions**.

## Non-goals

- **macOS.** No userfaultfd, no in-process equivalent. macOS encrypted
  blobs stay at `ErrMmapUnsupported` indefinitely. Same posture as the
  POC platform survey.
- **Windows.** Antigoal per the v1 design. Unchanged.
- **Combined encryption + compression.** v1 is encryption-only. The
  composition (decrypt chunk N → decompress that buffer → emit
  plaintext) requires both layers' chunking schemes to align — which
  isn't guaranteed. Tracked as a follow-up; not in this doc's scope.
- **Streaming AEADs without per-chunk independence.** Single-frame
  ChaCha20-Poly1305, `*-Stream` constructions where chunk N depends on
  chunk N−1's state, etc. These cannot satisfy the random-access
  contract and stay refused.
- **Per-byte decryption granularity.** Chunk granularity (typically 4
  KiB–64 KiB) is good enough; the user fault is page-granular anyway.

## Architecture

Four moving parts:

```
┌─────────────────────────────────────────────────────────────────┐
│  Capability probe (new)                                          │
│    RandomAccessDecryptor interface — extends or sits beside      │
│    interfaces.IOWrapper. Schemes opt in by satisfying it.        │
└─────────────────────────────────────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Per-scheme adapters (new)                                       │
│    age adapter, pivy-ebox adapter. Each parses its scheme's     │
│    header at promote time, derives the file key, returns a       │
│    RandomAccessDecryptor closed over (key, ciphertext file).     │
└─────────────────────────────────────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────────────────────────┐
│  mmap_blob promotion path (extended)                             │
│    MakeMmapBlobFromBlobReader: probe for                         │
│    RandomAccessDecryptor; if present, anon-mmap + uffd register, │
│    spawn fault handler; if absent, fall back to v1 strict policy.│
└─────────────────────────────────────────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Fault handler goroutine (Linux-only, per-blob)                  │
│    Read uffd events → identify chunk → DecryptChunk →            │
│    UFFDIO_COPY plaintext into user mapping. runtime.LockOSThread.│
└─────────────────────────────────────────────────────────────────┘
```

### Capability probe — `RandomAccessDecryptor`

New interface in either `domain_interfaces` (layer 0) or alongside
`MmapSource`:

```go
type RandomAccessDecryptor interface {
    // ChunkSize returns the AEAD chunk granularity in bytes. Must be
    // page-aligned. Typical values: 4 KiB, 64 KiB.
    ChunkSize() int

    // PlaintextSize returns the total decrypted blob size in bytes.
    // Derived from the ciphertext file size minus header bytes minus
    // per-chunk tag overhead — scheme-specific arithmetic.
    PlaintextSize() int64

    // DecryptChunk decrypts the chunk at chunkIdx into dst. dst must
    // be sized at least ChunkSize() (or PlaintextSize() % ChunkSize()
    // for the final, possibly-partial chunk). Returns the number of
    // plaintext bytes written.
    DecryptChunk(chunkIdx int, dst []byte) (n int, err error)

    // Close releases any held resources (file handles, key buffers).
    // After Close, the decryptor MUST zero its key material before
    // returning.
    io.Closer
}
```

`MakeMmapBlobFromBlobReader` extends its capability probe:

```go
// Existing v1 path: identity wrappers + *os.File backing.
if isFile && config.HasIdentityWrappers() {
    return mmapFile(...)
}

// New path: random-access decryptor over the (still required) *os.File backing.
if isFile {
    if dec, ok := tryBuildRandomAccessDecryptor(config, file); ok {
        return mmapEncryptedFile(file, dec, reader.GetMarklId())
    }
}

return nil, ErrMmapUnsupported
```

`tryBuildRandomAccessDecryptor` lives next to the existing wrapper-probe
helpers in env_dir or mmap_blob — its job is to inspect
`config.GetBlobEncryption()`, type-assert against
`RandomAccessDecryptor`-bearing wrappers, and (if compression is also
identity) return the decryptor.

### Per-scheme adapters

Each encryption scheme grows a constructor that produces a
`RandomAccessDecryptor` from `(MarklId, *os.File)`. Concretely:

- `go/internal/?/age_random/` — age adapter. Reads age header (bech32-
  encoded recipient stanzas), unwraps file key via the user's age
  identity, returns a decryptor that knows age's chunk layout.
- `go/internal/?/pivy_ebox_random/` — pivy ebox adapter. Reads the
  ebox header (PIV slot references, encrypted file key), unwraps file
  key via PIV touch, returns a decryptor that knows pivy's chunk
  layout.

Both adapters are Linux-only build-tagged (the userfaultfd path is
Linux-only, and mounting these in non-Linux builds achieves nothing).

### Promotion sequence

```
1. MakeMmapBlobFromBlobReader called with reader (a BlobReader).
2. Reader implements MmapSource → MmapSource() returns *os.File + plaintext-or-ciphertext-size hint.
3. Probe: config.HasIdentityWrappers() ? → fast path (v1).
4. Else probe: tryBuildRandomAccessDecryptor(config, file) ?
   a. config.GetBlobEncryption() type-asserts to a RandomAccessDecryptor-bearing wrapper.
   b. Wrapper's constructor is invoked: parse header, derive file key, return decryptor.
   c. PlaintextSize() = decryptor's view of the logical size.
5. anon-mmap PROT_READ|PROT_WRITE of size PlaintextSize, MAP_PRIVATE|MAP_ANONYMOUS.
6. userfaultfd(UFFD_USER_MODE_ONLY|O_CLOEXEC|O_NONBLOCK).
7. UFFDIO_API + UFFDIO_REGISTER over the anonymous region (mode MISSING).
8. Spawn fault-handler goroutine (runtime.LockOSThread; closes over decryptor + uffd + base + bitmap).
9. Return mmapBlob{bytes, marklId, decryptor, uffd, handlerStop, ...}.
```

### Per-fault loop (handler goroutine)

```
1. unix.Poll uffd with 100 ms timeout, check stop flag between polls.
2. On POLLIN, read 32-byte uffd_msg. Validate event type = PAGEFAULT.
3. chunkIdx = (msg.address − base) / ChunkSize.
4. Already populated? Skip (defensive — UFFDIO_COPY also resolves queued faults for that chunk).
5. plaintextScratch := scratchBufs[goroutineID][:ChunkSize]
6. DecryptChunk(chunkIdx, plaintextScratch) → may return < ChunkSize for the final chunk.
7. UFFDIO_COPY {dst: base + chunkIdx*ChunkSize, src: &plaintextScratch[0], length: returned bytes}.
8. Mark chunk populated in bitmap.
```

### Close path

```
1. Set stop flag.
2. Wait for handler goroutine to exit (drain done channel).
3. decryptor.Close() — adapter zeros file-key buffer here.
4. unix.Close(uffd).
5. unix.Munmap(mappedBytes).
6. ciphertextFile.Close().
```

`mmapBlob.Close()` is still idempotent via `sync.Once`. The new fields
(handler goroutine, uffd, decryptor) are all optional — the v1 path
leaves them nil.

## Open questions (require verification)

These need answers before any code lands.

### age payload format — TBD

Best understanding of [age v1](https://age-encryption.org/v1):

- Header is a sequence of recipient stanzas (`-> <type> <args>` lines)
  followed by a MAC line and a `--- <header MAC>` separator. Header
  size is variable; one ReadAt at offset 0 with a generous buffer
  parses it.
- Payload is encrypted with ChaCha20-Poly1305 in **64 KiB plaintext
  chunks** (`64 * 1024` bytes plaintext → `64*1024 + 16` bytes
  ciphertext per chunk; final chunk may be smaller).
- Per-chunk nonce is `counter || final_byte` (11-byte counter big-endian +
  1-byte flag, where flag = 0 for non-final chunks and 1 for the final).
- File key (32 bytes) is derived from any matching recipient stanza.

**Verify before implementing:** the chunk size, AEAD construction, and
nonce derivation. The age repo's `internal/stream/stream.go` is the
authoritative source. Confirm the constants match
`filippo.io/age@v1.3.1` (madder's pinned dep).

### pivy ebox format — TBD

I have no reliable reference for pivy ebox's payload chunking. The
adapter design assumes pivy uses some chunked-AEAD layout; if it
doesn't (e.g. single-frame AEAD over the whole payload, or a custom
non-random-access streaming scheme), pivy ebox cannot satisfy
`RandomAccessDecryptor` and stays at `ErrMmapUnsupported`.

**Verify before implementing:** pivy ebox's payload encryption scheme.
Sources:
- `github.com/joyent/pivy` README and `ebox.h` / `ebox.c` for the
  binary layout.
- madder's existing pivy integration at
  `purse-first/libs/dewey/delta/pivy` for how it currently decrypts.

If pivy ebox is **not** chunked-AEAD, this path supports age only and
pivy stays excluded — that's a real outcome to plan for.

### PlaintextSize derivation per scheme

For age, plaintext size = ciphertext_payload_size − ⌈chunks⌉ × 16.
The ciphertext payload size = file size − header size. Need to parse
header to know its size, then arithmetic gives plaintext. Cheap.

For pivy: TBD pending the format clarification above.

### Decryption errors mid-mapping

A corrupted or tampered chunk fails AEAD verification at fault time.
The handler can't return an error to the caller's read — the kernel
already faulted, the caller's thread is blocked waiting for a page.
Three options:

**(a) UFFDIO_ZEROPAGE** — gives the caller zeros, which they treat as
valid bytes. Wrong: silent data corruption.

**(b) Set a poisoned flag on `mmapBlob`, then UFFDIO_ZEROPAGE.** The
caller reads zeros. `Verify()` catches the corruption later. Still
silent until the caller asks. Not ideal but tolerable for a feature
that already requires opt-in `Verify()` for digest checking.

**(c) Detect at promote time by sniff-decrypting one chunk** (typically
chunk 0). Doesn't catch later-chunk corruption — a malicious actor who
tampers chunk 53 evades the sniff.

**Recommendation:** combine (b) and (c). Sniff-decrypt the first and
last chunks at promote time (cheap — two AEAD ops). If either fails,
refuse the promotion. Mid-mapping failures still set the poisoned
flag, and `Verify()` is the caller's safety net.

A more aggressive option: have the handler crash the process on AEAD
failure. Defensible — corrupted ciphertext past sniff-time means
something is genuinely wrong — but irreversible from the caller's POV.
Not v1 default.

### Concurrency

One fault handler per `MmapBlob`. Each handler holds a locked OS
thread (per the POC's deadlock-avoidance pattern). With many
simultaneous mmap'd encrypted blobs, that's many locked threads.
Bounds:

- A typical GGUF use case is 1–4 mapped models. Fine.
- A pathological case (a process mmap's hundreds of small encrypted
  blobs) would be a problem.

Future optimization: share one userfaultfd across N registered
ranges, with a single handler routing faults to the right blob's
decryptor. Adds a routing table but bounds OS-thread cost.

Defer to a follow-up issue; v1 of this path is per-blob handlers.

### Key zeroization

Go's heap doesn't help here. The pattern:

```go
// Inside the adapter's Close():
for i := range fileKey {
    fileKey[i] = 0
}
runtime.KeepAlive(fileKey)
```

The `KeepAlive` ensures the compiler doesn't elide the zero loop. Same
pattern as the standard `crypto/subtle` discipline.

Worth flagging: the file key is held in memory for the lifetime of
the `MmapBlob`. Long-lived mmap'd blobs = long-lived in-memory keys.
That's a tradeoff vs. the convenience of not re-prompting the user
for each access. The streaming `BlobReader` path has the same property
during a single read — but the read finishes and the key is gone.
Document this in the user-facing API: an `MmapBlob` over an encrypted
blob holds the file key until `Close()`.

### Combined encryption + compression

Out of scope for this doc. The pipeline today is `file → decrypt →
decompress → tee`. To support combined wrappers in the mmap path,
both layers would need chunked random access *and* their chunk
boundaries would have to align — which they generally don't. The
realistic combinations to support later:

1. Encryption only (this doc).
2. Compression only (would need a streamable-zstd format like `zstd`'s
   seekable extension; not a v2 concern).
3. Both — only if the encryption layer's chunks contain compressed
   plaintext that's also chunked at the same granularity. Possible
   but requires a coordinated change to the writer.

## Code placement (sketch)

```
go/internal/0/domain_interfaces/blob_store.go
    + interface RandomAccessDecryptor

go/internal/echo/env_dir/blob_config.go
    + Config.GetRandomAccessDecryptor(file *os.File) (RandomAccessDecryptor, error)
      // returns nil, nil if not applicable

go/internal/?/age_random/    (new, layer TBD — needs file-key access)
    age adapter

go/internal/?/pivy_ebox_random/    (new, gated on pivy chunking confirmation)

go/internal/foxtrot/mmap_blob/
    + promote_encrypted.go   //go:build linux
        — extended promotion: try identity, then random-access decryptor.
        — anon-mmap + uffd register + handler goroutine.
    + handler_linux.go
        — fault loop. Reuses the POC's pattern.
```

The non-Linux build of `mmap_blob` continues to support v1 strict-
wrapper promotion only. The encryption path is `//go:build linux`
throughout.

## Testing strategy

**Unit (no real keys):**
- Fake `RandomAccessDecryptor` with a known chunk-pad-XOR scheme.
  Tests the userfaultfd plumbing without depending on age/pivy.
- Tests cover: positive promotion, fault handling for each chunk,
  bitmap dedup, `Verify()` over the plaintext, `Close()` zeros the key
  buffer.

**Integration (real schemes, gated by environment):**
- age: generate an age key in `t.TempDir()`, encrypt a fixture, mmap,
  verify. Always-runnable; no user touch required.
- pivy ebox: PIV-touch-required. Tag with a build constraint or env-
  guard so CI doesn't block on hardware. Optional opt-in.

**Race + memory:**
- `go test -race` over the unit tests. Fault handler vs caller
  goroutine accessing the bitmap.
- Manual stress: mmap a 1 GiB encrypted blob, random-read 100k offsets,
  measure decrypt-call count. Should equal touched-chunk count.

## Rollback

Purely additive on top of v1. The encryption-mmap path is gated by:

1. The `RandomAccessDecryptor` capability — if no scheme satisfies it,
   `ErrMmapUnsupported` per v1 behavior.
2. The `//go:build linux` tag on the new files — non-Linux builds
   compile without the new path entirely.

To roll back: delete the new packages, revert the
`MakeMmapBlobFromBlobReader` extension. v1 promotion semantics restore
unchanged. No wire-format change, no on-disk migration, no public API
removal (the new `RandomAccessDecryptor` interface in layer 0 stays
unimplemented — harmless).

**Promotion criteria:** 30 days of in-tree use without callers
reporting:
- Mid-mapping decryption failures that aren't caught by the
  promote-time sniff.
- Locked-OS-thread exhaustion at typical use levels.
- Key-zeroization races (post-Close access to a `Bytes()` slice
  returning non-zero bytes from the freed key region).

## Out-of-scope follow-ups

- **Combined encryption + compression mmap path.** Coordinated chunk
  alignment between the layers, or eager-decrypt-then-decompress on
  fault.
- **Shared userfaultfd across many `MmapBlob`s** to bound OS-thread
  cost.
- **macOS via FSKit** (Sequoia 15.4+) — would need a Swift bridge per
  the platform survey in the v1 design.
- **CLI surface for encrypted-blob mmap.** Probably never useful (CLI
  stdout = streaming, mmap adds nothing). Do not pursue without a
  concrete consumer.

## Decision points the user must confirm before code

1. **age chunk format verification.** Is the 64 KiB / ChaCha20-Poly1305
   / 11-byte-counter-plus-flag-byte description correct for
   `filippo.io/age@v1.3.1`? If yes, age adapter proceeds. If no, the
   adapter's chunk arithmetic is rewritten to match.
2. **pivy ebox format.** Is the payload chunked AEAD? If yes, with
   what parameters? If no, pivy stays at `ErrMmapUnsupported` and this
   doc supports age only.
3. **Decryption-error policy.** Default proposal: sniff-at-promote +
   poison-on-mid-fault + caller-driven `Verify()`. Confirm acceptable.
4. **Key lifetime.** A key held in memory for the lifetime of an
   `MmapBlob` is a real tradeoff. Document, or add a `RekeyEvery`
   policy that re-prompts after N reads. Default: hold for lifetime.

Until items 1 and 2 are answered, this design is a **sketch**, not an
implementation plan.
