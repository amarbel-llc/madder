# madder

Madder is a content-addressable blob storage CLI. The entry point is
`go/cmd/madder/`; the build also produces sibling binaries
`madder-cache` and `madder-mcp` from the same Go module, plus their
man pages.

## External consumer: cutting-garden

`amarbel-llc/cutting-garden` is the standalone filesystem-tree
capture/restore CLI. It used to live in-tree under
`go/cmd/cutting-garden/` + `go/internal/india/commands_cutting_garden/`
but moved out as part of madder#216 (Phase 6 cutover, 2026-05-26).
Cutting-garden now consumes madder as a library via the public
`pkgs/` substrate — `pkgs/blob_store_env`, `pkgs/env_dir`,
`pkgs/madder_env`, `pkgs/tap_diagnostics`, `pkgs/arg_resolver`,
`pkgs/output_format` — and ships its own binary + receipt-format
spec. The capture-receipt wire-format type tag
`cutting_garden-capture_receipt-fs-v1` is now owned entirely by
cutting-garden; madder no longer reads or writes it.

The extraction design is recorded at
`docs/plans/2026-05-10-extract-cutting-garden-design.md` (historical
reference). When updating madder's public `pkgs/` surface, consider
that cutting-garden is a downstream consumer; breaking changes there
should be coordinated.

## History: madder was extracted from dodder

Madder was extracted in April 2026 from a larger project called **dodder**
(`github.com/amarbel-llc/dodder`). Dodder is an immutable cryptographic
object graph inspired by Git, Nix, and Zettelkasten. Madder is the blob
store layer that dodder is built on; it was pulled into its own repo so it
can be built, tested, and released on its own cadence. Dodder is still
actively maintained — the two repos are now peers, not parent/child.

The extraction is recorded in `docs/plans/extract-from-dodder.md` and in
commit `92aa28a` ("Extract madder from dodder with dewey dependency"). Key
mechanical shape:

- Internal packages were copied out of dodder's `go/internal/` (layers 0
  through india).
- Imports that used to point at dodder's `go/lib/` were rewritten to the
  shared `dewey` library (`github.com/amarbel-llc/purse-first/libs/dewey`).
- Madder's go module is `github.com/amarbel-llc/madder/go`.

## Interpreting `dodder` references in this codebase

A fresh reader naturally sees every `dodder` reference as a pointer to a
currently-maintained sibling project that madder is coupled to. Almost
always, that is wrong. The remaining references fall into these buckets:

### Legacy wire format — intentional, do not rename

Protocol identifiers that are written into files and read by dodder itself.
Renaming them in madder desyncs the wire format. Tracked separately in
[#16](https://github.com/amarbel-llc/madder/issues/16).

- `go/internal/charlie/markl_registrations/main.go` — registers the
  `dodder-*` purposes (`dodder-repo-public_key-v1`,
  `dodder-object-digest-v2`, `dodder-request_auth-response-v1`, etc.)
  against the markl framework, transitionally until dodder registers
  its own ([#255](https://github.com/amarbel-llc/madder/issues/255)).
  The purpose string constants themselves now live upstream in piggy's
  `go/markl` module (`github.com/amarbel-llc/piggy/go/markl/pkgs/markl`
  — the markl core moved there under piggy#183; madder's
  `go/internal/bravo/markl/` and `go/internal/alfa/blech32/` were
  deleted in the cutover).
- `go/internal/bravo/directory_layout/util.go` —
  `fileNameBlobStoreConfigLegacy = "dodder-blob_store-config"` kept for
  reading pre-rename on-disk blob stores.

### Lineage prose — informational, not a live dependency

References that describe dodder's data model or link back to dodder for
context. Madder operates on that data model, so the prose is accurate
domain description.

- `docs/man.7/{blob-store,markl-id}.md` — describe "dodder
  objects", "dodder repositories", and the `dodder-*` markl-id scheme.
  Dodder is the canonical owner of these concepts. (The `hyphence.md`
  man page is now a redirect stub; the format moved to
  `amarbel-llc/hyphence` — see madder#253.)
- Subpackage AGENTS.md files and `futility` comments refer to "dodder" or
  "dodder-style commands" because the text hasn't been re-homed; these are
  stale prose, not active couplings.
- `go/internal/futility/app_test.go` uses `"dodder"` as a sample utility
  name in fixtures.

### Speculative TODOs — not active integration

- `go/internal/alfa/inventory_archive/base_selector_size.go` has TODOs like
  *"madder queries dodder for blob type info"* describing a hypothetical
  future pack-blobs strategy from the era when madder ran inside dodder.
  These are design speculation, not current behavior. Triaged alongside the
  broader TODO sweep in
  [#19](https://github.com/amarbel-llc/madder/issues/19).

### Already migrated

- XDG utility name and env vars — `XDGUtilityNameDodder`, `DIR_DODDER`,
  `BIN_DODDER`, `DODDER_XDG_UTILITY_OVERRIDE` were renamed or dropped under
  [#42](https://github.com/amarbel-llc/madder/issues/42) (commit
  `677007a`). Runtime now resolves under `$XDG_*_HOME/madder/`. Test
  `go/internal/echo/env_dir/env_var_names_test.go` pins the current names.

When in doubt about a `dodder` reference, map it to one of the buckets
above. Wire-format strings in particular cannot be silently renamed — ask
before touching them.
