# sftp_probe

Pure-function probe library for `madder
sftp-analyze-and-suggest-configs`. No SFTP, no UI, no filesystem.
Takes bytes / typed configs in, returns verdicts.

## Key types

- `Stage` (enum: OK / Decrypt / Decompress / HashMismatch) — where
  in the read pipeline a verification attempt failed (or didn't).
- `Candidate` — one (compression, encryption) hypothesis. Holds
  both the `blob_store_configs.Config` (for emission to disk) and
  the `blob_io.Config` (for verification).
- `SampleResult` / `Aggregate` — per-attempt and per-candidate
  rollups.

## Key functions

- `EnumerateCandidates(layout, keys) []Candidate` — combinatorial
  cross-product of {none, gzip, zlib, zstd} × {none, age+keyᵢ}.
- `VerifySample(reader, expectedHex, candidate) SampleResult` —
  attempts decode through the candidate's pipeline, hashes the
  result, compares to expectedHex.
- `Rank(aggregates) []Aggregate` — sort by Verified desc, ties
  broken by stage diversity (single-stage failures rank above
  multi-stage flailers because they are more diagnosable).

## Design

See [`docs/plans/2026-05-08-sftp-analyze-and-suggest-configs-design.md`](../../../../docs/plans/2026-05-08-sftp-analyze-and-suggest-configs-design.md)
section "Detection model — verify, don't sniff".
