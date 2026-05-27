---
status: experimental
date: 2026-05-17
promotion-criteria: |
  Promote to `experimental` once Phase 1 (config self-check) lands and
  the bats suite covering the new `madder config-pin_digest` command
  and the `madder list` migration footer passes. Promote to `accepted`
  after Phase 2 (digest suffix on blob-store-ids) lands behind the
  same test bar and at least one persisted-reference call site (e.g.
  cutting-garden capture receipts) opts into emitting the
  digest-bearing form.
---

# Config Digest Pins

## Problem Statement

A blob-store-id today is a name + location prefix
(`go/internal/alfa/blob_store_id/main.go`). Two stores named `default`
on different machines, or two `default` stores that drift after a
manual `blob_store-config` edit, are indistinguishable by their ID.
References to blob stores in receipts and configs are therefore
untrustworthy across hosts and tamper-blind against local edits.

The `blob_store-config` file itself has no integrity check. A user
editing the config — to flip `verify-on-collision`, to change
compression, or maliciously — leaves no in-band evidence. The file is
trusted by every read path on the basis of "it parsed".

This FDR closes both gaps in two phases:

1. **Phase 1 (self-check).** Every `blob_store-config` carries an
   `@ <markl-id>` line in its hyphence metadata. The digest covers the
   config's body bytes. Read paths recompute and assert; mismatch is
   a hard failure with both digests in the error.
2. **Phase 2 (digest suffix on IDs).** `blob_store_id.Id` accepts an
   optional digest suffix:
   `default@blake2b256-9ft3m74l5t2ppwjrvfg3wp380jqj2zfrm6zevxqx34sdethvey0s5vm9gd`.
   At resolve time, the suffix is `AssertEqual`'d against the
   config's Phase 1 digest. Two stores sharing a name across hosts
   are now distinguishable by their digest; persisted references can
   pin a store cryptographically without giving up the friendly hint.

The mechanism for Phase 1 already exists: hyphence's typed-metadata
coder writes and reads `@ <markl-id>` lines today
(`go/internal/charlie/hyphence/coder_metadata.go:26`). The
expected-vs-actual error type already exists too: `markl.ErrNotEqual`
at `go/internal/bravo/markl/errors.go:167`. Phase 1 wires the
existing pieces together for `blob_store-config`; Phase 2 extends the
ID's text form to carry the digest.

This FDR originates from issue #194.

## Interface

### Phase 1: Config Self-Check

**Wire format.** A migrated `blob_store-config` carries an `@` line
in its hyphence metadata section:

    !toml-blob_store_config-v0
    @ madder-blob_store-config-digest-v1@blake2b256-9ft3m74l5t2ppwjrvfg3wp380jqj2zfrm6zevxqx34sdethvey0s5vm9gd

The digest covers the **body bytes** that follow the closing hyphence
boundary — i.e. the config payload that `coder.Blob.EncodeTo`
serializes. The metadata section (the `!` line, the `@` line itself,
both `Boundary` markers, and the blank separator) is **not** in the
hash input. This matches the natural seam in
`hyphence.CoderToTypedBlob` (`coder_to_typed_blob.go:133-181`) and
keeps the self-reference well-defined.

**Hash family.** `blake2b256`, hard-coded. madder's preferred hash;
the only alternative (`sha256`) does not justify a configuration knob
for self-check in Phase 1.

**Markl purpose.** A new purpose registered in
`go/internal/bravo/markl/purposes.go`:

    madder-blob_store-config-digest-v1

This is a madder-native artifact, not a dodder wire-format string. It
does **not** belong in the locked-strings bucket tracked by #16.

**Write path.** Every code path that emits a fresh
`blob_store-config` populates the `@` line unconditionally:

- `madder init`
- `madder init-sftp-explicit`, `madder init-sftp-ssh_config`
- `madder init-webdav`
- `madder init-s3`
- `madder init-pointer`, `madder init-pointer-v0`
- `madder init-from`
- `madder init-inventory-archive`

