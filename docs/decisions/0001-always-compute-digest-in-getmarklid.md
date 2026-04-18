---
status: accepted
date: 2026-04-18
decision-makers: Sasha F
---

# Always compute the digest in `Hash.GetMarklId`

## Context and Problem Statement

`Hash.GetMarklId` historically skipped `hash.Sum` when no data had been written, leaving the returned id with `len(data) == 0`. `formatHash.null`, initialised at package load via `hash.Sum(nil)`, instead holds the digest of the empty input (`len(data) == hash.Size()`). `Id.IsNull` smoothed over the divergence by treating `len == 0` **and** bytes-equal-to-`formatHash.null` as equivalent null states, but every downstream path that byte-compares, serialises, or encodes an id implicitly relied on that smoothing. The TODO at `go/internal/bravo/markl/hash.go:53` ("verify this works as expected") was the author flagging exactly this uncertainty.

## Decision Drivers

* A single byte representation for "null id" is easier to reason about than two that are kept equivalent by a helper.
* Any future code path that reaches for `bytes.Equal`, `String()`, or a binary encoder without going through `IsNull` would observe the divergence as a bug.
* Removing the branch must not regress production hot paths.

## Considered Options

1. **Retire the branch — always call `hash.Sum`.** Every id gets `len == hash.Size()`. The byte-level divergence disappears.
2. **Tag nullness explicitly.** Add an `isNull bool` to `Id`, keep the short representation, but make the invariant self-describing instead of inferred.
3. **Shared-pointer copy-on-write.** Point an unwritten id's `data` at `formatHash.null.data` and detach on any mutation. Zero-copy fast path preserved, mutation hazard contained behind an `ensureOwned` helper.

## Decision Outcome

Chosen option: **"Retire the branch"**, because benchmarks show the branch guards a cold, defensive path that no caller exercises in practice, and the invariant simplification retires a class of latent correctness risks at sub-microsecond cost.

`BenchmarkGetMarklId` (`go/internal/bravo/markl/hash_bench_test.go`, 5 runs, Intel i7-1165G7):

| Case | With branch | Always Sum | Δ ns/op | Allocs/op |
|---|---:|---:|---:|---:|
| SHA256/NoWrite | 130 ns | 182 ns | +52 | 4 → 4 |
| SHA256/Write_64B | 223 ns | 226 ns | +3 | 4 → 4 |
| SHA256/Write_1KB | 772 ns | ~718 ns | noise | 4 → 4 |
| SHA256/Write_64KB | 35,312 ns | 34,963 ns | noise | 4 → 4 |
| Blake2b/NoWrite | 130 ns | 240 ns | +110 | 4 → 4 |
| Blake2b/Write_64B | 247 ns | 248 ns | noise | 4 → 4 |
| Blake2b/Write_64KB | 52,102 ns | 51,742 ns | noise | 4 → 4 |

The branch saves 50–110 ns only on the `NoWrite` path, with zero allocation delta. An audit of `GetMarklId` call sites (`markl_io/writer.go:98`, `env_dir/blob_writer.go:96`, `foxtrot/blob_stores/multi.go:107`, `markl_io/reader.go:135,171`) shows every caller writes or reads data before calling `GetMarklId`, so the `NoWrite` path is not on any production hot path.

### Consequences

* Good, because every `Id` returned by `GetMarklId` now satisfies `len(data) == format.GetSize()`, making byte-equality across null representations a non-issue.
* Good, because the `// TODO verify this works as expected` comment and its implicit invariant debt are retired.
* Bad, because the defensive `GetMarklId()`-on-unwritten-hash path is ~100 ns slower. No known caller exercises it.
* Neutral, because `Id.IsNull`'s `len(data) == 0` branch is retained — it still catches zero-value `Id{}` structs that were never produced by a `Hash`.

### Confirmation

* `BenchmarkGetMarklId` lives in `go/internal/bravo/markl/hash_bench_test.go` and documents the cost.
* Existing `TestId` (`go/internal/bravo/markl/id_test.go:90-112`) continues to pass: `AssertEqual(idZero, idNull)` holds because `IsNull` still treats zero-value `Id{}` as null.

## Pros and Cons of the Options

### Retire the branch

* Good, because the invariant is trivially stated: every hashed id has `len == Size`.
* Good, because no audit of serialisers, encoders, or comparators is required.
* Neutral, because the pool machinery still accounts for 4 allocs/op; no memory-profile change.
* Bad, because the cold `NoWrite` call becomes ~40–85% slower in relative terms (tens to low hundreds of nanoseconds absolute).

### Tag nullness explicitly

* Good, because the invariant is encoded in the type rather than reconstructed from bytes.
* Good, because the fast path for the unused-hash case stays fast.
* Neutral, because `Id`'s memory footprint grows by one bool (padded).
* Bad, because every `IsNull`, equality, serialisation, and comparison path must be updated to consult the tag; any missed site reintroduces the divergence.

### Shared-pointer COW

* Good, because it realises the original optimisation intent (no copy for the null case) safely.
* Bad, because `slices.Grow` on a shared slice silently returns the same backing array when capacity suffices — every mutation site (`allocDataIfNecessary`, `allocDataAndSetToCapIfNecessary`, `setData`, `Sum`) must be routed through a `detach` helper, and one missed site corrupts the package-level constant.
* Bad, because `GetBytes()` callers would need a documented "read-only" contract that is currently unenforced.
* Bad, because the audit and testing cost dwarfs the ~100 ns it saves.

## More Information

* Baseline benchmark results: `.tmp/bench_baseline.txt` (local; not checked in).
* Current callers audit via `rg "GetMarklId\(\)" go/internal` — all call sites write or read data prior to invoking.
