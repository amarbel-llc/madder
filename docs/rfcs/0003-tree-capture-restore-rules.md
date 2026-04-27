---
status: accepted
date: 2026-04-27
---

# Tree-Capture / Tree-Restore Operational Rules

## Abstract

This document specifies behavioral rules for producers (`madder tree-capture`) and consumers (`madder tree-restore`) of `madder-tree_capture-receipt-v1` blobs. The rules cover root scoping, root collision detection, store-hint metadata, restore-side path sanitization, and the per-type materialization contract. The receipt's record schema (paths, types, modes, blob references) is documented separately in `tree-capture-receipt(7)`; this document layers normative obligations on top of that schema so that a receipt written by any conformant producer is safe to restore by any conformant consumer.

## Introduction

`madder tree-capture` walks one or more directories and writes their contents as content-addressable blobs into a configured store, emitting a receipt blob per store-group that lists every captured entry (`tree-capture-receipt(7)`). The inverse — `madder tree-restore` — is being introduced under [#87](https://github.com/amarbel-llc/madder/issues/87) and consumes a receipt to materialize the captured tree on disk.

For the producer/consumer pair to be safe and predictable, several rules need to be normative rather than implicit:

1. **Capture roots** can today be any directory the user names, including those outside the current working directory or upward through `..`. That choice was deliberate in v1 of `tree-capture`, but it complicates the restore-side path-sanitization story and surfaces classic Zip-Slip-style risks. This RFC narrows capture-roots to PWD or descendants thereof, mirroring git's work-tree scoping (with PWD as the implicit work tree).
2. **Receipts** today carry no record of which store wrote them. The consumer must guess. This RFC defines an additive hyphence-metadata convention that lets a receipt name its origin store and lock the lookup to the store's config-blob digest, so consumers can self-resolve in the common case and detect drift in the uncommon one.
3. **Restore-side path handling** has to be defense-in-depth against corrupt or hostile receipts. This RFC specifies the sanitization rule and the refusal conditions.
4. **Per-entry materialization** has to be unambiguous across types (file, dir, symlink, other), since the receipt itself is content-addressed and consumers across machines must agree on what restore produces.

The wire format of receipts is not respecified here. The hyphence type remains `madder-tree_capture-receipt-v1` and the body NDJSON schema is unchanged from `tree-capture-receipt(7)`. This RFC is purely additive metadata + behavioral obligations.

## Requirements Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in RFC 2119.

## Specification

### Roles

- **Producer**: any tool that writes a `madder-tree_capture-receipt-v1` blob. The reference producer is `madder tree-capture`.
- **Consumer**: any tool that reads a `madder-tree_capture-receipt-v1` blob to materialize the captured tree on disk. The reference consumer is `madder tree-restore`.

A given binary MAY implement only the producer side, only the consumer side, or both.

### Producer Rules

#### Root Scoping

A producer MUST refuse any positional capture-root argument that does not resolve to the producer's current working directory (PWD) or a descendant of PWD.

For each positional argument `root` that classifies as a directory (per `tree-capture(1)`'s arg-classification rules), the producer MUST:

1. Compute `abs := filepath.Abs(root)` against PWD.
2. Compute `rel, err := filepath.Rel(pwd, abs)`.
3. Refuse the argument if `err != nil`, if `rel == ".."`, or if `rel` begins with `".." + os.PathSeparator`.

The refusal error message MUST identify the offending argument and PWD, and SHOULD include a remediation hint suggesting that the user `cd` into a parent directory that contains the desired path. A conformant error message has the form:

    error: <root>: outside working directory at <pwd>
    hint: cd to a parent directory containing <root>, then re-run

This rule mirrors git's work-tree scoping (`git add ../sibling` is refused with `'../sibling' is outside repository at '/path/to/repo'`), with PWD as the implicit work tree (madder has no repo-root concept analogous to git's `.git/`).

The rule applies to every positional root argument, including absolute paths (which are equivalent to "non-descendant" when they resolve outside PWD), shell-expanded tildes (which arrive at the producer already expanded to absolute paths), and parent-relative paths.

#### Root Collision Detection

A producer MUST refuse a capture invocation if two or more positional capture-roots within the same store-group resolve to the same path after `filepath.Clean`.

Examples that MUST be refused at planning time, before any walking begins:

- `madder tree-capture src ./src`
- `madder tree-capture vendor vendor/`
- `madder tree-capture ./internal ./internal/.`

The refusal error message MUST identify both colliding arguments and the canonicalized path they share.

The intent is that valid receipts never contain two `root` values that collapse to the same path under `filepath.Clean`. Consumers can rely on this invariant when resolving multi-root receipts (see Multi-Root Receipts below).

#### Symlink Roots

A producer MUST refuse a positional capture-root that is itself a symbolic link (whether or not the link target is a directory). This is the existing behavior of `tree-capture`'s planner; it is restated here for completeness.

The intent is that symlinks are recorded in the body as `type:"symlink"` entries with their literal target string, not silently dereferenced. A user who intends to capture the linked tree's contents is expected to resolve the symlink with `realpath(1)` before invoking the producer.

#### Receipt Metadata: Store Hint

When the producer knows the identifier and the configuration-blob digest of the store it is writing the receipt into, the producer SHOULD emit a hyphence metadata line of the form:

    - store/<store-id> < <config-markl-id>

Where:

- `<store-id>` is the producer's local identifier for the destination store. For `madder tree-capture` this is the `blob_store_id` (e.g. `default`, `.archive`, `.work`).
- `<config-markl-id>` is the markl-id of the destination store's `toml-blob_store_config-vN` blob, in the canonical text form produced by `markl-id(7)`.

The line MUST appear in the receipt's hyphence metadata block, between the opening `---` boundary and the type line `! madder-tree_capture-receipt-v1`.

A producer MAY omit the line entirely if it cannot determine the destination store's identifier or if the store has no addressable configuration blob (e.g. an in-memory test fixture). Consumers MUST tolerate the absence of the line (see Store-Hint Resolution below).

The line is additive metadata. Older readers that do not interpret it MUST treat it as opaque metadata per `hyphence(7)` and MUST NOT reject the receipt on its account.

#### Body Schema

The body's NDJSON schema is unchanged from `tree-capture-receipt(7)`. Producers MUST continue to emit entries sorted by `(root, path)` for byte-identical output across runs of equivalent inputs. The hyphence type MUST remain `madder-tree_capture-receipt-v1`.

A producer that needs to record fields not specified in the v1 schema (for example, mtime or owner/group) MUST allocate a new type (`madder-tree_capture-receipt-v2`, etc.) rather than extending v1.

### Consumer Rules

#### Destination Preconditions

A consumer MUST refuse to restore into a destination directory that already exists at invocation time. The consumer creates the destination as part of restore.

This rule keeps the MVP simple. Overwrite, merge, and partial-restore policies are deferred (see Out of Scope).

#### Path Sanitization

For each entry `e` in a receipt being restored into destination `dest`, the consumer MUST compute the materialized path as:

    materialized := filepath.Clean(filepath.Join(dest, e.root, e.path))

The consumer MUST refuse to restore the entire receipt — without leaving any partial materialization on disk — if any of the following hold for any entry:

1. `materialized` is not equal to `filepath.Clean(dest)` and is not lexically rooted under `filepath.Clean(dest) + os.PathSeparator`.
2. `e.root` or `e.path` contains a NUL byte (`\x00`).
3. `e.root` is the empty string.

These are defense-in-depth checks against corrupt or hand-crafted receipts. Receipts produced by a conformant producer (per Producer Rules above) cannot trigger them, because PWD-scoping at capture time precludes parent-escape, and well-formed filesystems do not produce empty-string root names.

Newlines (`\n`, `\r`) and other valid-UTF-8 characters in `e.root` and `e.path` MUST be PERMITTED. They are filesystem-legal on POSIX systems and survive NDJSON encoding/decoding intact. The consumer MUST NOT reject an entry on the basis of its name containing newlines or other unusual-but-legal characters.

#### Per-Type Materialization

For each entry `e` accepted by Path Sanitization, the consumer MUST materialize it according to `e.type`:

| `e.type` | Materialization |
|---|---|
| `file` | Resolve `e.blob_id` against the configured store. Open `materialized` for write (create-only). Stream the blob via `io.Copy` (`BlobReader` provides `io.WriterTo`; bytes flow chunk-wise). On success, `chmod` to `e.mode & 0o777`. |
| `dir` | `os.MkdirAll(materialized, e.mode & 0o777)`. |
| `symlink` | `os.Symlink(e.target, materialized)`. The mode field MUST be ignored (POSIX symlink modes are platform-fudged and not portable). |
| `other` | Skip. The consumer SHOULD emit a sink notice naming the path and skipping reason. The consumer MUST NOT attempt to recreate devices, FIFOs, sockets, or other special files. |

For `type=file`, the consumer MUST stream content through the BlobReader's `WriteTo` path. Consumers MUST NOT buffer file content in memory; any conformant `BlobReader` implementation is required to satisfy `io.WriterTo` and the consumer must use it.

The consumer MUST NOT preserve mtime, atime, owner, group, xattrs, or ACLs. These fields are not present in the v1 receipt schema; preservation is deferred to a future schema version.

### Store-Hint Resolution

A consumer determines which configured blob store to use for resolving `blob_id` references via the following procedure:

1. Parse the receipt's hyphence metadata block.
2. If a line of the form `- store/<id> < <markl-id>` is present:
   1. If a configured store on the consumer machine has a matching `<id>` AND its current `toml-blob_store_config-vN` blob has a markl-id equal to `<markl-id>`, the consumer MUST use that store and SHOULD NOT prompt the user.
   2. If a configured store has matching `<id>` but its config-blob markl-id differs from `<markl-id>`, the consumer MUST emit a warning identifying the discrepancy. The consumer MUST refuse to proceed unless the user has explicitly named a store via the `-store` command-line flag.
   3. If no configured store has matching `<id>`, the consumer SHOULD fall back to the active store (per the consumer's normal store-selection rules) and SHOULD emit a notice that the receipt names a store that is not configured locally. The user MAY override via `-store`.
3. If no `- store/<id> < <markl-id>` line is present, the consumer SHOULD use the active store and SHOULD emit a notice that the receipt carries no store hint. The user MAY override via `-store`.

The intent is that a single-machine restore from the same store the receipt was written into requires no flags and produces no prompts; cross-machine and post-reconfiguration scenarios surface as actionable diagnostics.

### Multi-Root Receipts

A receipt MAY contain entries from multiple positional roots when a single store-group capture has multiple top-level directory arguments. Each distinct value of `e.root` materializes as a top-level subdirectory under `dest`, after Path Sanitization.

Consumers MUST NOT re-canonicalize `e.root` beyond what `filepath.Clean(filepath.Join(dest, e.root, e.path))` provides. Producer-side Root Collision Detection guarantees that no two distinct values of `e.root` within a single receipt resolve to the same path under `filepath.Clean`, so consumers can rely on root-distinctness as a structural invariant.

## Examples

### Valid Receipt with Store Hint

A receipt for two files under `src/` captured into store `.work` (whose `toml-blob_store_config-v3` blob has markl-id `blake2b256-9ft3m74l5t2ppwjrvfg3wp3…`):

    ---
    - store/.work < blake2b256-9ft3m74l5t2ppwjrvfg3wp380jqj2zfrm6zevxqx34sdethvey0s5vm9gd
    ! madder-tree_capture-receipt-v1
    ---

    {"path":".","root":"src","type":"dir","mode":"0755"}
    {"path":"main.go","root":"src","type":"file","mode":"0644","size":482,"blob_id":"blake2b256-9ft3m74l5t2ppwjrvfg3wp3…"}
    {"path":"go.mod","root":"src","type":"file","mode":"0644","size":92,"blob_id":"blake2b256-pwjrvfg3wp380jqj2zfrm6z…"}

The `blob_id` values in the body are abbreviated for readability; real receipts carry the full `format-data` form specified in `markl-id(7)`. The store-hint lock is shown in full.

### Capture-Time Refusal: Non-Descendant Root

User runs `tree-capture` from `/home/sasha/projects/foo` and attempts to capture a sibling directory:

    $ pwd
    /home/sasha/projects/foo
    $ madder tree-capture ../bar
    error: ../bar: outside working directory at /home/sasha/projects/foo
    hint: cd to a parent directory containing ../bar, then re-run

Exit code is nonzero. No blobs are written. No receipt is produced.

### Capture-Time Refusal: Root Collision

    $ madder tree-capture src ./src
    error: roots "src" and "./src" both resolve to "src" after Clean
    hint: pass each directory only once per store-group

Exit code is nonzero. No blobs are written.

### Restore-Time Refusal: Path Escape

A hand-crafted receipt with a malicious `path` field:

    ---
    ! madder-tree_capture-receipt-v1
    ---

    {"path":"../../../etc/passwd","root":"src","type":"file","mode":"0644","size":1234,"blob_id":"blake2b256-qpzry9x8gf2tvdw0s3jn54…"}

When restored into `out/`:

    $ madder tree-restore <receipt-id> out/
    error: entry escapes destination
      root: src
      path: ../../../etc/passwd
      materialized: /etc/passwd
      destination: /home/sasha/projects/foo/out

Exit code is nonzero. The destination directory `out/` is not created. No bytes are materialized.

### Store-Hint Resolution Branches

Given a receipt with hint `- store/.work < blake2b256-9ft3m74l5t2ppwjrvfg3wp3…`:

**Auto-use** — `.work` is configured locally and its config-blob hash matches:

    $ madder tree-restore <receipt-id> out/
    # (no prompt, blobs resolve via .work)

**Mismatch-warn** — `.work` is configured locally but its config-blob hash differs:

    $ madder tree-restore <receipt-id> out/
    warning: store .work has been re-configured since this receipt was written
      receipt config-hash: blake2b256-9ft3m74l5t2ppwjrvfg3wp3…
      current config-hash: blake2b256-3wp380jqj2zfrm6zevxqx3…
    error: pass -store <id> to override and use the current store
    hint: re-running with -store .work uses the current configuration

**Missing-fallback** — no `.work` configured locally:

    $ madder tree-restore <receipt-id> out/
    notice: receipt names store ".work" which is not configured locally
    notice: falling back to active store
    # (proceeds with the active store; -store may override)

## Security Considerations

This specification's materialization rules write to attacker-controllable filenames. The Path Sanitization rules are the primary defense: a receipt with parent-escape or absolute-path entries (whether produced by a buggy non-conformant producer or a malicious adversary) is refused before any disk write occurs.

The Producer's Root Scoping rule reduces the attack surface by ensuring conformant producers never emit `root` values outside the user's working tree. Consumers MUST NOT trust producer conformance and MUST apply the Path Sanitization checks regardless.

The Store-Hint mechanism is advisory and MUST NOT be treated as authentication of receipt provenance. A receipt that names a particular store does not prove that the receipt was written by an authorized actor with access to that store. Consumers SHOULD treat the store-hint as a routing convenience, not an authorization signal.

The config-markl-id lock detects benign drift (the destination store has been reconfigured since the receipt was written) but does not protect against attackers who can forge receipts. Receipts are content-addressed blobs; their integrity derives from the store that holds them, not from the receipt's metadata.

Consumers MUST NOT follow symlinks they create during restore in any subsequent operation that traverses the restored tree until the user has had an opportunity to review it. A captured tree containing `symlink` entries pointing to `/etc/shadow` is restored faithfully (the link's target is a literal string), and a follow-up operation that opens files under the restored tree could be tricked into reading the symlink target. This is the standard symlink-replacement class of vulnerability and is no different from the risk of any tar-style extraction.

## Conformance Testing

Conformance tests for this specification live in `zz-tests_bats/`:

- `zz-tests_bats/tree_capture.bats` — producer rules. Existing file; new tests are added under #87's parent issue for Root Scoping and Root Collision.
- `zz-tests_bats/tree_restore.bats` — consumer rules. To be created when [#87](https://github.com/amarbel-llc/madder/issues/87) lands.

Tests use binary injection via `bats-emo`:

    require_bin MADDER_BIN madder

so an alternative implementation of `madder` (or of either subcommand) can be substituted by exporting `MADDER_BIN` to its path.

### Covered Requirements

| Requirement | Test File | Description |
|---|---|---|
| Producer Rules § Root Scoping (MUST refuse `../foo`) | `tree_capture.bats` | `tree_capture_refuses_parent_escape_root` |
| Producer Rules § Root Scoping (MUST refuse absolute root) | `tree_capture.bats` | `tree_capture_refuses_absolute_root` |
| Producer Rules § Root Collision Detection | `tree_capture.bats` | `tree_capture_refuses_collision_after_clean` |
| Producer Rules § Symlink Roots | `tree_capture.bats` | (existing) |
| Producer Rules § Receipt Metadata: Store Hint | `tree_capture.bats` | `tree_capture_emits_store_hint_when_known` |
| Consumer Rules § Destination Preconditions | `tree_restore.bats` | `tree_restore_refuses_existing_destination` |
| Consumer Rules § Path Sanitization (MUST refuse path-escape) | `tree_restore.bats` | `tree_restore_refuses_path_escape_no_partial_writes` |
| Consumer Rules § Path Sanitization (MUST refuse NUL byte) | `tree_restore.bats` | `tree_restore_refuses_nul_byte_in_path` |
| Consumer Rules § Per-Type Materialization (file/dir/symlink) | `tree_restore.bats` | `tree_restore_round_trips_<type>` |
| Consumer Rules § Per-Type Materialization (other → skip) | `tree_restore.bats` | `tree_restore_skips_type_other_with_notice` |
| Store-Hint Resolution § Auto-use | `tree_restore.bats` | `tree_restore_uses_hint_store_when_config_matches` |
| Store-Hint Resolution § Mismatch-warn | `tree_restore.bats` | `tree_restore_warns_on_config_drift` |
| Store-Hint Resolution § Missing-fallback | `tree_restore.bats` | `tree_restore_falls_back_to_active_store_on_missing_hint` |

## Compatibility

This specification is the first formal spec of the `tree-capture` / `tree-restore` operational contract. The producer side (`tree-capture`) is partially deployed; this RFC adds two new producer-side requirements (Root Scoping, Root Collision Detection) and one new SHOULD (Store Hint metadata). Existing callers of `tree-capture` that pass non-PWD-descendant roots will see a refusal after these rules ship; this is a breaking change for any caller that relied on the prior permissive behavior. The breakage is intentional and motivated by Path Sanitization simplification on the consumer side.

The body schema (`madder-tree_capture-receipt-v1`) is preserved unchanged. Receipts produced before this RFC ships are accepted by conformant consumers. Receipts produced after this RFC ships are accepted by older readers (the new metadata line falls under hyphence's "lines may appear in any order" tolerance per `hyphence(7)`).

Implementation lands in two phases:

1. **Producer-side enforcement**: PWD-scoping check and collision detection in the `tree-capture` planner. Store-hint metadata emission in the receipt encoder. Filed as one or more issues stacked under #87.
2. **Consumer**: `tree-restore` is implemented per #87. The producer-side phase MUST land first, so receipts produced once the consumer ships already conform to this RFC.

## References

### Normative

- `docs/man.7/tree-capture-receipt.md` — receipt body schema (record fields, sort order, hyphence type).
- `docs/man.7/hyphence.md` — metadata block syntax.
- `docs/man.7/markl-id.md` — canonical text encoding for content-addressable identifiers.
- RFC 2119 — "Key words for use in RFCs to Indicate Requirement Levels", Bradner, S., March 1997.

### Informative

- [#87](https://github.com/amarbel-llc/madder/issues/87) — `tree-restore` command implementation.
- [#88](https://github.com/amarbel-llc/madder/issues/88) — Pre-walk huh confirm-gate for large captures (independent of this RFC).
- [#89](https://github.com/amarbel-llc/madder/issues/89) — `hyphence(7)` tag-syntax clarification (informative; this RFC's `- store/<id>` convention does not depend on the man-page wording fix).
- `docs/decisions/0005-remote-driven-sftp-blob-stores.md` — adjacent ADR establishing that the remote `blob_store-config` is the source of truth for hash/buckets/compression/encryption, which informs this RFC's choice to lock the store-hint to the config-blob's markl-id rather than to the store's transport-layer settings.
