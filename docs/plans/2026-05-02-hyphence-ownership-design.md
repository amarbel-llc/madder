# Hyphence ownership convergence — design

**Status:** completed 2026-05-02 (see Addendum below — the implementation collapsed to docs-only).

**Date:** 2026-05-02

**Tracks:** [#115](https://github.com/amarbel-llc/madder/issues/115) (closed as completed)

**Related:** dodder #41 (the production issue motivating dodder's strict-mode extensions), [#112](https://github.com/amarbel-llc/madder/issues/112) (parallel pattern: deleting dodder vocab from madder once dodder takes over its registrations), [dodder #157](https://github.com/amarbel-llc/dodder/issues/157) (the dodder-side migration follow-up filed after #115 shipped)

> ## Addendum (2026-05-02 — post-implementation)
>
> This design assumed the code-port from dodder was the bulk of the work. **It turned out the port was already done in madder when the design was written** — `BlobTeeWriter`, `AllowMissingSeparator`, `peekSeparatorLine`, both boundary helpers, and all 7 separator-discipline tests were already at parity with dodder's. The actual implementation collapsed to docs-only:
>
> - Man page rebrand (drop "Dodder" branding, normative wording).
> - `docs/rfcs/0001-hyphence.md` — project's first RFC, with formal MUST/SHOULD/MAY language and 26 normative statements.
> - Test-vector file (`go/internal/charlie/hyphence/testdata/rfc_vectors.txt`) + conformance test harness (`TestRFCConformance_HyphenceTestVectors`).
>
> See `docs/plans/2026-05-02-hyphence-ownership.md` for the docs-only implementation plan and the close-issue comment on #115 for the recap. The "Code changes" section below describes work that did NOT need to happen — kept here for historical context.

## Goals

- Madder owns the canonical hyphence implementation; dodder drops its `internal/charlie/hyphence` fork and consumes `pkgs/hyphence` directly.
- Madder's hyphence implementation enforces the body-separator rule the man page already documents (strict default), with `AllowMissingSeparator` as opt-in for legacy on-disk reads.
- Hyphence as a *named format* gets first-class spec documentation: an RFC with normative MUST/SHOULD/MAY language plus the existing user-facing man page, decoupled from dodder branding.
- Dodder's RFC enforcements (`BlobTeeWriter`, `peekSeparatorLine`, boundary helpers, separator-discipline tests) live in madder going forward.

## Non-goals

- Extracting hyphence to a standalone repo or to `dewey`. Convergence first; pre-birth nothing.
- Coordinating dodder's migration into the same PR. Madder ships the public surface and the RFC; dodder picks up at its own cadence as a follow-up there.
- Re-evaluating any other line-prefix semantics (`#`, `-`, `@`, `!`, `<`, `%`). The metadata-line vocabulary stays exactly as the man page documents it.
- Versioning the format itself. Hyphence stays version-less by design; type strings carry version evolution per the existing convention.

## Code changes

### Port from dodder into `internal/charlie/hyphence`

- `decoder.go`: add `BlobTeeWriter io.Writer` and `AllowMissingSeparator bool` fields on `Decoder`. Default zero-value behavior is strict (matches dodder's default). Add the `peekSeparatorLine()` validator that fires when `AllowMissingSeparator` is false and the body would otherwise start without a blank line; on violation, return a typed error.
- `boundary.go`: add `ReadBoundaryFromRingBuffer` and `PeekBoundaryFromPeeker` helpers. Both are pure additions — no signature changes to existing helpers.
- `coder_metadata_test.go`: port the 7 separator-discipline test functions verbatim. They exercise both Reader and Decoder paths and cover both directions of the `AllowMissingSeparator` toggle.

### Decoder behavior change

Before: madder's decoder accepts a missing blank line, silently producing an object with empty body or empty digest.

After: that input errors with a typed `ErrMissingBodySeparator` (or whatever name dodder uses — port the existing one) unless `AllowMissingSeparator: true`.

### Madder writer audit

Issue #115 claims madder's existing emitters all already write the separator. Step 1 of the implementation will be a grep + spot-check across madder's 14 import sites to verify before flipping the default, so we don't ship a regression. If any madder emitter is missing the separator, fix it (it's a one-line fix per emitter — match what dodder already does).

**Spot-check sites of varying risk:**

- `internal/foxtrot/blob_stores/store_remote_sftp.go` — reads `blob_store-config` from SFTP. Remote-written by madder itself, should be fine; worth a real-data spot-check before merge.
- `internal/charlie/capture_receipt/v1_io.go` — reads capture receipts. Madder-written so should conform.
- `internal/india/commands/init_from.go` — reads import inputs that *may* have non-madder origin. **Uncertainty (likely low risk):** if `init_from` accepts hyphence input from external tools, it may need `AllowMissingSeparator: true` for the lenient-import path. Verify at implementation time; do not block design on this.

### API additions to public facade

`pkgs/hyphence/main.go` regenerates via dagnabit; the new `BlobTeeWriter`/`AllowMissingSeparator` fields appear automatically since they're exported on `Decoder`. The error type gets re-exported. No call-site changes needed in madder itself — strict default and zero-value `AllowMissingSeparator` mean callers get the new behavior without code edits.

### Out of scope for this PR

- Any refactoring of metadata-line parsing.
- Any change to `Coder` / `Encoder` / `CoderTypeMap` semantics.
- Any rename of files. Pure additive port.

## Spec docs

### Update `docs/man.7/hyphence.md`

- Title `HYPHENCE(7) Dodder | Miscellaneous` → `HYPHENCE(7) Madder | Miscellaneous`.
- Drop dodder-specific framing: "primary persistence and interchange format for dodder objects" → "text-based serialization format for object metadata + body, used by madder and dodder."
- Promote the "BODY SEPARATOR" warning to normative form: "Decoders MUST reject input that omits this blank line unless explicitly configured to accept legacy data."
- Add `SEE ALSO` reference to the new RFC.

### Add `docs/rfcs/0001-hyphence.md` (project's first RFC)

This work establishes `docs/rfcs/NNNN-title.md` as the convention (zero-padded 4 digits, lowercase title-with-dashes, parallel to ADRs in `docs/decisions/`). Use the eng:rfc skill when actually drafting the body.

Sections:

1. **Status / metadata** — proposed → accepted on merge.
2. **Abstract** — what hyphence is, in two sentences.
3. **Notational conventions** — RFC 2119 MUST/SHOULD/MAY language.
4. **Document grammar** — formal grammar (regex + production rules) for boundary lines, metadata lines, body separator.
5. **Metadata line types** — normative spec for `#`, `-`, `@`, `!`, `<`, `%` semantics, content constraints, ordering rules. Man page is descriptive; RFC is prescriptive.
6. **Decoder behavior** — strict mode is normative ("decoders MUST reject…"). The lenient `AllowMissingSeparator` mode is documented as an *implementation accommodation* for legacy data, NOT part of the format spec.
7. **Encoder behavior** — canonical line order MUST be followed when emitting; decoders MUST accept any order when reading.
8. **Versioning** — type-string-driven evolution; format itself is unversioned.
9. **Examples** — same set as the man page, framed as test vectors.
10. **Test vectors** — `(input bytes, expected parse result)` pairs that any conforming implementation MUST pass. Include failure cases.

The RFC stays small (probably ~150 lines). The test-vector section is what makes it actionable for a future implementer.

### Cross-references

- Man page refers to the RFC for normative language.
- RFC refers to the man page for tutorial-style examples.
- `go/internal/charlie/hyphence/CLAUDE.md` gets a one-line pointer to both.

## Migration / dodder coordination

### Madder side, this PR

Single PR covering: code port (decoder.go fields + peekSeparatorLine + boundary helpers + tests), man page rebrand, RFC doc, and the writer audit fix-ups (if any madder emitter turns out to be missing the separator). Self-contained — `just test-go` and the bats suite green it before merge.

### Dodder side, separate PR in dodder's repo

Once madder ships:

1. Drop `dodder/internal/charlie/hyphence/` entirely.
2. Replace 25 import sites: `dodder/internal/charlie/hyphence` → `github.com/amarbel-llc/madder/go/pkgs/hyphence`.
3. Where dodder's reads cross legacy on-disk objects without the separator, set `AllowMissingSeparator: true` on the relevant `Decoder`. Need dodder-side audit to enumerate these.
4. Run dodder's full test suite to confirm the strict default doesn't break anything other than the explicitly-flagged legacy-read paths.

### Coordination signal

Once madder's PR merges, file a tracking issue on dodder (or comment on dodder's existing tracking issue if there is one) referencing the merged commit. **Don't block the madder PR on dodder readiness** — madder ships first, dodder follows when convenient.

### No flag day

Old/new aren't being switched; new behavior is additive (a stricter check by default). The dual-architecture is the toggle itself, exposed continuously from day one.

## Rollback strategy

### Dual-architecture period

The `AllowMissingSeparator bool` field on `Decoder` is itself the dual-architecture: strict (default) and lenient (opt-in) coexist continuously. Either behavior is reachable from any caller, so a regression in strict-default doesn't require a code revert — just a one-line flip at the affected call site.

### Rollback procedure for the strict-default flip

If the audit missed a madder emitter that lacks the separator and a regression appears post-merge:

1. **Quick fix (1 line):** at the breaking call site, set `AllowMissingSeparator: true` on the `Decoder`. Ship as a follow-up commit.
2. **Multiple sites broken:** flip the package-wide default by changing the zero-value semantics — the field becomes `EnforceBodySeparator bool` (negated) so zero-value is lenient. One-line implementation change in `decoder.go`. The "blast door" rollback.
3. **Full revert:** `git revert <merge-commit>`. Single-PR shape means revert is a single-commit operation.

### Promotion criteria — when do we delete the lenient toggle?

**Never, deliberately.** `AllowMissingSeparator` stays as a permanent affordance for legacy-data reads. Dodder will need it for its on-disk objects predating its own #41 fix; cost of keeping a 5-line toggle indefinitely is negligible. The criterion isn't "remove the toggle" — it's "no madder code path defaults to lenient" (already the post-PR state).

### RFC rollback

Editorial. If implementation experience proves the strict requirement was wrong, supersede via `docs/rfcs/0002-hyphence-v2.md` — same pattern as ADRs.

## Testing

### Inherited from dodder

The 7 separator-discipline tests in `coder_metadata_test.go` come over verbatim:

- `TestReaderMissingNewlineBetweenBoundaryAndBlobShouldFail` — Reader-level rejection.
- `TestDecoderMissingNewlineBetweenBoundaryAndBlobShouldFail` — Decoder-level rejection.
- `TestReaderAllowMissingSeparatorForwardsBlobContent` — opt-in lenient reads.
- `TestDecoderAllowMissingSeparatorForwardsBlobContent` — same at Decoder level.
- Three roundtrip tests for Writer/Encoder output → Decoder input under both modes.

### New madder tests

- `TestDecoder_BlobTeeWriter_CapturesBody` — verifies the new `BlobTeeWriter` field actually receives the blob bytes during decode.
- `TestErrMissingBodySeparator_TypeAssertable` — verifies callers can `errors.As` the error to make programmatic decisions (parallel to `ErrIsNullPurposeExtractable` in markl).
- `TestRFCConformance_HyphenceTestVectors` — consumes the RFC's test-vector section (input bytes → expected parse outcome) from a `testdata/` file. Keeps RFC and code from drifting silently.

### Existing madder coverage stays

The 14 import sites in madder go through their own test paths (bats, blob-store fsck, capture roundtrips). Pre-merge runs all of them with the new strict default; the writer audit catches any silent breakage.

### bats coverage is the practical confidence net

End-to-end bats suite (82 tests + 33 net-cap) reads and writes hyphence content via the actual `madder` binary in dozens of paths. Already runs in pre-merge.

### Test-vector file format

Newline-delimited `(input_hex \t expected_outcome)` pairs. Each test case in `TestRFCConformance_HyphenceTestVectors` iterates the file. Reusable pattern for any future RFC with test vectors; light enough not to deserve its own framework.

### Skill-level gaps — none

No fuzzing, no property-testing, no benchmark suite changes for this work. Format is small and surface is well-bounded.

## Order of operations (implementation plan handoff)

The detailed step-by-step lives in the implementation plan (next step: invoke `/eng:writing-plans`). Approximate sequence:

1. Writer audit across the 14 madder import sites.
2. Port code (`decoder.go` fields, `boundary.go` helpers, `errors.go` typed error).
3. Port tests verbatim.
4. Add new madder-side tests (`BlobTeeWriter`, `errors.As`, RFC test vectors).
5. Update man page (title rebrand, normative warning).
6. Draft `docs/rfcs/0001-hyphence.md` via the eng:rfc skill.
7. Generate test-vector file from RFC examples; wire `TestRFCConformance_HyphenceTestVectors`.
8. Run full pre-merge.
9. Commit, merge.
10. File dodder tracking issue referencing the merged commit.