The mechanism: the config encoder renders the body to an in-memory
buffer, computes the digest, sets `TypedBlob.BlobDigest`, then writes
metadata + body via the existing hyphence coder. The metadata writer
already emits a non-null `BlobDigest` as `@ <markl-id>`
(`coder_metadata.go:56-67`).

**Read path.** Three cases:

1. **`@` line present.** Recompute body digest, call
   `markl.AssertEqual(stored, computed)`. Mismatch returns
   `markl.ErrNotEqual` carrying `Expected` and `Actual`. The CLI
   rendering shows both digests on separate lines so the user can
   see exactly what drifted.
2. **`@` line absent (legacy config).** Trusted silently. Behavior
   identical to today. This is the back-compat seam: an upgraded
   madder does not break on first read of any existing on-disk
   config.
3. **`@` line present but malformed.** Hard parse error. A
   syntactically broken markl-id is a corruption signal, not a
   "legacy" condition.

**Body-byte capture during decode.** The body of a hyphence-wrapped
config is read into the typed blob, but the raw bytes are needed for
re-hashing. A thin wrapping reader tees the post-second-boundary
bytes into a `blake2b256` hasher; the hash is finalized when the
typed-blob decoder returns. This is added inside the
blob_store-config decode call site, not in the generic hyphence
coder.

### Migration Command: `madder config-pin_digest`

Mints the `@` line on a config that lacks one. Idempotent: a config
that already has an `@` line is decoded (which triggers the Phase 1
self-check) and re-emitted; on a matching digest the rewrite is a
byte-identical no-op.

**Synopsis.**

    madder config-pin_digest <blob-store-id> [<blob-store-id> ...]
    madder config-pin_digest --all

`<blob-store-id>` accepts the same forms as every other CLI surface
that takes one. `--all` walks every configured store under the
active XDG roots; the two modes are mutually exclusive.

**Exit codes.**

- `0` — every targeted config now carries a valid `@` line.
- nonzero — at least one config failed the Phase 1 self-check on
  decode. A mismatched config is exactly what migration must
  surface; silent re-mint of a tampered config would defeat the
  purpose of the digest.

### `madder list`: Migration Surface

`madder list` is the primary discovery path for both the migration
state and the digest-bearing IDs themselves.

**Text output.** Each row's ID column emits the canonical text form
including `@<digest>` for migrated stores. Unmigrated stores render
as the bare name with a `(unmigrated)` marker in the same column.

When at least one unmigrated store is in the listing, a footer at
the end of the output prints a copy-pasteable command listing
exactly those store IDs:

    NOTE: 3 store(s) above are missing tamper-detection digests.
          Run this to migrate them:

            madder config-pin_digest default .archive %scratch

