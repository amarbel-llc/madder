---
status: accepted
date: 2026-04-22
decision-makers: Sasha F
---

# Blob-store writes use `link(2) + unlink(2)` against a per-store temp directory

## Context and Problem Statement

[ADR 0002](0002-content-addressed-overwrite-is-fine.md) committed to `os.Rename(tempPath, blobPath)` as the publish primitive for content-addressed blob writes. That works, but it carries three rough edges that all become visible once more than one writer races on the same digest:

1. **Chmod race.** If a blob is supposed to end up read-only, the ordering is `rename → chmod`. A second writer's `rename(2)` atomically swaps in a fresh writable inode, opening a transient window where the intended read-only mode is absent. ([#29](https://github.com/amarbel-llc/madder/issues/29).) Worst case: the second writer crashes between `rename` and `chmod` and leaves a writable blob.
2. **No first-writer-wins signal.** `rename(2)` silently replaces the destination. The write path can't distinguish "new" from "duplicate" to feed metrics, audit, or dedup accounting.
3. **Inode churn.** A reader that holds an open FD across a concurrent same-digest write sees its inode swapped out from under it. Harmless for correctness under ADR 0002, but surprising.

## Decision Drivers

* All three issues disappear if the publish primitive is `link(2)` instead of `rename(2)`: the second writer gets `EEXIST` and never touches the first writer's inode.
* `link(2)` is portable across Linux and darwin. No syscall fallback logic needed.
* `link(2)` cannot cross filesystems (`EXDEV`). The global temp dir lives at `$XDG_CACHE_HOME/dodder/tmp-{pid}/` (or its CWD-scoped equivalent for `.`-prefixed blob-store-ids) while blob stores live under `$XDG_DATA_HOME/madder/blob_stores/{id}/`. In the default user layout both resolve under `~/` and share a mount; most deployments satisfy this invariant.
* Content-addressed stores should not have mutable blobs. Per ADR 0002 the bytes never change, so the mode bits never need to either. Write-once, read-many is the shape of the data.

## Considered Options

1. **Rename + chmod-before-rename.** Chmod the temp file read-only *before* `rename`, so the destination inode is born read-only. Closes issue 1. Does nothing for issues 2 or 3.
2. **`renameat2(RENAME_NOREPLACE)` / `renamex_np(RENAME_EXCL)`.** True atomic fail-if-exists. Closes all three but adds platform-specific syscall paths.
3. **`link(2) + unlink(2)` with per-store colocated temp dir.** Closes all three. Moves temp files under `<basePath>/.tmp-{pid}/` so `EXDEV` is structurally impossible, but pollutes the blob-store directory and forces `AllBlobs` walkers to skip dot-dirs.
4. **`link(2) + unlink(2)` reusing the existing XDG_CACHE_HOME tempdir.** Closes all three. Relies on the invariant that XDG_CACHE_HOME and XDG_DATA_HOME share a filesystem. Violations surface as a clear `EXDEV` error at first write, not silent data loss.

## Decision Outcome

**Chosen: option 4.** The publish primitive for local hash-bucketed blob stores is now:

1. `os.Chmod(tempPath, 0o444)` — set read-only on the temp inode *before* linking.
2. `os.Link(tempPath, blobPath)` — atomic publish.
3. On `errors.Is(err, fs.ErrExist)`: duplicate write, ADR 0002 guarantees equivalence. `os.Remove(tempPath)`, return nil.
4. On `errors.Is(err, syscall.EXDEV)`: temp and blob store are on different filesystems. Return a wrapped error pointing to this ADR and `blob-store(7)` so the user can remediate (colocate XDG_CACHE and XDG_DATA, or report if their layout is one we should support).
5. On success: `os.Remove(tempPath)` to release the temp path (blob keeps its own link).
6. `fsyncDir(filepath.Dir(blobPath))` for directory-entry durability.

