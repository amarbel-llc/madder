---
status: proposed
date: 2026-05-26
promotion-criteria: |
  Promote to `experimental` once an `init-multi` command exists, a
  `multi` store can be authored, round-tripped, and read through the
  same CLI surfaces as any other store (no per-command flag), and the
  four canonical bats scenarios pass (mirror, write_through with
  read_fill, write_through without read_fill, nested multi-of-multi).
  Promote to `accepted` once the per-command ad-hoc fallback paths
  (`blobFromRemainingStores` in `cat`/`has`/`list`/`fsck`) are
  removed in favor of multi-driven composition, and at least one
  downstream consumer (dodder or cutting-garden) opts into a multi
  config for its default store.
---

# Multi-store as a bonafide config type

## Problem Statement

The `Multi` blob store is implemented as an in-process composition
primitive (`go/internal/foxtrot/blob_stores/multi.go`, built across
tasks 1–13 of #182). It supports two modes (mirror, write-through
with optional tee-on-read cache fill) and operates on any list of
`BlobStoreInitialized` values.

Today, reaching `Multi` from the CLI requires either threading a
`-multi` flag through every command that needs fallback semantics
(`cat`, `has`, `list`, `fsck`), or duplicating the ad-hoc fallback
walk that already lives in each of those commands
(`blobFromRemainingStores` in `cat.go` and its siblings). Both shapes
are invasive: the flag approach pollutes the command surface and
prevents the cache-fill semantics from being a default; the
duplication approach guarantees the fallback diverges between
commands as the codebase evolves.

The Multi primitive is data — a list of stores plus a small mode
selector. It should be expressible as a `blob_store-config`, alongside
`local_hash_bucketed`, `remote_sftp`, and the other persistent store
types. When the user configures a multi store as their default, every
command transparently composes through it without per-command
flag-threading.

The blocking constraint has historically been cycle detection: a
config that references other configs creates a DAG; naive references
admit `A → B → A`. FDR-0008 Phase 2 dissolves this constraint.
Digest-bearing blob-store-IDs (`default@blake2b256-…`) carry the
referenced config's content digest. Because a config's own digest is
computed over its body bytes (which include its references), a
multi's digest cannot exist until the digests of every referenced
store are known. A self-reference or back-reference would require
computing a digest that depends on knowing itself first. The wire
format makes cycles unrepresentable — the same Merkle-DAG property
that underpins Git's commit graph, Nix's derivation graph, and
IPFS's content addressing.

This FDR adds `store_type = "multi"` to the config schema, requires
digest-bearing references (anchored on FDR-0008 Phase 2), and
specifies how multi stores compose with the existing default-store
selection and per-command read paths.

## Interface

### Config schema

A multi `blob_store-config` carries the standard hyphence header (with
the Phase-1 digest line from FDR-0008) followed by a TOML body:

    !toml-blob_store_config-v0
    @ madder-blob_store-config-digest-v1@blake2b256-…

    store_type = "multi"
    mode = "write_through"
    write_store = "local-default@blake2b256-2k4p9r3m…"
    read_stores = [
        "remote-cache@blake2b256-7q3w5h2x…",
        "remote-archive@blake2b256-9ft3m74l…",
    ]
    read_fill = true

Field semantics:

- `store_type = "multi"` — reserved string, registered alongside the
  other store types. Per the codec stability convention, the string
  is locked: no shadowing, no override.
- `mode` — `"mirror"` or `"write_through"`. Exactly one is required.
- `write_store` — required when `mode = "write_through"`. A single
  digest-bearing blob-store-id. The Multi's single write target.
  Forbidden when `mode = "mirror"`.
- `read_stores` — required when `mode = "write_through"`. An ordered
  list of digest-bearing blob-store-ids. Forbidden when
  `mode = "mirror"`.
- `mirror_stores` — required when `mode = "mirror"`. An ordered list
  of digest-bearing blob-store-ids. Forbidden when
  `mode = "write_through"`.
