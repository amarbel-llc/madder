---
status: exploring
date: 2026-04-28
promotion-criteria: |
  Promote to `proposed` once one design path is selected and an
  implementation milestone is identified. Selection requires:

  1. A user need that's blocked by the absence of remote pack
     (today: nobody has explicitly asked for it; SFTP loose-only
     mode is sufficient for current load-bearing workflows).
  2. Agreement on how the chosen path interacts with future
     server-side operations (verify, gc, repair) — i.e. whether
     this is a one-off pack feature or the first step toward a
     more general remote-worker model.
---

# Remote inventory-archive packing

## Problem Statement

Madder's `inventory-archive` blob store packs many small loose blobs
into a small number of indexed archive files for efficient storage and
O(1) lookup. The implementation hard-codes local-filesystem operations
(`os.MkdirAll`, `os.CreateTemp`, `os.Open`, `filepath.Glob`,
`os.Rename`), so an inventory-archive whose loose store is SFTP-backed
cannot publish or maintain its archive layer. Operators who chose SFTP
to share a content-addressed store across machines therefore lose
access to packing — which is exactly the optimization the load-heavy
SFTP workflows would benefit from most.

The `madder pack` subcommand currently silently produces a misshapen
or partially-functional store when invoked against an SFTP-backed
inventory archive. madder#55 and madder#29 surfaced this gap;
v0.3.0's bats parity work cleared write/read/sync/fsck on SFTP but
deliberately deferred pack pending a design call.

This FDR captures the four paths considered, their tradeoffs, and the
gating questions that determine which path (or sequence of paths)
makes sense. It is **explicitly not** committing to any of them for
v0.3.0; the goal is to fix the design space in writing so a future
release can pick a direction with full context.

## Considered Paths

### Path A — Filesystem interface seam

Lift `inventoryArchiveV1`'s I/O onto a small `Filesystem` (or
similar) interface: `MkdirAll`, `CreateTemp`, `Open`, `Glob`,
`Rename`, `Remove`, `Stat`. The local implementation wraps `os.*`;
an SFTP implementation wraps `sftpClient.*`. Plumb the chosen
implementation through `inventoryArchiveV1` at construction time
based on the loose-store's transport.

**Engineering shape:** ~300-500 lines touched across `pack_v1.go`,
`store_inventory_archive_v1.go`, `pack_parallel.go`, and a new
`archive_filesystem.go` defining the seam plus its two
implementations.

**Strengths.** Faithful translation. No behavioral change for local
stores. Single code path for both transports. Tests for one
implementation give confidence in both via the seam contract.

**Weaknesses.** Largest refactor of the four. Pushes pack
concurrency semantics onto SFTP (no advisory locking; non-POSIX
`Rename` semantics on some servers; concurrent packers competing
for the same set of loose blobs). Read-during-write protection
relies entirely on SFTP rename atomicity, which varies. The
`rebuildIndex` path re-fetches every `.index` file over the wire on
cache miss — slow but correct.

### Path B — Refuse SFTP-backed inventory archives

Detect at `MakeBlobStore` that the loose-blob-store-id resolves to
an SFTP store and refuse with a clear error citing this FDR and a
follow-up issue. Document the limitation; close the immediate gap
as "won't fix in this version."

**Engineering shape:** ~30 lines + a bats refusal scenario.

**Strengths.** Cheapest by an order of magnitude. Removes the
silent-misbehavior footgun. Buys time for the bigger design call.
Loose-only over SFTP remains useful for many workflows.

**Weaknesses.** Doesn't actually deliver remote pack. Operators
who want the optimization still don't get it. May surprise users
who expected the local feature to "just work" remotely.

### Path C — Local build + remote publish

Build the archive locally — same `pack_v1.go` machinery as today,
just staging into a local scratch directory — then publish the
finished `<checksum>.data` and `<checksum>.index` over SFTP using
the same atomic-rename pattern that `WriteRemoteConfig` already
uses. Local cache (under `$XDG_CACHE_HOME`) stays per-client. Read
seam still needs `sftpClient.ReadDir` + `sftpClient.Open` for
archives written by other clients.

**Engineering shape:** ~150-300 lines: a `publishArchive` helper, a
`BlobStorePublisher` (or transport-aware archives path) capability,
plus a small SFTP-side read seam (`ListArchiveIndexes`,
`OpenArchiveIndex`, `OpenArchiveData`).

**Strengths.** No interface refactor. Pack code stays local-FS
bound. Symmetric with `WriteRemoteConfig` — operators already know
the shape. Content addressing + per-file atomic rename collapse the
concurrency story: two clients packing the same loose blobs
produce identical bytes; the rename collision is idempotent. Local
cache is invalidation-free in the steady state.

**Weaknesses.** Requires local scratch space ~archive-sized. Users
who chose SFTP precisely because they don't have local disk are
out of luck. Bandwidth: every loose blob still has to come local
once during pack (same as Path A). Read seam is still needed —
narrowed to two operations vs all of `os.*`, but not zero.
Operations beyond pack (verify, gc, repair) are not addressed.

### Path D — Remote worker