The `tempFS` for `localHashBucketed` stores remains the existing `envDir.GetTempLocal()` — rooted at `$XDG_CACHE_HOME/dodder/tmp-{pid}/` or, for `.`-prefixed blob-store-ids, its CWD-scoped override. Cleanup is already handled by `envDir.resetTempOnExit`. No new directory appears inside the blob store, and the `AllBlobs` walkers do not need special-case filtering.

### Why not option 3 (per-store colocated temp dir)

Option 3 was implemented first and rejected during integration:

* Added a visible `.tmp-{pid}/` subdirectory to the blob-store on disk, violating the "blob-store directory contains only hash buckets + config" mental model.
* Forced `AllBlobs` (`util_local.go`) to filter dot-prefixed directories so the fsck/pack/sync iteration didn't mistake the temp dir for a hash bucket. That filter is a new invariant to maintain.
* Required per-store cleanup registration via `envDir.GetActiveContext().After(...)`.

Option 4 avoids all three for the cost of a precondition we already state in the manpage and enforce with a loud error.

### Reevaluation trigger

Switch to option 3 if any of the following become true:

* A significant share of users (CI containers, per-service XDG layouts, NAS-backed data) report the `EXDEV` error in practice.
* A supported deployment model intentionally separates XDG_CACHE_HOME onto tmpfs while XDG_DATA_HOME persists to disk.
* `env_dir.GetTempLocal` gains a non-blob-store caller for which colocation with a specific blob store is inappropriate.

### Consequences

* Published blobs are mode 0o444 from birth. No chmod-after-publish step, no transient writable window. Closes [#29](https://github.com/amarbel-llc/madder/issues/29).
* `DeleteBlob` still works: `os.Remove` requires write permission on the *parent directory*, not the file. 0o444 blob files can be unlinked normally.
* Same-digest concurrent writers see `EEXIST` on `link(2)` instead of a silent overwrite. First-writer-wins is now a visible signal the code can optionally surface (not yet plumbed; logging / metrics left as follow-up).
* Readers holding open FDs across a same-digest write see a stable inode — the first writer's file is never replaced.
* The `LockInternalFiles` configuration flag is retired. It was never honored (the chmod it gated was TODO'd out since the darwin origin of the code) and its role — "make internal files read-only" — is now the unconditional default. Old configs containing `lock-internal-files = true|false` still parse; the key goes through to `Undecoded()` silently and is ignored.
* Setups where `$XDG_CACHE_HOME` and `$XDG_DATA_HOME` are on different filesystems (container layouts with tmpfs cache, some NAS setups) now fail loudly with an `EXDEV` error on first blob write. The error references this ADR and `blob-store(7)` so the user can remediate. See "Reevaluation trigger" above.
* Test fixtures get both `basePath` and `tempPath` from `t.TempDir()`, which places them under the test runner's tmpdir — same filesystem, so `link(2)` works without special setup.

### Confirmation

* `just test-go-race -run TestConcurrent ./internal/foxtrot/blob_stores/...` passes clean (no race warnings) across the same-content, distinct-content, and mixed-payload concurrent-write tests.
* `TestConcurrentBlobWritesSameContent` asserts the published blob mode is 0o444 and the per-store temp dir is empty after 32 concurrent writers complete.

## Preconditions (from ADR 0002, still load-bearing)

All four conditions spelled out in ADR 0002 continue to hold. The migration does not weaken any of them:

1. Hash is collision-resistant (SHA256 or Blake2b256 only).
2. Digest is computed by the writer from the bytes written, not supplied by the caller.
3. Partial writes never reach `blobPath` (temp file + `link` atomicity, with `fsync` on temp before `link`).
4. Metadata (mtime, mode, inode) is not semantically load-bearing — readers identify blobs by their `blobPath` alone.

Under these preconditions, a `link(2)` that returns `EEXIST` is equivalent to a successful write: the destination already holds bytes whose digest equals the path.