The footer is omitted when every listed store is already migrated.
The footer wording matches the copy-pasteable-command pattern used
by the legacy-rename error (#175). The migration-discovery UX is
in the same spirit as the broader UX umbrella tracked by #174.

**ndjson / json output.** `madder list` already supports
`-format=ndjson` and `-format=json`, with ndjson auto-selected when
stdout is piped (`go/internal/india/commands/list.go:41-49`). Both
gain two fields:

- `digest`: the markl-id string for migrated stores (omitted for
  unmigrated).
- `digest_missing`: `true` for unmigrated stores (omitted for
  migrated).

No new format flag is introduced.

### Phase 2: Digest Suffix on Blob-Store-IDs

**Text form.**

    blob-store-id = [prefix] name [ "@" markl-id ]
    prefix        = "." | ".." | "..." | ... | "/" | "%" | "_" | "~"
    name          = [a-zA-Z0-9_-]+
    markl-id      = format "-" blech32-data

Examples:

- `default` — bare (unchanged).
- `default@blake2b256-9ft3m74l...` — XDG user, with digest.
- `.archive@blake2b256-...` — CWD-relative, with digest.
- `%scratch@blake2b256-...` — XDG cache, with digest.

**Parser (`Id.Set`).** Split on the **first** `@`. The left side
flows through the existing prefix/name parser unchanged. The right
side parses via `markl.Id.Set`; an invalid markl-id is a hard parse
error, not a silent drop. The name charset is unchanged
(`[a-zA-Z0-9_-]`) so `@` is unambiguously the digest separator.

**Canonical form (`Id.Canonical`).** Emits `[prefix]name[@digest]`.
The cwdDepth flattening rule from #145 (single-dot for Cwd) is
unchanged. The digest, when present, round-trips byte-identical.

**Struct.**

    type Id struct {
        location xdg_location_type.Typee
        id       string
        cwdDepth uint
        digest   markl.Id  // zero-value = no digest
    }

New accessors: `GetDigest() markl.Id`, `HasDigest() bool`,
`WithDigest(markl.Id) Id`.

**`Less` ordering.** Compares by location, then `id`, then
`cwdDepth`, then digest (via `markl.Compare`). Two IDs differing
only in their digest sort deterministically — codified by a hard
invariant test.

**Resolution.** When a CLI arg parses to an `Id` with
`HasDigest() == true`:

1. Locate the on-disk config exactly as today (prefix + name).
2. Decode it through the Phase 1 path. The config is either
   migrated (self-check runs) or legacy (trusted silently).
3. If the config has its own digest, `markl.AssertEqual` between
   the ID's digest and the config's digest. Mismatch returns
   `markl.ErrNotEqual` with both digests.
4. If the config is legacy (no `@` line) but the ID supplies a
   digest, return a dedicated typed error:

       blob-store-id supplied a digest but the store's config is
       unmigrated. Run `madder config-pin_digest <id>` to mint a
       digest, then retry.

   Silently trusting an ID's digest against an un-digestable config
   defeats the point of the suffix.

**Emission.** Phase 2 does **not** rewrite any existing on-disk
reference. Receipts, configs, and inventory archive entries stay
byte-identical on disk. Phase 2 only governs how `Id.Canonical()`
renders when the in-memory value happens to carry a digest. Future
call sites that want to *start* persisting digest-bearing IDs (e.g.
cutting-garden capture receipts) opt in deliberately at the call
site; this FDR does not sweep that change.

**Code touchpoints.**

- `go/internal/alfa/blob_store_id/main.go` — `Id` struct gains
  `digest`; `Set` / `String` / `Canonical` / `Less` / `MarshalText`
  / `UnmarshalText` updated.
- `go/internal/foxtrot/blob_store_env/main.go` — resolver path
  that turns an `Id` into a live store; new `AssertEqual` at the
  seam.
- `go/internal/golf/command_components/blob_store.go` — CLI flag
  parsing path; inherits the new shape through `Id.Set`.
- New error type for "digest supplied against unmigrated config",
  alongside `blob_store_id`.

**Inline-store-switching.** Several commands (`write`, `pack-blobs`,
`cat`, `has`) accept positional arguments that may be either data
arguments (file paths, markl IDs) or blob-store-ids. An arg of the
form `[a-zA-Z0-9_-]+@<markl-id>` is unambiguously a blob-store-id —
distinct from a bare markl-id (no leading name + `@`) and from a
filename (`./` or explicit prefix disambiguates). No precedence
change is required. Filenames containing literal `@` characters
must be disambiguated by an explicit `./` prefix, the same
convention used today for store IDs that look like filenames.

## Examples

### Phase 1: self-check catches tampering

    $ madder list
    default     blake2b256-9ft3m74l...    /home/sf/.local/share/madder/...
    $ vim ~/.local/share/madder/blob_stores/default/blob_store-config
    # ...edit one byte in the body...
    $ madder list
    error: blob_store-config digest mismatch for "default"
      expected: blake2b256-9ft3m74l...
      actual:   blake2b256-2k4p9r3m...

### Migrating an existing store

    $ madder list
    default      (unmigrated)    /home/sf/.local/share/madder/...
    .archive     (unmigrated)    /home/sf/proj/.madder/...

    NOTE: 2 store(s) above are missing tamper-detection digests.
          Run this to migrate them:

            madder config-pin_digest default .archive

    $ madder config-pin_digest default .archive
    $ madder list
    default@blake2b256-9ft3m74l...   /home/sf/.local/share/madder/...
    .archive@blake2b256-7q3w5h2x...  /home/sf/proj/.madder/...

### Phase 2: digest-bearing ID resolves cleanly

    $ madder cat -store 'default@blake2b256-9ft3m74l...' blake2b256-abc...
    <blob content>

### Phase 2: digest mismatch refuses

    $ madder cat -store 'default@blake2b256-WRONG...' blake2b256-abc...
    error: blob-store-id digest does not match resolved store
      expected: blake2b256-WRONG...
      actual:   blake2b256-9ft3m74l...

### Phase 2: digest supplied against legacy config refuses

    $ madder cat -store 'default@blake2b256-9ft3m74l...' blake2b256-abc...
    error: blob-store-id supplied a digest but the store's config
    is unmigrated.
    hint: run `madder config-pin_digest default` to mint a digest,
          then retry

## Testing

**Phase 1 — round-trip & tamper.**

- Round-trip: encode a config, decode, assert the `@` line
  round-trips byte-identical.
- Tamper: encode a config, mutate one body byte on disk, decode →
  expect `markl.ErrNotEqual` with both digests in the message.
- Legacy passthrough: hand-write a config without an `@` line,
  decode → expect success, no error.
- Malformed `@`: hand-write an `@` line with a bogus markl-id
  payload → expect hard parse error.

**Phase 1 — init coverage.** One bats integration test per `init-*`
command asserting the emitted `blob_store-config` contains an `@`
line.

**`madder config-pin_digest`.**

- Round-trip on a legacy config; re-run → second invocation is a
  byte-identical no-op.
- `--all` walks every configured store under the active XDG roots.
- A tampered config under `--all` causes a nonzero exit and is
  reported by ID.

**Phase 2 — parser.** Table-test in `blob_store_id`: bare `name`,
`name@blake2b256-...`, and every prefix (`.`, `..`, `/`, `%`, `_`,
`~`) crossed with `{with, without}` digest. Round-trip each through
`MarshalText` / `UnmarshalText`.

**Phase 2 — `Less` invariant (HARD REQUIREMENT).** Two IDs differing
only in their digest sort deterministically. Map/set callers depend
on this.

**Phase 2 — resolution.**

- Digest mismatch: config with digest X, ID with digest Y → expect
  `markl.ErrNotEqual`.
- Digest against legacy config: legacy config + ID with digest →
  expect the dedicated error pointing at `config-pin_digest`.

**`madder list`.**

- Mixed-state fixture (some migrated, some not): text output shows
  the digest-bearing form for migrated stores and `(unmigrated)`
  for legacy; footer lists exactly the unmigrated IDs.
- ndjson/json modes: migrated rows carry `digest`; legacy rows
  carry `digest_missing: true`.
- All-migrated fixture: no footer.

## Risks

- **Existing on-disk references break.** Mitigation: Phase 2 doesn't
  rewrite any persisted ID. The wire format is strictly additive
  (digestless forms keep working). Legacy configs (no `@` line) are
  trusted silently on read.
- **Filenames containing `@` misparse as digest-bearing IDs.**
  Mitigation: same disambiguation convention as today's
  name-vs-filename collisions — `./foo@bar` is a file, `foo@bar` is
  a store ID. To be called out in `blob-store(7)`.
- **Migration footer fatigue.** A user who runs `madder list`
  repeatedly without migrating sees the footer every time. This is
  acceptable noise — suppressing it would hide the upgrade signal.
  The footer disappears once stores are migrated.
- **`madder list` text output gets wide.** A `blake2b256` digest is
  ~70 chars including the `blake2b256-` prefix. An 80-col terminal
  is tight. A `--short` / `--no-digest` escape hatch is captured in
  Future Work; not blocking for the FDR.
- **Tamper detection misread as covering blob contents.** Mitigation:
  the FDR and `blob-store(7)` explicitly state the digest covers the
  config only.
- **Legitimate config edits trigger digest mismatch.** This is by
  design. The user re-mints via `madder config-pin_digest`,
  acknowledging the change explicitly.

## Rollback

- **Phase 2 first, Phase 1 second.** Revert order matters: roll
  back Phase 2's resolve-time `AssertEqual` (the new failure mode
  on digest-bearing IDs) before touching Phase 1. Phase 2 is opt-in
  — only users who type or persist a digest-bearing ID see the new
  failure — so the blast radius is bounded by adoption.