- `read_fill` — optional, defaults to `true`. Only meaningful when
  `mode = "write_through"`. When `true`, successful reads from a
  `read_stores` entry tee into `write_store` (using the tee built in
  #182 tasks 10–12). When `false`, no cache fill happens.

### References must be digest-bearing

Every blob-store-id appearing in a multi config carries the
digest suffix introduced in FDR-0008 Phase 2. A bare reference
(`default`, no `@…`) is a hard parse error inside a multi config
body, distinct from the legitimate bare form accepted by the CLI
elsewhere.

This requirement is the load-bearing wire-format rule that makes the
multi-store graph a Merkle DAG. The CLI's separate "digest supplied
against unmigrated config" error (Phase 2 of FDR-0008) does not apply
here: every reference inside a multi config is, by construction,
referring to a config that already had a digest when the multi was
authored.

### Construction and resolution

When `MakeBlobStores` walks the store map (`blob_store_env`), it
builds in two passes:

1. **Leaf pass.** Materialize every non-`multi` config. These have no
   dependencies and resolve in arbitrary order, exactly as today.
2. **Multi pass.** Materialize every `multi` config. The pass runs
   as a loop: in each iteration, build any multi whose every
   reference has already been resolved. The loop terminates when no
   progress is made.

If a multi's references are all resolved, the factory:

1. Looks up each referenced `BlobStoreInitialized` in the now-populated
   store map by the reference's name component.
2. Calls `markl.AssertEqual` between the reference's digest and the
   resolved config's Phase-1 digest. Mismatch returns `markl.ErrNotEqual`.
3. Constructs the `Multi` via the existing `NewMulti(ctx).…Build()`
   builder.

If the loop terminates with un-built multis remaining, every such
multi has either (a) a reference to a nonexistent store
(*dangling ref*) or (b) — impossible by Merkle-DAG construction —
a cycle. The factory reports the dangling refs by name and digest
and returns an error.

### Default-store selection

`EnvBlobStore.GetDefaultBlobStore` already selects one store as the
default by the existing XDG rules. Those rules are unchanged. When
the default config happens to be `store_type = "multi"`, the function
returns that multi as the default — it implements `BlobStore` like
any other store type.

`GetDefaultBlobStoreAndRemaining` continues to exist for backward
compatibility but its `remaining` slice is empty when the default is
a multi (the multi already encapsulates the fallback set).
`blobFromRemainingStores` in `cat.go` and siblings becomes
unreachable when the default is a multi.

In the post-rollout state (`accepted`), `blobFromRemainingStores` is
removed entirely. Users who want fallback semantics author a multi;
users who don't, don't.

### Nesting

`multi` configs may reference other `multi` configs. Depth is
unbounded; cycles are unrepresentable. The construction loop above
handles arbitrary depth without modification — each multi simply
waits for its referenced multis to build first.

The construction loop is `O(N)` over the store-map size for any
finite DAG of depth `D`: at worst the loop iterates `D` times, each
iteration touching every unbuilt multi. In practice, two- or
three-level hierarchies (fast tier → slow tier, or
fast-and-medium-tier mirror → cold tier write-through) cover the
imagined use cases.

### Authoring: `madder init-multi`

A new init command mirrors the shape of the existing `init-*` family.

    madder init-multi <id> --mode {mirror,write_through} \
        [--write-store <id>] \
        [--mirror-store <id> ...] \
        [--read-store <id> ...] \
        [--read-fill | --no-read-fill]

Each `<id>` argument may be:

- A digest-bearing form (`default@blake2b256-…`) — used verbatim.
- A bare name (`default`) — `init-multi` resolves it to the current
  digest by reading the referenced config's Phase-1 digest, and
  emits the digest-bearing form into the new multi config.

The bare-name shortcut is a UX convenience; the on-disk multi config
always carries digest-bearing references. A subsequent leaf rotation
(`madder config-pin_digest <leaf>` after a hand-edit) does NOT update
the multi's reference — that's a deliberate manual re-mint (see
*Risks*).

### `madder list` rendering

A multi config is listed like any other store, with two
multi-specific additions:

- The mode (`mirror` / `write_through (read_fill)` /
  `write_through (no read_fill)`) appears next to the store type.
- A `-tree` flag — added by this FDR — walks the multi graph and
  renders the reference structure inline. Without `-tree`, only the
  top-level multi is shown.

ndjson / json output gains:

- `refs`: an array of `{name, digest, role}` objects where `role` is
  `"write"`, `"read"`, or `"mirror"`.
- `mode`: `"mirror"` or `"write_through"`.
- `read_fill`: `true` / `false` (omitted for mirror mode).

## Examples

### Two-tier write-through with cache fill

The most common shape: a fast local store backed by a slow remote
archive. Reads come from local when present, fall through to remote
otherwise, and tee back into local so the next read is fast.

    # 0. Two leaf stores already exist:
    #    default     blake2b256-2k4p9r3m…  /var/cache/madder/default
    #    .archive    blake2b256-9ft3m74l…  sftp://archive.example/blobs

    $ madder init-multi cache --mode write_through \
        --write-store default \
        --read-store .archive \
        --read-fill

    $ cat ~/.local/share/madder/blob_stores/cache/blob_store-config
    !toml-blob_store_config-v0
    @ madder-blob_store-config-digest-v1@blake2b256-…

    store_type = "multi"
    mode = "write_through"
    write_store = "default@blake2b256-2k4p9r3m…"
    read_stores = [".archive@blake2b256-9ft3m74l…"]
    read_fill = true

    $ madder cat blake2b256-abc…
    <blob bytes; if missing from `default`, fetched from .archive and
     teed into default on the way through>

### Mirror across two local stores

A redundancy pattern: writes go to both stores, reads fall back across
them.

    $ madder init-multi mirror-fs --mode mirror \
        --mirror-store .ssd \
        --mirror-store .nvme

    $ cat ~/.local/share/madder/blob_stores/mirror-fs/blob_store-config
    !toml-…

    store_type = "multi"
    mode = "mirror"
    mirror_stores = [
        ".ssd@blake2b256-…",
        ".nvme@blake2b256-…",
    ]

### Nested: multi-of-multi

Tiered storage: SSD-and-NVMe mirror as the fast tier, fast tier
write-through cached against tape archive.

    $ madder init-multi fast --mode mirror \
        --mirror-store .ssd --mirror-store .nvme

    $ madder init-multi tiered --mode write_through \
        --write-store fast \
        --read-store .tape \
        --read-fill

    $ madder list -tree
    tiered      multi:write_through(read_fill)  blake2b256-…
        └── fast      multi:mirror                blake2b256-…  (write)
        │       ├── .ssd                          blake2b256-…
        │       └── .nvme                         blake2b256-…
        └── .tape     remote_sftp                 blake2b256-…  (read)

A read through `tiered` flows: fast.mirror → .ssd (or .nvme on
miss) → .tape (on full miss). On hit from .tape, the bytes tee into
`fast`, which mirrors them to both .ssd and .nvme. Subsequent reads
serve from the fast tier.

### Cycle attempt: structurally impossible

A user trying to fabricate a self-reference cannot get past the
authoring step:

    $ madder init-multi cycle --mode mirror \
        --mirror-store cycle@blake2b256-…  # ← what digest?
    error: blob-store-id `cycle` does not exist yet; cannot
    self-reference at init time. (And there is no digest you could
    supply: the digest you'd need is the digest of this config,
    which depends on the digest you're trying to supply. This is
    intentional — multi configs form a Merkle DAG.)

### Dangling reference

A multi config referencing a store that doesn't exist fails at
construction:

    $ madder list
    error: multi store `cache` references `nonexistent@blake2b256-…`
    which is not present in any configured XDG scope.

## Limitations

- **Editing a leaf rotates its digest, invalidating dependent multi
  configs.** `madder config-pin_digest <leaf>` re-mints the leaf's
  digest; any multi that referenced the old digest will fail
  `AssertEqual` at next load. The user must re-mint each downstream
  multi (a future `madder config-rebuild_multi` command, listed in
  *Future Work*, automates this).
- **Multi configs are persistent. There is no inline `--store=multi:…`
  flag.** A user who wants ad-hoc multi composition for a single
  command invocation must author a multi config first. The argument
  for this constraint is that ad-hoc multi composition is exactly
  what FDR-0009 is replacing — the per-command `-multi` flag we
  rejected.
- **The `mirror_stores` set has no schema for partial-write tolerance.**
  A failed write to one mirror member fails the whole write (existing
  Multi mirror-mode semantics). Future work could add a quorum
  setting.
- **A leaf store cannot atomically be "moved into" a multi.**
  Switching a default store from a leaf to a new multi requires
  authoring the multi config, then changing which config the default
  XDG path resolves to (today by `madder init` order; future work
  could add a `madder default <id>` command).
- **`list -tree` is the only graph-rendering surface.** Other
  commands (`has`, `cat`) do not surface the multi structure; they
  operate transparently as if the multi were a single store. This is
  by design — the multi exists to be invisible at the read/write
  surface.
- **No quota or budget enforcement.** A `write_through` multi with
  `read_fill = true` will tee unboundedly into its write store. If
  the write store fills up, writes start failing — same failure mode
  as writing to a full single-store today.

## More Information

- **Depends on FDR-0008 (`docs/features/0008-config-digest-pins.md`).**
  Phase 1 supplies the body-digest mechanism this FDR's references
  assert against. Phase 2 supplies the digest-bearing blob-store-id
  parser. FDR-0009 cannot ship until both phases of FDR-0008 are at
  `experimental` or later. The FDR-0008 *Future Work* item "Pinning
  persisted references" names exactly this FDR's reference shape.
- **Origin: #182** — the Multi primitive (the implementation FDR-0009
  consumes). Tasks 1–13 of #182 build the Multi type, the builder,
  the tee, and the cross-hash mapping. Tasks 14–17 (the `-multi` CLI
  flag work) are obviated by this FDR; they were cancelled mid-#182
  in favor of this design.
