---
status: accepted
date: 2026-04-24
decision-makers: Sasha F
---

# Per-blob write-log at `$XDG_LOG_HOME/madder/` via a `BlobWriteObserver`

## Context and Problem Statement

madder has no audit or provenance record of blob writes. Once [ADR 0003](0003-blob-store-hardlink-writes.md) landed the `link(2)` + `EEXIST` publish protocol, and [#31](https://github.com/amarbel-llc/madder/issues/31) added opt-in verify-on-collision, a single write call can end in four distinct dispositions — newly written, dedup-skipped, verify-match, verify-mismatch — and none of them are recorded anywhere durable. We want a best-effort append-only log that makes every blob write call visible after the fact, covers all four dispositions, and in future carries a caller-supplied description so a `madder write --log-description '…'` invocation is traceable back to its intent.

## Decision Drivers

* One record per blob *published* — not per `io.Writer` close. The disposition is only known after the publish syscall returns, so the log hook must observe publish, not the bufio-level `Close`.
* Must cover all four dispositions (`written`, `exists`, `verify-match`, `verify-mismatch`). The existing `BlobWriter.Close() error` signature collapses three of them into a nil return.
* `xdg_log_home(7)` constraints: log path is `$XDG_LOG_HOME/madder/…` with `$HOME/.local/log/madder/` as the fallback; records are append-oriented, may grow unbounded, and deletion MUST NOT affect application correctness.
* `Multi.MakeBlobWriter` fans a single write out to N child stores. We want one record per child, so the observer hook lives below the Multi seam.
* Future: `--log-description <str>` flag on the `write` subcommand needs a plumbing path into the observer without requiring a new interface per release.
* Writing to a plain file from concurrent madder processes must not interleave partial records.

## Considered Options

1. **`BlobWriter` decorator, log on `Close`.** Wrap every `domain_interfaces.BlobWriter` at `Multi.MakeBlobWriter` with a decorator that emits one NDJSON line when `Close()` returns. Cheap to implement — one new wrapper, one wiring site.
2. **Log inline in the publish path.** Call a log writer directly from `store_local_hash_bucketed.go` (and its sibling store impls) where the `link(2)` disposition is known. No new interface.
3. **`BlobWriteObserver` interface, observer injected at store construction.** Add a small interface (one method: `OnBlobPublished(storeId, marklId, size, op)`) that each concrete store calls from its publish code after disposition is determined. `env_dir` constructs and injects the observer when it wires the store; tests pass a no-op.
4. **Do nothing; rely on `debug.Options` stderr lines.** Use the existing debug-logging infrastructure. No new file, no XDG plumbing.

## Decision Outcome

Chosen option: **Option 3 — `BlobWriteObserver` interface injected at store construction**, because it cleanly separates the disposition signal (owned by the publish code) from log formatting and lifetime management (owned by a new leaf package), and because it is the only option that faithfully distinguishes all four dispositions.

### Implementation

A new leaf package `go/internal/<slot>/write_log/` (next free Greek slot) owns:

* `type Op string` with constants `"written" | "exists" | "verify-match" | "verify-mismatch"`.
* `type BlobWriteObserver interface { OnBlobPublished(storeId blob_store_id.Id, markl domain_interfaces.MarklId, size int64, op Op) }`.
* A file-backed implementation that opens `$XDG_LOG_HOME/madder/blob-writes-YYYY-MM-DD.ndjson` (resolved via `dewey/echo/xdg`) with `O_WRONLY|O_APPEND|O_CREATE`, reopens lazily when the calendar day rolls over, and emits one NDJSON line per call. Open/append errors are captured once per process via `debug.Options` and otherwise swallowed.
* A no-op implementation returned when logging is disabled.

Record shape:

```json
{"ts":"2026-04-24T12:34:56.789Z","utility":"madder","pid":12345,
 "store_id":"…","markl_id":"sha256-…","size":1234,"op":"written",
 "description":"optional, omitted when empty"}
```

Observer wiring:

* `env_dir` constructs the observer from its XDG layer and hands it to each store at `NewBlobStore…`. The `Multi` store passes its single observer reference down to every child store so each leaf publish call emits a record against its own `store_id`.
* The publish code in `store_local_hash_bucketed.go` (and the equivalent call site in other store impls as they grow write paths) calls `observer.OnBlobPublished(…)` exactly once per publish attempt, after the `link(2)` branch has resolved:
  - Success (`err == nil`) → `op = "written"`.
  - `EEXIST` without verify-on-collision → `op = "exists"`.
  - `EEXIST` with verify-on-collision matched → `op = "verify-match"`.
  - `EEXIST` with verify-on-collision mismatched → `op = "verify-mismatch"` (before the existing error is returned).

The `--log-description` flag on `write` stashes its value in the existing write-path `Config`; the subcommand hands the string to the observer at construction (or via a per-call context), keeping the observer interface unchanged.

### Atomicity and rotation

`O_APPEND` writes ≤ `PIPE_BUF` (4096 B on Linux) are atomic on local filesystems, which covers every expected NDJSON record with generous headroom. Daily rotation is derived from the current wall-clock date at write time — no cron, no rename, no fsync dance: the process opens a new file when it notices the day changed and closes the old handle. Long-running processes only pay the reopen cost once per day.

Rotation produces at most one file per calendar day per madder install. The files accumulate forever; `xdg_log_home(7)` permits unbounded growth and delegates retention to the user.

### CLI and environment

* `--no-write-log` global flag, default `false` (logging on). This flag depends on global-flag support in `futility.Utility.RunCLI` landing first (see Prerequisites).
* `MADDER_WRITE_LOG` environment variable: `0` disables, anything else (or unset) leaves the default on. Lets test harnesses and `just` recipes disable without touching argv.
* `--log-description <str>` on the `write` subcommand (future; interface is forward-compatible).

### Consequences

Good:

* Every blob publish produces one durable record with enough context to reconstruct what was written, when, into which store, and under what disposition.
* Verify-on-collision outcomes become first-class audit events rather than disappearing into a `nil` return.
* Multi-store writes produce one record per child, so dedup / replication behavior is visible without extra instrumentation.
* The observer interface is small, testable in isolation, and trivially stubbed.
* Per-day files keep each on-disk object bounded in size while still letting power-users `cat blob-writes-*.ndjson | jq` across history.

Bad:

* Every store impl now has to call the observer at the right point in its publish path. New store types (remote backends, archive-backed) have to remember to wire it; a regression test per store is required.
* Disk usage grows unboundedly until the user prunes. Consistent with `xdg_log_home(7)` but surprising if a user expected madder to self-manage.
* Concurrent multi-machine writes to a shared `$XDG_LOG_HOME` (NFS, SMB) lose the `O_APPEND` atomicity guarantee. Out of scope — `XDG_LOG_HOME` is user-local by spec — but worth flagging in the manpage.
* Daily rotation is driven by wall-clock date, so a system clock step can produce a second file dated in the past or split a day. Best-effort logging makes this acceptable.

### Confirmation

* A new `blob-write-log` integration test writes a small set of blobs with distinct dispositions (fresh write, duplicate, verify-match, verify-mismatch), then parses the generated NDJSON and asserts one record per publish with the correct `op`.
* Unit test on the observer's open/reopen logic: advance the notion of "today" across a write boundary and assert a new file is created with the expected name.
* `--no-write-log` test asserts zero records and no file creation under `$XDG_LOG_HOME`.

### Reevaluation trigger

Revisit this ADR if any of the following become true:

* Record volume outgrows NDJSON-in-a-file ergonomics (user reports of multi-GB log dirs, `jq` latency). Likely remediation: hourly rotation, or a compact binary log.
* A store impl gains a publish model that can't synchronously name a disposition (e.g. async replication) — the observer call site has to move or grow.
* `dewey/echo/xdg` grows native `LogHome` support; the XDG resolution code in this package collapses to a one-liner.

## Prerequisites

This decision cannot be implemented until two pieces land:

1. **Rename the XDG utility name from `dodder` to `madder`.** Today `go/internal/echo/env_dir/main.go:19` defines `XDGUtilityNameDodder = "dodder"` and `before_xdg.go:23` sets `OverrideEnvVarName = "DODDER_XDG_UTILITY_OVERRIDE"`. Both are marked `TODO`. If we ship the log under `$XDG_LOG_HOME/madder/` while the rest of madder's XDG scope is still `dodder`, the on-disk layout drifts in a way that is harder to reverse than fixing it first. Tracked in its own issue.
2. **Global / persistent flag support in `futility.Utility.RunCLI`.** `go/internal/futility/cli.go:35` explicitly states *"No global flags at present — walk args directly for the subcommand name."* Flags today are per-`Command` only, via `Params []Param`. `--no-write-log` must apply to every subcommand that can publish blobs, which means either the utility-level layer learns to own flags or every affected `Command` has to copy-paste the same `Param`. Tracked in its own issue.

## See Also

* `xdg_log_home(7)` — user-local log base directory, default `$HOME/.local/log`, append-oriented.
* ADR 0002 — content-addressed overwrite-is-fine semantics (makes `op="exists"` a valid success).
* ADR 0003 — `link(2)` + `EEXIST` publish protocol (produces the disposition this log records).
* Issue #31 — verify-on-collision, which introduced `verify-match` / `verify-mismatch`.