- **Phase 1's read-side `AssertEqual` is the unit.** Revert that
  one call site to remove the digest-mismatch failure mode entirely.
  The `@` lines remain on disk and are harmless.
- **The `digest` field on `Id` is harmless to leave compiled in.**
  Zero-value means "no digest"; rolling back the parser to ignore
  `@` is sufficient if a wire-format-side rollback is ever needed.
- **No feature flag.** Both phases ship without an env-var or
  config gate. Phase 1's per-call-site revert is small enough that
  gating it would add complexity without measurable safety upside.
  Phase 2 is naturally opt-in by user action.
- **Pre-Phase-1 binary compatibility is out of scope.** A user who
  downgrades madder after migrating configs is responsible for
  re-testing; this FDR makes no claim about reverse-version reads.

## Future Work

Each item gets its own explore issue.

- **Absolute-path location type for blob-store-ids.** Resolves the
  `.foo` vs `..foo` relative-form ambiguity by allowing a store to
  be addressed by absolute path in the ID itself. Likely warrants
  its own RFC because it changes the wire format, the parser, and
  the interaction with `cwdDepth` (see #145, #153, #156). The Phase
  2 ID parser leaves room: the `name` slot stays opaque to the
  digest mechanism, so a future location type fits without
  disturbing digest semantics.
- **Pinning persisted references.** Call sites that emit
  blob-store-ids into receipts, configs, and inventory archives can
  opt in to emitting the digest-bearing form. Per-call-site design.
  cutting-garden capture receipts are the natural first candidate.
- **`madder list --short` / `--no-digest` flag.** Narrow-terminal
  escape hatch. The default stays full-digest (per this FDR).
- **`madder fsck` integration.** `fsck` could surface unmigrated
  stores the same way `list` does, with the same copy-pasteable
  command. Ties into #174.
- **sha256-only stores.** Phase 1 hard-codes `blake2b256` as the
  self-check hash. Revisit if a sha256-only store needs self-check.

## More Information

- **Origin:** #194 ("Explore: extend blob-store IDs with content
  blake-digest as markl-id (hint + hard pin)").
- **Locked wire-format strings (#16):** the new
  `madder-blob_store-config-digest-v1` purpose is madder-native and
  is **not** in the dodder-wire-format bucket. No coordination
  required.
- **UX umbrella (#174):** the `madder list` migration footer is in
  the same spirit as the `init` / `sync` / `fsck` / `pack` UX
  improvements tracked there.
- **Copy-pasteable-command pattern (#175):** the legacy-rename
  error establishes the footer pattern this FDR adopts.
- **Relative-path ambiguity (#145, #153, #156):** related to the
  deferred absolute-path-location future work, but explicitly out
  of scope here.
- **Hyphence metadata mechanism:**
  `go/internal/charlie/hyphence/coder_metadata.go:21-71` —
  Phase 1 reuses the existing `@ <markl-id>` line-writer and
  `BlobDigest` typed-blob field.
- **Expected-vs-actual error type:**
  `go/internal/bravo/markl/errors.go:167-203` —
  `markl.ErrNotEqual` + `markl.AssertEqual`. Used by both phases.