- **Supersedes the `-multi` flag approach.** A previous design
  threaded `-multi` and `-no-read-fill` flags through `cat`, `has`,
  `list`, and `fsck` (referenced in the multi-blob-store-builder
  plan, `docs/plans/2026-05-13-multi-blob-store-builder.md`). That
  surface is replaced by config-driven composition.
- **Related issues.** #156 (stable store keys à la dodder repos) —
  the digest-bearing reference shape from FDR-0008 Phase 2 addresses
  the cross-host stability question for free. #194 (origin of digest
  pins). #145 / #153 (relative-path ambiguity) — orthogonal; the
  multi reference shape uses whatever ID shape Phase 2 ships.
- **Implementation primitive.**
  `go/internal/foxtrot/blob_stores/multi.go` and `multi_builder.go`
  (the type and builder built by #182). FDR-0009's impl adds a thin
  config-resolution layer on top; no change to the primitive itself.
- **Locked wire-format string.** The new `store_type = "multi"` value
  is registered alongside the existing store types
  (`local_hash_bucketed`, `remote_sftp`, …). Codec stability
  convention applies: the string is locked.
- **Manpage.** `docs/man.7/blob-store-multi.md` (currently #195) will
  be re-scoped post-FDR-0009 to cover both the primitive and the
  config-type wrapper.

Signed-off-by: Clown 🤡 <https://github.com/amarbel-llc/clown>
