---
status: proposed
date: 2026-04-28
promotion-criteria: |
  Promote to `experimental` once the implementation lands and the bats
  conformance suite for [RFC 0003 §Conformance Testing] passes. Promote
  to `accepted` after the v0.6.0 milestone ships and the feature has
  shipped in at least one tagged madder release.
---

# tree-restore

## Problem Statement

`madder tree-capture` writes a content-addressable receipt that records
every entry of a captured directory tree. Today there is no way to
reconstruct that tree from a receipt: a user with a receipt id has no
in-tree command that materializes the captured files, directories, and
symlinks back onto disk. This feature adds the inverse —
`madder tree-restore <receipt-id> <dest>` — covering the consumer side
of the operational contract specified by [RFC 0003] §Consumer Rules.

The absence of a consumer also leaves [RFC 0003]'s store-hint
mechanism unused: receipts now carry `- store/<id> < <markl-id>`
metadata (per #92), but no in-tree code consumes it. `tree-restore` is
the first consumer that needs to resolve the source store, validate
config drift, and surface the diagnostics RFC 0003 §Store-Hint
Resolution specifies.

## Interface

### Synopsis

    madder tree-restore [-store <id>] <receipt-id> <dest>

### Positional arguments

- `<receipt-id>` — the markl-id of a `madder-tree_capture-receipt-v1`
  blob. Required.
- `<dest>` — the directory the captured tree will be materialized
  under. MUST NOT exist at invocation time. Required.

### Flags

- `-store <id>` — explicit blob-store-id to resolve `<receipt-id>` and
  every entry's `blob_id` against. Overrides the receipt's store-hint
  resolution. When omitted, the consumer follows
  [RFC 0003] §Store-Hint Resolution.

### Exit codes

- `0` — restore completed; `<dest>` exists with the captured tree
  materialized under it.
- nonzero — restore was refused or aborted; `<dest>` does NOT exist
  on disk (no partial materialization). Diagnostics on stderr.

### Behavior

Materialization happens in three phases:

1. **Resolve store + parse receipt.** Use `-store` if set, else the
   active default store. Open the receipt blob. Parse via
   `tree_capture_receipt.Coder.DecodeFrom` (the dispatcher in #87
   step 2). The result is a `*V1` carrying optional `Hint` and
   `Entries`.

2. **Resolve effective store via hint.** Per [RFC 0003] §Store-Hint
   Resolution: see [Store-hint resolution](#store-hint-resolution).

3. **Validate then materialize.** Per [RFC 0003] §Path Sanitization
   and §Per-Type Materialization: see [Sanitization](#sanitization)
   and [Per-type materialization](#per-type-materialization). Refusal
   from validation aborts before any disk write; refusal from
   materialization (e.g. mid-stream blob read failure) leaves a
   partially-written tree only as a last resort and emits a
   diagnostic naming the failed entry.

#### Destination preconditions

Per [RFC 0003] §Consumer Rules §Destination Preconditions:

- `<dest>` MUST NOT exist when `tree-restore` is invoked. The consumer
  creates `<dest>` as part of restore.
- `os.Lstat(dest)` is checked before any other work. If it returns nil
  error (i.e. the path exists), refuse with:

      error: <dest>: destination already exists
      hint: choose a destination that does not exist, or remove this one

  Exit nonzero. No store reads, no blob fetches.

#### Sanitization

Per [RFC 0003] §Consumer Rules §Path Sanitization, for each entry
`e`, the consumer computes:

    materialized := filepath.Clean(filepath.Join(dest, e.root, e.path))

The consumer MUST refuse the entire receipt — without leaving any
partial materialization on disk — if any of the following hold for any
entry:

1. `materialized` is not equal to `filepath.Clean(dest)` and is not
   lexically rooted under `filepath.Clean(dest) + os.PathSeparator`.
2. `e.root` or `e.path` contains a NUL byte (`\x00`).
3. `e.root` is the empty string.

Refusal happens BEFORE `<dest>` is created, before any blob is opened,
and before any byte is written. Diagnostics:

- Path escape:

      error: entry escapes destination
        root: <e.root>
        path: <e.path>
        materialized: <materialized>
        destination: <filepath.Clean(dest)>

- NUL byte:

      error: entry contains NUL byte
        root: <quoted_e.root>
        path: <quoted_e.path>

- Empty root:

      error: entry has empty root
        path: <e.path>

In every refusal case, exit nonzero and DO NOT create `<dest>`.

Newlines (`\n`, `\r`) and other valid-UTF-8 characters in `e.root` and
`e.path` MUST be permitted. Per [RFC 0003] §Consumer Rules, the
consumer MUST NOT reject an entry on the basis of its name containing
newlines or other unusual-but-legal characters. Tested by
`tree_restore_round_trips_unusual_filenames` (deferred — see
[Limitations](#limitations)).

#### Per-type materialization

Per [RFC 0003] §Consumer Rules §Per-Type Materialization, for each
entry `e` accepted by [Sanitization](#sanitization):

| `e.type` | Materialization |
|---|---|
| `file` | Resolve `e.blob_id` against the resolved store. Open `materialized` for write (create-only, `os.O_WRONLY|os.O_CREATE|os.O_EXCL`, mode `0o666` modulo umask). Stream the blob via `io.Copy(file, blobReader)` — `BlobReader` provides `io.WriterTo`. On success, `os.Chmod(materialized, e.mode & 0o777)`. |
| `dir` | `os.MkdirAll(materialized, e.mode & 0o777)`. The MkdirAll behavior is acceptable here because §Sanitization guarantees `materialized` is rooted under `dest` and the receipt has a `dir` entry for every ancestor dir per the producer's walk. |
| `symlink` | `os.Symlink(e.target, materialized)`. The mode field MUST be ignored. The target string is the literal value from `e.target` — NOT resolved, NOT cleaned, NOT validated against the destination's containment. (See [Limitations](#limitations) on symlink-following.) |
| `other` | Skip with a notice: `notice: skipping entry of type "other": <materialized>`. The consumer MUST NOT attempt to recreate devices, FIFOs, sockets, or other special files. |

Streaming for `file`: the consumer MUST NOT buffer file content in
memory. The `BlobReader` returned by `MakeBlobReader` is required to
satisfy `io.WriterTo`; the consumer uses `io.Copy(dst, blobReader)`
which prefers `WriteTo` when available.

The consumer MUST NOT preserve mtime, atime, owner, group, xattrs, or
ACLs.

#### Store-hint resolution

Per [RFC 0003] §Store-Hint Resolution, the consumer determines which
configured blob store to use for resolving entry `blob_id` references
via the following procedure (after the receipt has been parsed):

1. **`-store` flag wins.** If `-store <id>` is set, use that store. No
   warning, no fallback. The flag suppresses every branch below.
2. **Hint present, store configured, config-blob hash matches** — use
   the hinted store, no prompt:

       (no diagnostic; restore proceeds silently against the hinted store)

3. **Hint present, store configured, config-blob hash differs** —
   refuse without `-store` override:

       warning: store <hint.StoreId> has been re-configured since this
       receipt was written
         receipt config-hash: <hint.ConfigMarklId>
         current config-hash: <local-config-markl-id>
       error: pass -store <id> to override and use the current store
       hint: re-running with -store <hint.StoreId> uses the current
       configuration

   Exit nonzero. No materialization.

4. **Hint present, store NOT configured locally** — fall back to the
   active store with a notice:

       notice: receipt names store "<hint.StoreId>" which is not configured locally
       notice: falling back to active store

5. **No hint** — use the active store with a notice:

       notice: receipt carries no store hint
       notice: falling back to active store

The receipt blob ITSELF is fetched in phase 1 via the same store
resolution as the entries' blobs. If the receipt is in store A and the
entries reference store B (per the hint), this is a non-issue today
because all entries in a single receipt come from a single store-group
per RFC 0003's producer rules.

The local config-blob markl-id is computed the same way `tree-capture`
computes the hint at write time (see #92's `computeStoreHint`):
re-encode the store's `GetBlobStoreConfig()` through a digesting
writer using the store's `GetDefaultHashType()`. Hash families MUST
match for the comparison; if the receipt's hint uses a hash family the
local store does not support, the consumer falls through to branch 4
(missing-fallback) with a notice naming the hash mismatch.

### MCP and TAP output

`tree-restore` does NOT emit MCP-style structured output in v1. The
consumer's diagnostics are plain stderr lines. A future
`-format json` flag is deferred (see [Limitations](#limitations)).

## Examples

### Round-trip

    $ madder tree-capture -format json src
    ok 1 - capture src
    ok 2 - receipt blake2b256-7g…

    $ madder tree-restore blake2b256-7g… restored
    $ ls restored/src
    main.go
    go.mod

The receipt has a hint, the local store matches the hint's
config-markl-id, no diagnostic.

### Refusal: destination exists

    $ mkdir out
    $ madder tree-restore blake2b256-7g… out
    error: out: destination already exists
    hint: choose a destination that does not exist, or remove this one

Exit nonzero. `out/` is unchanged.

### Refusal: path-escape entry

A hand-crafted receipt with `{"root":"src","path":"../../../etc/passwd",...}`:

    $ madder tree-restore <receipt-id> out/
    error: entry escapes destination
      root: src
      path: ../../../etc/passwd
      materialized: /etc/passwd
      destination: /home/sasha/projects/foo/out

Exit nonzero. `out/` is NOT created.

### Store-hint mismatch

The receipt was written against `.work` whose config has since been
rotated:

    $ madder tree-restore <receipt-id> restored
    warning: store .work has been re-configured since this receipt was written
      receipt config-hash: blake2b256-9ft3m74l5t2ppwjrvfg3wp3…
      current config-hash: blake2b256-3wp380jqj2zfrm6zevxqx3…
    error: pass -store <id> to override and use the current store
    hint: re-running with -store .work uses the current configuration

    $ madder tree-restore -store .work <receipt-id> restored
    (proceeds against the current .work configuration)

### Symlink preservation

A receipt with a `type:"symlink"` entry whose `target` is `../bar`:

    $ madder tree-restore <receipt-id> restored
    $ readlink restored/src/link
    ../bar

The target is preserved verbatim, NOT resolved.

## Limitations

These are deliberate scope boundaries for v1. Each is filed as a
follow-up issue or deferred to a future schema version.

- **No mtime / atime / owner / group / xattrs / ACLs preservation.** The
  v1 receipt schema does not record these; preserving them is deferred
  to `madder-tree_capture-receipt-v2` ([RFC 0003] §Producer Rules
  §Body Schema).
- **No overwrite, merge, or partial-restore policy.** `<dest>` MUST NOT
  exist; the consumer creates it. There is no `-force`, no `-merge`,
  no `-resume`. Re-running after a partial failure means deleting the
  partial output and re-invoking.
- **No cross-store fallback for individual entries.** If the resolved
  store is missing a referenced `blob_id`, the restore aborts with a
  diagnostic naming the entry. There is no per-entry probe into other
  configured stores.
- **No `-format json` MCP output.** Deferred to a follow-up. The v1
  output is human-readable diagnostics on stderr only.
- **No symlink-target sanitization.** `e.target` is written literally
  via `os.Symlink`. A receipt with a symlink pointing to `/etc/shadow`
  is restored faithfully. [RFC 0003] §Security Considerations notes
  this is the standard tar-style risk; the consumer relies on the
  caller not following symlinks in the restored tree until reviewed.
- **No `-dry-run` flag.** Validation runs in the same pass as
  materialization (refusing before any write); a separate dry-run mode
  is deferred.
- **No mtime recording for the destination.** `<dest>` and any created
  directories receive the OS-default mtime at creation time, not the
  capture-time mtime.
- **Conformance test for unusual-but-legal filenames is deferred.**
  [RFC 0003] §Path Sanitization permits newlines and other valid-UTF-8
  characters in `e.root` / `e.path`. The bats scenario for this is not
  in v1's matrix; it is filed as a follow-up. The Go-level sanitizer
  permits them by construction (it only refuses NUL bytes, empty
  roots, and parent-escape).

## More Information

- [RFC 0003] — Tree-Capture / Tree-Restore Operational Rules
  (`docs/rfcs/0003-tree-capture-restore-rules.md`). Normative source
  for every behavioral rule in this FDR.
- [#87] — the issue this FDR resolves.
- [#91] — producer-side root scoping + collision detection. Closes the
  capture-time half of [RFC 0003] §Producer Rules.
- [#92] — store-hint metadata emission. Produces the metadata this
  FDR's §Store-Hint Resolution consumes.
- ADR 0005 — the remote-driven SFTP design that motivated the
  config-hash lock in the store hint.

## Conformance test mapping

For traceability, every normative rule in [RFC 0003] §Consumer Rules
maps to a bats test in `zz-tests_bats/tree_restore.bats`. The matrix
is the v1 acceptance criterion:

| [RFC 0003] § | Test |
|---|---|
| §Destination Preconditions | `tree_restore_refuses_existing_destination` |
| §Path Sanitization (parent-escape) | `tree_restore_refuses_path_escape_no_partial_writes` |
| §Path Sanitization (NUL byte) | `tree_restore_refuses_nul_byte_in_path` |
| §Path Sanitization (empty root) | `tree_restore_refuses_empty_root` |
| §Per-Type Materialization (file) | `tree_restore_round_trips_file` |
| §Per-Type Materialization (dir) | `tree_restore_round_trips_dir` |
| §Per-Type Materialization (symlink) | `tree_restore_round_trips_symlink` |
| §Per-Type Materialization (other → skip) | `tree_restore_skips_type_other_with_notice` (DEFERRED — hard to inject without root) |
| §Store-Hint Resolution §Auto-use | `tree_restore_uses_hint_store_when_config_matches` |
| §Store-Hint Resolution §Mismatch-warn | `tree_restore_warns_on_config_drift` |
| §Store-Hint Resolution §Missing-fallback | `tree_restore_falls_back_to_active_store_on_missing_hint` |
| §Store-Hint Resolution (no hint) | `tree_restore_falls_back_to_active_store_on_missing_hint` |
| §Store-Hint Resolution (-store override) | `tree_restore_store_flag_overrides_hint` |

Tests that share a row (no-hint and missing-fallback) are folded
together because the diagnostic shape is identical; the receipt
fixture differs.

[RFC 0003]: ../rfcs/0003-tree-capture-restore-rules.md
[#87]: https://github.com/amarbel-llc/madder/issues/87
[#91]: https://github.com/amarbel-llc/madder/issues/91
[#92]: https://github.com/amarbel-llc/madder/issues/92