A richer-than-SFTP transport where the remote runs a madder process
exposing pack/verify/gc/repair as RPCs. Three plausible substrates:

- **D1 — `madder daemon --stdio` over SSH.** Spawn the binary
  remotely on demand, speak a wire protocol on stdin/stdout. Auth
  is just SSH; deployment is "copy the binary to remote $PATH." No
  open ports, no daemon process to monitor.
- **D2 — long-running gRPC service.** Real daemon, listening port,
  mTLS or token auth. Better for multi-tenant workers but bigger
  ops surface.
- **D3 — object-store native (S3-compatible).** Use the storage
  layer's own server-side concatenate / copy primitives. Different
  shape — no "worker" per se, the storage itself does the heavy
  lifting.

**Engineering shape:** D1 is ~1500-3000 lines including wire
protocol, daemon mode, client-side dispatcher, ADR + ops docs.
D2/D3 are larger.

**Strengths.** Bandwidth: zero loose-blob round-trips during pack
— the work happens server-side where the loose blobs already are.
Single source of truth for pack output, no concurrency races. The
worker becomes the natural home for verify, gc, repair, integrity
scrub — currently client-only operations that suffer the same
"read every blob over the wire" problem pack does. Solves a
broader class of problems with one mechanism.

**Weaknesses.** Largest design and ops surface. Wire-protocol
versioning becomes a long-tail commitment. Many sysadmin policies
allow `sshd` but not running custom daemons. Worker mode requires
the remote operator to run a madder process they trust — different
trust model from SFTP-as-dumb-storage.

## Comparison

| Dimension | A (FS seam) | B (refuse) | C (local build) | D (worker) |
|-----------|-------------|------------|-----------------|------------|
| Loose-blob bandwidth during pack | N round-trips | n/a | N round-trips | 0 — work is local to blobs |
| Local scratch needed | none | n/a | ~archive size | none |
| Concurrency story | SFTP-dependent | n/a | bounded by content-addressed atomic rename | server is single source of truth |
| Sysadmin requirements | sshd | sshd | sshd | sshd + madder binary on remote |
| Design weight | medium | light | medium | heavy (new wire protocol, daemon lifecycle) |
| Code volume | ~300-500 LoC | ~30 LoC | ~150-300 LoC | ~1500-3000 LoC + ADR + ops docs |
| Beyond pack | no | no | no | yes — verify, gc, repair all become server-side |
| Breaks if SFTP rename non-atomic | yes | n/a | partial — only on cross-client conflict | no |

## Layering and Compatibility

Paths C and D are **not exclusive**. A future D-style worker can
expose `Pack` as an RPC; clients with a worker available route there,
clients without one fall back to C (local build, remote publish).
The two layer naturally:

1. **Path C as a near-term feature** delivers remote pack for the
   common case without precommitting to a daemon.
2. **Path D1 as a follow-up** adds the worker mode when verify, gc,
   or repair become priorities. The inventory-archive store grows a
   third constructor: `local`, `remote-via-publish` (Path C),
   `remote-via-worker` (Path D).

Path B is compatible with all of the above: any of A/C/D supersedes
the refusal.

## Gating Questions

Before promoting this FDR to `proposed`, answer:

1. **Is there a concrete user demand for remote pack?** SFTP
   loose-only mode is shipping in v0.3.0 and may be sufficient for
   the current load-bearing workflows. If no operator is asking for
   pack, Path B (refuse + document) may be the indefinite resting
   state.
2. **Are verify / gc / repair on the roadmap?** If yes, every one
   of those features will hit the same "read every blob over the
   wire" wall pack does, and they all share Path D as the right
   end-state. That changes the calculus toward D1 sooner rather
   than later.
3. **What's the local-scratch tolerance?** For Path C to be useful,
   operators need to have local disk at least as big as the archive
   they're producing. If a meaningful fraction of SFTP users are
   space-constrained locally, C alone isn't enough.
4. **Does the deployment model allow a daemon?** D1 needs the
   madder binary on the remote and SSH access. If the remote is a
   managed SFTP-only host (e.g. some NAS appliances, some hosted
   storage services), D is structurally unavailable.

## Limitations

This FDR is in `exploring` status. It does **not** propose an
implementation, schedule, or interface — only the problem space and
the candidate paths. Concrete interfaces (flag names, error
messages, on-disk layout) are deferred to a per-path FDR or ADR
once a path is selected.

## More Information

- madder#55 — SFTP bats parity (the umbrella issue that surfaced
  this gap).
- madder#29 — explicit "SFTP pack parity" follow-up tracking the
  unimplemented work.
- ADR 0005 — remote-driven SFTP blob stores. Establishes the
  "remote owns its config" principle that Path C inherits.
- `go/internal/foxtrot/blob_stores/pack_v1.go` — current pack
  implementation. The `os.*` calls there enumerate the surface
  Path A would have to abstract.
- `go/internal/foxtrot/blob_stores/discover.go` —
  `WriteRemoteConfig` is the existing precedent for "build
  artifact locally, publish atomically over SFTP" that Path C
  generalizes.
