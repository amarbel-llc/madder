---
status: accepted
date: 2026-04-20
decision-makers: Sasha F
---

# Content-addressed blob writes may silently overwrite the destination

## Context and Problem Statement

`localFileMover.Close` (`go/internal/echo/env_dir/blob_mover.go:80-170`) writes a blob by streaming bytes into a temp file while computing the digest, then `os.Rename(tempPath, blobPath)` to commit. When two writers race on the same content — the expected case in a content-addressed store — they both produce the same digest and the same `blobPath`, and the second `rename` atomically replaces the first.

The code carries an `ErrorOnAttemptedOverwrite` flag on `MoveOptions` that was meant to return `ErrBlobAlreadyExists` when a blob already exists at `blobPath`. On Linux the check is misplaced — `rename(2)` never fails just because the destination exists, so the fallback branch at `blob_mover.go:146-158` only fires on cross-filesystem rename and the flag is effectively dead code. `ErrorOnAttemptedOverwrite` is also never set to `true` anywhere in the repository, and `MakeErrBlobAlreadyExists` has no other callers.

Before removing the flag we need to commit, in writing, to the underlying semantic: **for this blob store, is it always safe for a successful write to replace an existing blob at the same path?** Without that commitment we can't know whether future weak-hash modes, adversarial-input paths, or audit requirements will want the check back.

## Decision Drivers

* Concurrent writes producing the same digest are the expected behaviour in a content-addressed store and should not need caller-side coordination.
* `rename(2)` semantics on Linux (silent replace) differ from what `ErrorOnAttemptedOverwrite` implies on darwin/portable code; keeping the flag advertises a guarantee the implementation cannot make.
* Any written contract should hold under the hash algorithms madder currently supports and should spell out the conditions that would invalidate it.
* Industry precedent for content-addressed stores is overwhelmingly "write is idempotent" — git objects, restic, IPFS, Plan 9 Venti — and tooling interop is easier if madder matches.

## Considered Options

1. **Declare overwrite-is-fine and retire the flag.** Document the four conditions under which same-digest-equals-same-bytes holds, delete `ErrorOnAttemptedOverwrite`, `errorOnAttemptedOverwrite`, the branch at `blob_mover.go:146-158`, and `MakeErrBlobAlreadyExists`. Revisit if a weak-hash mode is ever added.
2. **Make the flag actually work.** Stat before rename (portable, TOCTOU, but harmless for CAS since any racing writer produces identical bytes) or use `renameat2(RENAME_NOREPLACE)` on Linux + `renamex_np(RENAME_EXCL)` on darwin for true atomic fail-if-exists.
3. **Use link-based writes.** `link(tempPath, blobPath)` gets atomic fail-if-exists (`EEXIST`) for free and would give first-writer-wins semantics implicitly. But `link(2)` cannot cross filesystems and madder's `tempFS` is not co-located with the blob store, so this requires a larger refactor. Tracked separately in [#30](https://github.com/amarbel-llc/madder/issues/30).

## Decision Outcome

Chosen option: **"Declare overwrite-is-fine and retire the flag"**, because the four preconditions all hold in madder today, the flag is unused dead code that advertises a guarantee the implementation doesn't deliver on Linux, and every industry peer with strong-hash content addressing makes the same call.

The guarantee is: **for any `localHashBucketed` blob store with a strong (collision-resistant) hash, a successful write to `blobPath` may silently replace an existing file at `blobPath`, and readers will always observe bytes whose digest equals `blobPath`.**

This guarantee holds iff all four of the following are true, which they are today:

1. **The configured hash is collision-resistant.** madder supports only `sha256` (`FormatIdHashSha256`) and `blake2b256` (`FormatIdHashBlake2b256`). A concurrent writer cannot produce different bytes that map to the same `blobPath` without breaking the hash.
2. **The digest is computed by the writer from the bytes being written, not supplied by the caller.** `env_dir.NewWriter` streams bytes through the hash; the digest returned by `GetMarklId()` is bound to the actual file contents. A writer cannot lie about content.
3. **Partial writes never reach `blobPath`.** If a writer crashes mid-stream the digest is never finalised, `blob_mover.go:115` short-circuits on null, rename never happens, and the temp file is orphaned. Crash recovery cannot produce wrong-content-at-right-path.
4. **Metadata is not semantically load-bearing.** mtime, ctime, and inode identity change on overwrite. Nothing in madder currently treats those as stable. If audit-trail "first writer wins" semantics are ever required, this decision must be revisited — silent overwrite loses that signal.

If a future hash mode is added that does not satisfy condition (1) — e.g. a short non-cryptographic hash for legacy interop — the overwrite check must be re-introduced *for that mode*, gated on the hash family.

### Consequences

* Good, because same-digest races between writers succeed silently without caller-side coordination, matching the semantics callers already rely on implicitly.
* Good, because the dead `ErrorOnAttemptedOverwrite` flag, the unreachable branch at `blob_mover.go:146-158`, and the unused `MakeErrBlobAlreadyExists` are retired — the type no longer advertises a guarantee it can't keep.
* Good, because the contract is now explicit: a future reviewer can check the four conditions before adding a hash mode, rather than inferring intent from Linux-specific rename behaviour.
* Neutral, because `os.Rename` already exhibits this behaviour on Linux today; the written contract matches the existing runtime behaviour rather than changing it.
* Bad, because first-writer-wins semantics are deliberately not exposed. If metrics or audit paths ever need that signal, they must either read-and-compare before writing (accepting the TOCTOU) or wait for the hardlink migration in [#30](https://github.com/amarbel-llc/madder/issues/30), which recovers first-writer semantics via `EEXIST`.

### Confirmation

* Concurrent-write test coverage (`TestConcurrentBlobWrites{SameContent,DistinctContent,Mixed}` in `go/internal/foxtrot/blob_stores/`, run under `go test -race`) exercises the same-digest race and the distinct-digest case, documenting the behaviour this ADR commits to.
* `rg ErrorOnAttemptedOverwrite` should return zero hits after the flag is removed; if it reappears, the author must reconcile with this ADR.

#### Industry precedent

Each entry below was verified for this ADR by inspecting the cited source. Entries the author could not verify from a primary source are not included.

* **git** — `object-file.c` → `finalize_object_file_flags()`. The loose-object finalize path calls `link(tmpfile, filename)` first. On `EEXIST` it invokes `check_collision(tmpfile, filename)` to byte-compare the existing object against the newly written one before treating the duplicate as a no-op. When `link` is not supported by the filesystem (Coda, FAT), git falls back to `rename(2)`. Git is **more** paranoid than madder's plan here: it verifies content equality even when the hash matches, because git targets environments where sha1 collisions have been demonstrated (Shattered, 2017). madder targets sha256 and blake2b256, where no collision is known, so the byte-compare step is omitted. If a weak-hash mode is ever added, revisit this stance and the byte-compare option tracked in [#31](https://github.com/amarbel-llc/madder/issues/31).
* **restic** — `Repository.SaveBlob(ctx, blobType, buf, id, storeDuplicate)` (godoc). The `storeDuplicate` parameter defaults to `false`; when a blob with that content hash is already known, the save is skipped and the return value `known` is set to `true`. Duplicate detection is an expected code path, not an error. restic exposes the known/unknown signal to callers via the return tuple, while madder does not expose it today — [#30](https://github.com/amarbel-llc/madder/issues/30) (hardlink migration) would recover an equivalent signal via `EEXIST`.
* **IPFS blockstore (go-boxo)** — `blockstore/blockstore.go` → `Put()`. The default path calls `Has()` first ("Has is cheaper than Put, so see if we already have it") and returns `nil` if the block already exists ("`if err == nil && exists { return nil // already stored. }`"). The `WriteThrough(true)` option bypasses the existence check and writes unconditionally — effectively "overwrite is fine" when callers opt in. This is structurally identical to restic's `storeDuplicate` escape hatch.
* **OSTree** — `libostree/ostree-repo-commit.c` → `commit_path_final()`. When finalising an object, an `EEXIST` from the atomic-commit syscall is treated as "someone else got here first": the error is not propagated, and the caller's cleanup path unlinks the temp file ("`if (errno != EEXIST) return glnx_throw_errno_prefix(error, ...); /* Otherwise, the caller's cleanup will unlink+free */`"). The `EEXIST`-returning syscall implies OSTree uses a link-based finalize (like git's primary path), not plain `rename(2)`.
* **Plan 9 Venti** — Quinlan & Dorward, *Venti: A New Approach to Archival Storage* (USENIX FAST 2002), and the venti(6) / venti(8) man pages. Venti is a write-once read-many store keyed by SHA-1 fingerprint; writes are idempotent by design and duplicate blocks are transparently coalesced. Venti's write-once policy goes further than madder — a block, once stored, cannot be modified at all — but the relevant similarity is that writing the same fingerprint twice is a no-op rather than an error.

The takeaway: every production content-addressed store the author surveyed treats same-digest writes as at worst idempotent. Two (restic, IPFS) expose an optional "write anyway" override, suggesting the skip-by-default semantic is standard but occasionally needs an escape hatch. One (git) goes further and byte-compares on collision — a hedge against weak hashes. madder's choice to retire `ErrorOnAttemptedOverwrite` is consistent with the skip-by-default convention, conditional on the four preconditions above continuing to hold. The byte-compare hedge and an optional "write anyway" path are captured separately as future work ([#31](https://github.com/amarbel-llc/madder/issues/31)).

## Pros and Cons of the Options

### Declare overwrite-is-fine and retire the flag

* Good, because the invariant is stated up front with its preconditions rather than inferred from platform rename semantics.
* Good, because dead code is deleted — there is no type-level advertisement that the implementation doesn't deliver.
* Good, because it matches the behaviour of every comparable content-addressed store the author surveyed (git, restic, IPFS, Venti).
* Neutral, because the runtime behaviour on Linux is unchanged.
* Bad, because it forecloses first-writer-wins auditing without an explicit design for it. Mitigated by [#30](https://github.com/amarbel-llc/madder/issues/30).

### Make the flag actually work

* Good, because the type-level advertisement would match runtime behaviour.
* Good, because it would preserve an escape hatch for a future weak-hash mode.
* Bad, because the flag is currently unused — building the mechanism is speculative work for a caller that does not exist.
* Bad, because on Linux the portable approach (stat-before-rename) has TOCTOU that doesn't matter for CAS, and the atomic approach (`renameat2(RENAME_NOREPLACE)`) requires platform-specific syscall wiring.

### Use link-based writes

* Good, because `link(2)` gives `EEXIST` on collision portably across Linux and darwin, recovering first-writer semantics without extra machinery.
* Good, because it also eliminates the chmod-race in [#29](https://github.com/amarbel-llc/madder/issues/29).
* Bad, because `link(2)` cannot cross filesystems and madder's `tempFS` (`XDG_CACHE_HOME/madder/tmp-{pid}`) is not co-located with the blob store (typically under `XDG_DATA_HOME`), so a naive switch breaks on systems with separate cache and data mounts.
* Bad, because correctly landing this requires per-store temp directories and migration of existing `GetTempLocal` callers — larger than the concurrent-write hardening this ADR is scoped with. Tracked as [#30](https://github.com/amarbel-llc/madder/issues/30).

## More Information

* Investigation thread began from auditing madder's docs for concurrency-safety coverage and finding the `ErrorOnAttemptedOverwrite` fallback at `blob_mover.go:146-158` unreachable on Linux.
* Related work: [#29](https://github.com/amarbel-llc/madder/issues/29) (chmod race in `lockFile` path, deferred), [#30](https://github.com/amarbel-llc/madder/issues/30) (hardlink migration, deferred).
* Hashes currently in use: `FormatIdHashSha256`, `FormatIdHashBlake2b256` — see `go/internal/bravo/markl/format.go:29-30`.
