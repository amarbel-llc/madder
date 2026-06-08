# Design: sync adopts go-crap v2 (ndjson-crap) — tracer bullet

**Date**: 2026-06-07
**Status**: approved
**Scope**: `madder sync` only; other streaming commands follow up separately.

## Goal

Tracer bullet proving madder can consume the just-released go-crap v2
(`github.com/amarbel-llc/crap/go-crap`, ndjson-crap canonical wire format,
RFC 0001 `go-pkgs` flake output) end to end: flake wiring → go.mod →
a real command emitting ndjson-crap that `crap-present` can render.

**Acceptance gate**: the user's personal test — a sync check against the
built binary.

## 1. Dependency wiring

Consumer-side change following the existing tap/tommy pattern
(flake-input-go_mod protocol, RFC 0001; see madder#208/#211/#213):

- `flake.nix`: new input `crap = github:amarbel-llc/crap` with the usual
  `follows` (igloo, nixpkgs-master, utils, bats).
- `go/gomod.nix`: `goFlakeInputs."github.com/amarbel-llc/crap/go-crap" =
  { src = crap.packages.${system}.go-pkgs; }` — crap's `go-pkgs` is built
  from the `./go-crap` subtree, so no `subPath`.
- `go/go.mod`: `require github.com/amarbel-llc/crap/go-crap <version>` +
  `just gomod2nix`.

**Known risk (verify first)**: the upstream release tag is `go-crap/v2.0`
— not valid Go-module semver (no patch component), and the module path
lacks the `/v2` suffix a v2 module requires. Theory: go.mod can only pin
a pseudo-version (`v0.0.0-20260608…-5f5a10b…`). Plan: start with the
pseudo-version to keep madder unblocked; file a crap issue to retag
`go-crap/v2.0.0` with module path `…/go-crap/v2` if confirmed.

## 2. Format surface

> **Revision (2026-06-08): sync presents the viewport natively on a TTY.**
> The original draft kept TAP as sync's interactive default. Corrected:
> `madder sync` on a TTY now renders the go-crap **viewport** itself
> (in-process, live), and TAP is dropped from sync entirely.

- `output_format` gains `FormatCRAP = Format("crap")` (flag value,
  completion text, FlagDescription) and an exported `IsTTY(*os.File) bool`
  helper. `FormatTAP` stays in the package — other commands still use it;
  only **sync** drops it.
- `Resolve`/`ResolvePipedDefault` remain for the other streaming commands
  (their interactive default is still TAP; their piped adoption of crap is
  tracked in madder#234). sync does NOT use `ResolvePipedDefault` because
  its TTY branch is the viewport, not a wire format.
- **sync's resolution** (in `runStore`):
  - `auto` + TTY → native viewport: `viewport.Present(os.Stdout, …)` fed by
    `syncCrapSink` over an `io.Pipe` (the in-process form of
    `madder sync | crap-present`). The viewport renders to stdout (the tty);
    no wire bytes hit stdout in this mode.
  - `auto` + piped → ndjson-crap wire (`syncCrapSink` → stdout).
  - `-format crap` → raw ndjson-crap wire to stdout, even on a TTY (escape
    hatch to see/redirect the records).
  - `-format ndjson` / `-format json` → legacy `{id,state,size,error}`
    records (byte-identical to before).
  - `-format tap` → **rejected by sync** with a guiding error (the shared
    flag still parses `tap` for other commands; sync refuses it at runtime).
- `syncTapSink` is deleted.
- Viewport-mode plumbing: producer (the blob loop + summary + finalize)
  runs on the main goroutine so all `req` interaction stays single-threaded;
  `viewport.Present` runs in a goroutine reading the pipe; `pw.Close()`
  after `finalize()` gives the reader EOF so `Present` returns. Notices
  (e.g. "limit hit") are discarded in viewport mode — the Summary record is
  rendered natively. Records are producer-generated and always valid, so the
  viewport's decoder cannot error mid-stream (the only deadlock edge).
- Command help + man text document the new default and the opt-out.
- `output_format` is a `dagnabit export` facade source
  (`pkgs/output_format`, consumed by cutting-garden). The additions are
  additive; `just generate-facades` is part of the work.

Flip scope decision: **sync only**. Other commands keep auto→ndjson until
they grow crap sinks (follow-up issue). fsck's adoption also interests
dodder (dodder#243 wants the viewport progress UI for its fsck).

## 3. The crap sink (record mapping)

New `syncCrapSink` implementing `syncSink` over `ndjsoncrap.Writer`:

| sync event | ndjson-crap record |
|---|---|
| stream start | `Meta{Title:"sync", Source:"madder"}` header |
| `transferred(id, n)` | `Test{N:seq, OK:true, Description:"<id> (<size>)", Diagnostic:{"state":"transferred","size":n}}` |
| `exists(id)` | `Test{OK:true, Directive:{Kind:"skip", Reason:"exists"}, Diagnostic:{"state":"exists"}}` |
| `failed(id, n, err)` | `Test{OK:false, Diagnostic:{"state":"failed","error":err}}` |
| `listError(err)` | `Test{OK:false, Description:"(unknown blob)", Diagnostic:{"state":"list_error","error":err}}` |
| `notice(msg)` | stderr (same as JSON mode today) |
| `bailOut(msg)` | `Bailout{Message:msg}` |
| `finalize()` | `Summary{Passed, Failed, Skipped, Total, Valid:true}` + flush |

Interface adjustments:

- sink-internal monotonic test-point counter for `Test.N`;
- `syncSink` grows `summary(succeeded, failed, ignored, total)`, called
  from the existing deferred block. TAP impl: today's `Comment`; JSON
  impl: today's stderr line; crap impl: the `Summary` record.

No `Plan` record: the schema requires Plan to precede the first test and
sync streams without an upfront count; `Summary.PlanCount` carries the
total instead (exact semantics checked against
`docs/ndjson-crap-schema.md` during implementation).

## 4. Testing

- Unit: table test for `syncCrapSink` asserting emitted lines decode via
  `ndjsoncrap.Reader` round-trip; `ResolvePipedDefault` cases. Run via
  `just test-go` (the `-tags test` lane).
- bats: existing piped-sync tests gain an explicit `-format ndjson`
  (they now test the opt-out); one new case asserts the piped default is
  ndjson-crap (`"type":"crap"` header + `summary` line). Merge hook runs
  the lane.
- End-to-end: `madder sync … | crap-present` viewport render, verified
  manually (`crap-present` is not in madder's devshell).
- **Gate**: user's personal sync check on the built binary.

## 5. Rollback strategy

Dual-architecture is inherent: all three legacy lanes (`tap`, `ndjson`,
`json`) remain intact and reachable.

- **Rollback procedure**: one-line change — sync reverts to
  `Resolve(os.Stdout)` (auto→ndjson piped); `-format crap` stays
  available but non-default. Scripted consumers self-serve instantly via
  `-format ndjson`.
- **Promotion criteria**: fsck/write/pack-blobs have crap sinks
  (follow-up) and a release cycle passes with no `-format ndjson`
  regressions; then fold the piped-crap default into shared `Resolve`
  and deprecate the legacy sync record shape.

## 6. Tuning levers

- **`exists` → skip directive** (current: `Directive{skip, "exists"}`).
  Signal: viewport rendering of skips too noisy on large already-synced
  runs → demote to plain `OK:true`.
- **Diagnostic field names** (`state`/`size`/`error`, mirroring legacy
  records). Signal: a cross-command crap convention emerges (fsck
  follow-up) → align then.

## Follow-ups

1. crap: fix `go-crap/v2.0` tag / module path for Go consumers (pending
   pseudo-version theory confirmation).
2. madder: adopt `-format crap` + piped default in
   fsck/write/pack-blobs/list (fold into shared `Resolve` per promotion
   criteria).
