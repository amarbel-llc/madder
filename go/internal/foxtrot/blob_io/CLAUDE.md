# blob_io

Blob-IO machinery for content-addressable stores: codec config
(compression + encryption + hash format), streaming `Writer`/`Reader`,
the link(2)-based `Mover` that publishes blobs into the
content-addressable tree, and the byte-level collision check used on
the EEXIST branch.

Extracted from `internal/echo/env_dir` in May 2026 to free the `Config`
name in env_dir for the env-construction `Config` type proposed in
[#123]. The blob-* files in env_dir always carried a `// TODO move
into own package` comment; this is that move.

## Key types

- `Config` — codec bundle: hash format, path-join function,
  compression IO wrapper, encryption IO wrapper. Fed into every
  Writer/Reader/Mover. `DefaultConfig` is the no-compression,
  no-encryption identity bundle.
- `Writer` (returned by `NewWriter`) — wraps an `io.Writer` with the
  configured compression+encryption layers and a hash digester.
- `Reader` (returned by `NewReader`/`NewFileReaderOrErrNotExist`) —
  symmetric for reads. Supports the `MmapSource` capability
  (used by `mmap_blob`) when wrappers are identity.
- `Mover` (returned by `NewMover`) — `MoveOptions` carries the
  destination path and observer; `Close` performs the link(2)
  publish, with optional byte-level verification on EEXIST.
- `ErrBlobAlreadyExists` / `ErrBlobMissing` — the two store-side
  blob errors. `IsErrBlobAlreadyExists` / `IsErrBlobMissing`
  helpers wrap `errors.Is` against zero values.
- `MakeHashBucketPath*` / `PathFromHeadAndTail` /
  `MakeDirIfNecessary*` — path helpers re-exported from
  `dewey/delta/files`, plus the Markl-id-aware variant
  `MakeHashBucketPathFromMerkleId`.

## Layering

Sits at the foxtrot layer (alongside `blob_stores`, `mmap_blob`).
Imports only from layers ≤ delta (domain_interfaces, alfa, bravo,
delta packages, plus dewey libraries). Does not import env_dir.

## Consumers

- `internal/foxtrot/blob_stores` (local hash-bucketed + remote SFTP)
- `internal/foxtrot/mmap_blob` (test-only)
- `internal/india/commands` (`sync` uses `IsErrBlobAlreadyExists`)
- `internal/charlie/arg_resolver` (`NewFileReaderOrErrNotExist` for
  file-mode arg resolution)

## See also

- `docs/plans/2026-05-03-env-dir-multi-scope.md` (Step 0)
- ADR 0003 (link(2)-based publish, chmod-before-link)
- ADR 0004 (BlobWriteObserver wired through `MoveOptions`)
- Issue #31 (verify-on-collision, exercised via
  `MoveOptions.VerifyOnCollision` + `check_collision.go`)
