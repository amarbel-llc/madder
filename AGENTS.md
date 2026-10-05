# madder

Madder is a content-addressable blob storage CLI. The entry point is
`go/cmd/madder/`; the build also produces sibling binaries
`madder-cache` and `madder-mcp` from the same Go module, plus their
man pages. `go/cmd/mad/` is a thin alias entry point (`cmd/mad`,
mirroring dodder's `der`) that runs the same `madder` command tree
under the shorter program name; it shares `madder`'s XDG scope and man
pages, it just isn't the utility that generates them.

## Testing lanes

The nix lanes are authoritative. Every bats lane is a derivation built
from the same `$out/bin/madder` that `.#madder` produces, so the
dev loop and CI share one cache:

- `just test-bats` — `.#bats-default` (the `!net_cap,!piv_agent` filter)
- `just test-bats-net-cap` — `.#bats-net_cap` (SFTP/WebDAV harnesses,
  self-sufficient via `netCapExtraBinaries`)
- `just test-bats-piv-agent` — `.#bats-piv_agent` (madder against the
  real piggy-agent over fibby, piggy's virtual PIV card, via
  `pivAgentExtraBinaries`, which also carries the SFTP fixture server
  for the sealed-key-over-SFTP tests; a Rust build, Linux only)
- `just run-bats-tags <tag>` — `.#bats-<tag>`, one lane per unique
  `# bats file_tags=` directive, auto-discovered at flake-eval time
- `just run-bats-race` / `just run-bats-cover` — race- and
  coverage-instrumented variants

There is deliberately **no devshell lane driven from the root
justfile**. `run-bats-targets` (and its `zz-tests_bats/test-targets`
delegate) were dropped in favor of the above.

### Caveats of the nix-only arrangement

`mkBatsLane` (`go/default.nix`) exposes exactly one selector: `filter`,
forwarded verbatim to `bats --filter-tags`. That constrains the dev
loop in ways worth knowing before you go looking for a flag that
isn't there:

- **No per-test selection.** Nothing wraps `bats --filter <regex>`. To
  run a single `@test`, invoke bats directly in the devshell. (This was
  never plumbed in the old local lane either — dropping it cost
  nothing here.)
- **Per-file selection works only through tags.** By convention each
  `.bats` file carries its own tag, so `.#bats-<tag>` is effectively
  per-file. An ad-hoc subset of files that do *not* share a tag is not
  expressible.
- **Per-test `# bats test_tags=` generate no lane.** Discovery scans
  only `file_tags`. Since `filter` is forwarded verbatim, calling
  `mkBatsLane { filter = "sometag"; }` directly does match them.
- **No rerun-only-failed.** `--filter-status failed` needs prior-run
  state on disk; a derivation starts clean every build.
- **No ad-hoc selectors at all, structurally.** Lane outputs are
  enumerated at flake-eval time, so `nix build` cannot take a runtime
  selector — `.#bats-<X>` must already exist as an attribute. A
  `nix run .#bats -- <args>` app is the shape that would fix this.
- **Silence on success.** A passing derivation prints nothing; use
  `nix log` for the TAP stream and `--keep-failed` to keep artifacts.

These gaps are tracked in madder#288. The test-name-filter half is
upstream-owned — `batsLane` lives in the `bats` flake input
(`code.linenisgreat.com/bats`) and would have to accept the selector
before `mkBatsLane` could forward it — tracked there as bats#40.

`zz-tests_bats/justfile` still holds devshell recipes (`test`,
`test-tags`, `test-net-cap`) for running bats against `$PWD`'s
binaries. They are **not authoritative**, they bypass the nix sandbox,
and they can fail on hosts where the bats wrapper's sandbox cannot
initialize (observed: `failed to initialize Linux bridge`). Prefer the
nix lanes; reach for these only when you specifically need bats' own
flags.

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
(`code.linenisgreat.com/dodder`). Dodder is an immutable cryptographic
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
  shared `dewey` library (`code.linenisgreat.com/purse-first/libs/dewey`).
- Madder's go module is `code.linenisgreat.com/madder/go`.

## Interpreting `dodder` references in this codebase

A fresh reader naturally sees every `dodder` reference as a pointer to a
currently-maintained sibling project that madder is coupled to. Almost
always, that is wrong. The remaining references fall into these buckets:

### Legacy wire format — intentional, do not rename

Protocol identifiers that are written into files and read by dodder itself.
Renaming them in madder desyncs the wire format. Tracked separately in
[#16](https://code.linenisgreat.com/madder/issues/16).

- `go/internal/charlie/markl_registrations/main.go` — registers the
  legacy purpose-id aliases (`dodder-repo-private_key-v1`,
  `zit-repo-private_key-v1`) needed to read pre-rename on-disk blob
  stores. The `dodder-*` purposes themselves are registered by dodder
  ([#255](https://code.linenisgreat.com/madder/issues/255)); their
  string constants live upstream in piggy's `go/` module
  (`code.linenisgreat.com/piggy/go/pkgs/markl` — the markl core
  moved there under piggy#183; madder's `go/internal/bravo/markl/` and
  `go/internal/alfa/blech32/` were deleted in the cutover).
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
  `amarbel-llc/hyphence` — see madder#253. The `markl-id.md` man page
  is maintained in madder as a local reference but the normative
  wire-format spec is **piggy RFC 0011** at
  `code.linenisgreat.com/linenisgreat/piggy`; `docs/rfcs/0002-markl-id-format.md`
  is now a superseded stub — see madder#274.)
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
  [#19](https://code.linenisgreat.com/madder/issues/19).

### Already migrated

- XDG utility name and env vars — `XDGUtilityNameDodder`, `DIR_DODDER`,
  `BIN_DODDER`, `DODDER_XDG_UTILITY_OVERRIDE` were renamed or dropped under
  [#42](https://code.linenisgreat.com/madder/issues/42) (commit
  `677007a`). Runtime now resolves under `$XDG_*_HOME/madder/`. Test
  `go/internal/echo/env_dir/env_var_names_test.go` pins the current names.

When in doubt about a `dodder` reference, map it to one of the buckets
above. Wire-format strings in particular cannot be silently renamed — ask
before touching them.
