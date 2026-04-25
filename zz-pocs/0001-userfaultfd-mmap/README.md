# POC 0001: userfaultfd-driven lazy mmap of zstd-compressed payload

## What this proves

Standard file-backed `mmap(2)` cannot transform bytes on page-fault — the
kernel reads the literal file. So the question is whether we can serve a
caller a `[]byte` view whose bytes are **decompressed on demand** without
materializing the whole logical blob up front.

This POC demonstrates that yes, we can, on Linux, using `userfaultfd(2)`
with the `UFFD_USER_MODE_ONLY` flag (kernel ≥5.11). No `CAP_SYS_PTRACE`,
no `vm.unprivileged_userfaultfd=1`, no admin opt-in.

## Mechanism

1. Generate a deterministic 64 MiB plaintext where the byte at offset `i`
   is the low byte of `splitmix64(i)`. Verifiable by computing the
   expected byte from any offset, no random-access seeking required.
2. Compress in 1 MiB chunks (independent zstd frames). Write the
   concatenated compressed bytes to `payload.zchunks` and a sidecar
   `payload.idx` (`(N+1)` × `uint64` little-endian — chunk start offsets,
   final entry is EOF).
3. `mmap` 64 MiB anonymous (`MAP_PRIVATE|MAP_ANONYMOUS`, `PROT_READ`).
4. `userfaultfd(UFFD_USER_MODE_ONLY|O_CLOEXEC)` → ioctl `UFFDIO_API` →
   ioctl `UFFDIO_REGISTER` (mode `MISSING`) over the mapping.
5. Spawn a fault-handler goroutine. On each `UFFD_EVENT_PAGEFAULT`:
   identify the chunk, decompress it once, then `UFFDIO_COPY` the whole
   1 MiB chunk (256 pages) into the mapping. A 64-bit bitmap tracks
   which chunks are already populated.
6. Verifier: pick `N` random byte offsets, compare `mmapBytes[off]`
   against `splitmix64(off) & 0xff`. Any mismatch → exit 1.

## Why these choices

- **klauspost/compress/zstd**: pure-Go, simpler than madder's
  transitive `DataDog/zstd` (cgo). POC is its own module so this dep
  doesn't bind madder.
- **`UFFD_USER_MODE_ONLY`**: lets an unprivileged process register a
  mapping it owns. The default `vm.unprivileged_userfaultfd=0` only
  blocks *kernel-mode* fault handling; user-mode-only is allowed.
- **Per-chunk decompression on first fault, whole-chunk `UFFDIO_COPY`**:
  one decompression amortizes across 256 pages and the kernel discards
  any queued faults for already-populated pages.
- **Linux-only build tag**: the file-mmap → `[]byte` lazy-decompress
  story has no in-process equivalent on macOS. See the parent design
  conversation for the platform survey.

## How to run

```
cd zz-pocs/0001-userfaultfd-mmap
just run
```

Or directly:

```
go run .
```

Requires kernel ≥5.11 (for `UFFD_USER_MODE_ONLY`). Verify with
`uname -r`.

## Expected output

A successful run prints fixture stats, fault-handling stats, and
`POC OK` on the last line, exiting 0. Sample output is captured in
`results.txt`.

## Results

Confirmed on Linux 6.17.0-20-generic (Ubuntu 24.04 base) with
`vm.unprivileged_userfaultfd=0`. No `CAP_SYS_PTRACE`, no admin
opt-in. `go vet` clean. `go build -race` + run also exits 0.

```
kernel page size: 4096 bytes
fixture: 64 MiB plaintext, 64 chunks, compressed=67111232 bytes (gen 214ms)
uffd registered: base=0x7fffad000000 len=67108864
handler ready
verify: 4096 random reads in 29.793ms
faults handled: 64
decompressions: 64 (64 / 64 chunks touched)
POC OK
```

Note: the splitmix64-derived plaintext is high-entropy and does not
compress — output is 67_111_232 bytes vs 67_108_864 bytes input, i.e.
~37 bytes of zstd frame overhead per chunk. The POC tests the
page-fault mechanism, not the compression ratio. A real GGUF file
would compress meaningfully; that's a separate benchmarking concern.

## What this does NOT prove

- Performance under contended random-read workloads. A serious benchmark
  would compare userfaultfd-lazy vs. eager-decompress-into-`MAP_ANON`
  vs. plain streaming reads on a workload representative of GGUF
  random reads.
- Correctness under multiple concurrent reader goroutines. The handler
  is single-threaded; the verifier is single-threaded.
- Safety of the chosen bitmap synchronization (the handler is the only
  writer, but a more realistic design would use `UFFDIO_COPY` ordering
  guarantees more carefully).
- Anything about the production design — see parent design discussion
  for ownership transfer, wrapper-compat policy, and the
  `MakeMmapBlobFromBlobReader` shape.
