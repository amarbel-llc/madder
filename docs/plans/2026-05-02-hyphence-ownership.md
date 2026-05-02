# Hyphence ownership convergence — implementation plan

> **For Claude:** REQUIRED SUB-SKILL: Use eng:subagent-driven-development to implement this plan task-by-task.

**Goal:** Land the documentation half of #115 — drop dodder branding from the hyphence man page, promote the body-separator rule to normative form, and add `docs/rfcs/0001-hyphence.md` (the project's first RFC) with test vectors. The code-side port is already done in madder; this plan is docs-only plus dodder coordination.

**Architecture:** Madder's `internal/charlie/hyphence` already contains every RFC enforcement #115 contemplated porting (BlobTeeWriter, AllowMissingSeparator, peekSeparatorLine, ReadBoundaryFromRingBuffer, PeekBoundaryFromPeeker, all 7 separator-discipline tests). The actual remaining work is making madder the canonical *spec* owner, not just the canonical implementation.

**Tech Stack:** mandoc(1) (man page rendering), Go testing (test-vector conformance), eng:rfc skill (drafting the RFC body), markdown.

**Rollback:** Pure docs change. Revert via `git revert <merge-commit>`. No production behavior changes; no callers break. The RFC's normative claims about strict-mode defaults already match the implementation, so spec and code can't diverge from this PR.

**Tracks:** [#115](https://github.com/amarbel-llc/issues/115). Design doc: `docs/plans/2026-05-02-hyphence-ownership-design.md`.

---

## Task 1: Sanity-check code parity (no edits expected)

**Promotion criteria:** N/A — verification only.

**Files:**
- Inspect: `go/internal/charlie/hyphence/decoder.go`
- Inspect: `go/internal/charlie/hyphence/boundary.go`
- Inspect: `go/internal/charlie/hyphence/coder_metadata_test.go`

**Step 1: Confirm RFC-enforcement surface is present in madder**

Run:

```bash
rg -n 'BlobTeeWriter|AllowMissingSeparator|peekSeparatorLine|ReadBoundaryFromRingBuffer|PeekBoundaryFromPeeker' go/internal/charlie/hyphence/
```

Expected: at minimum these matches (line numbers may vary):

```
go/internal/charlie/hyphence/decoder.go:15:	AllowMissingSeparator bool
go/internal/charlie/hyphence/decoder.go:17:	BlobTeeWriter         io.Writer
go/internal/charlie/hyphence/decoder.go:154:func (decoder *Decoder[BLOB]) peekSeparatorLine(
go/internal/charlie/hyphence/boundary.go:45:func PeekBoundaryFromPeeker(peeker Peeker) (err error) {
go/internal/charlie/hyphence/boundary.go:69:func ReadBoundaryFromRingBuffer(
```

If any are missing, stop and re-evaluate the design — the assumption underlying this plan is wrong.

**Step 2: Confirm separator-discipline tests pass under strict default**

Run:

```bash
just test-go ./internal/charlie/hyphence/... -run TestReader.*Separator -v
just test-go ./internal/charlie/hyphence/... -run TestDecoder.*Separator -v
just test-go ./internal/charlie/hyphence/... -run TestDecoderBlobTeeWriter -v
```

Expected: PASS for all matched tests.

**Step 3: No commit**

This task is verification only.

---

## Task 2: Man page rebrand and normative wording

**Promotion criteria:** N/A.

**Files:**
- Modify: `docs/man.7/hyphence.md`

**Step 1: Update title and branding**

Edit `docs/man.7/hyphence.md`:

- Line 5: `title: HYPHENCE(7) Dodder \| Miscellaneous` → `title: HYPHENCE(7) Madder \| Miscellaneous`
- Line 10: `hyphence - dodder object serialization format` → `hyphence - text-based metadata + body serialization format`
- Lines 25-28 (DESCRIPTION): replace

  > Hyphence (hyphen-fence) is a text-based serialization format that uses **---** boundary lines to enclose a metadata section. It is the primary persistence and interchange format for dodder objects: repository configs, blob store configs, workspace configs, type definitions, and user-facing zettels all use hyphence.

  with

  > Hyphence (hyphen-fence) is a text-based serialization format that uses **---** boundary lines to enclose a metadata section followed by an optional body. It is used by madder for blob-store metadata and by cutting-garden for capture-receipt metadata, and by dodder for repository configs, blob store configs, workspace configs, type definitions, and user-facing zettels.

**Step 2: Promote the body-separator warning to normative form**

In the BODY SEPARATOR section (around line 72), replace:

```
Without this blank line, the body content is silently dropped during parsing.
```

with:

```
Decoders MUST reject input that omits this blank line. Implementations
MAY expose an opt-in lenient mode for reading legacy data (see the
`AllowMissingSeparator` field on the Go reference implementation), but
that mode is NOT part of the format spec — emitters MUST always include
the separator.
```

**Step 3: Add SEE ALSO reference to the new RFC**

The bottom of the file currently reads:

```
**markl-id**(7), **organize-text**(7), **blob-store**(7)
```

Update to:

```
**markl-id**(7), **organize-text**(7), **blob-store**(7). For the normative format
specification, see `docs/rfcs/0001-hyphence.md`.
```

**Step 4: Verify the man page renders cleanly**

Run:

```bash
just debug-gen_man hyphence.7 | head -20
```

Expected: man page header reflects "Madder" not "Dodder"; no mandoc errors.

(Note: `just debug-gen_man` defaults to `madder.1` — the recipe takes a page name argument.)

**Step 5: Commit**

```bash
git add docs/man.7/hyphence.md
git commit -m "docs(hyphence): drop dodder branding, promote separator rule to normative

Hyphence is the format madder and dodder share, not 'dodder's
serialization format'. Title rebrands to MADDER taxonomy; description
names both consumers; the BODY SEPARATOR warning becomes a normative
MUST, with the lenient AllowMissingSeparator mode flagged as a
non-normative implementation accommodation for legacy data.

References the new docs/rfcs/0001-hyphence.md (added in a follow-up
commit in the same PR).

Tracks #115.

:clown: by Clown — https://github.com/amarbel-llc/clown"
```

---

## Task 3: Add test-vector file scaffolding (no test code yet)

**Promotion criteria:** N/A.

**Files:**
- Create: `go/internal/charlie/hyphence/testdata/rfc_vectors.txt`

**Step 1: Choose the test-vector file format**

Format: tab-separated, one test case per line, fields are:

```
<name>\t<input-base64>\t<outcome>\t<expected-detail>
```

Where:

- `<name>` — short kebab-case test-case identifier (used as `t.Run` subtest name).
- `<input-base64>` — base64-encoded raw bytes of the input. Base64 because the inputs contain newlines and we don't want to escape them.
- `<outcome>` — one of `parse-ok`, `parse-error-missing-separator`, `parse-error-invalid-boundary`.
- `<expected-detail>` — for `parse-ok`, the base64-encoded expected blob body. For errors, ignored (use `-`).

Comments allowed via `#` at the start of a line.

**Step 2: Populate the file with 6 starter test cases**

Create `go/internal/charlie/hyphence/testdata/rfc_vectors.txt` with the following content. (Generate the base64 strings with `echo -n '...' | base64 -w0`; the values below have already been computed.)

```
# RFC 0001 hyphence test vectors. Each line: name\tinput-b64\toutcome\texpected-blob-b64
# `parse-ok` outcomes verify the blob body matches expected. Errors verify
# the named error is returned. Comment lines start with #.

minimal-no-body	LS0tCiEgdG9tbC10eXBlLXYxCi0tLQo=	parse-ok	
body-with-separator	LS0tCiEgbWQKLS0tCgpoZWxsbwo=	parse-ok	aGVsbG8K
body-without-separator	LS0tCiEgdG9tbC10eXBlLXYxCi0tLQpmaWxlLWV4dGVuc2lvbiA9ICdwbmcnCg==	parse-error-missing-separator	-
no-metadata	cGxhaW4gYm9keQo=	parse-ok	cGxhaW4gYm9keQo=
boundary-no-newline	LS0t	parse-error-invalid-boundary	-
empty-input		parse-ok	
```

(Decoded for reference:
- `minimal-no-body`: `---\n! toml-type-v1\n---\n` → ok, no body.
- `body-with-separator`: `---\n! md\n---\n\nhello\n` → ok, body `hello\n`.
- `body-without-separator`: same as `TestDecoderMissingNewlineBetweenBoundaryAndBlobShouldFail` — expect `parse-error-missing-separator`.
- `no-metadata`: `plain body\n` → no boundary line, accepted as bodyless or treated as all-body depending on `RequireMetadata` flag.
- `boundary-no-newline`: `---` (no trailing newline) → `parse-error-invalid-boundary`.
- `empty-input`: empty → ok, no metadata, no body.)

**Step 3: Verify the file is a valid testdata file (no test runs yet)**

Run:

```bash
file go/internal/charlie/hyphence/testdata/rfc_vectors.txt
```

Expected: `ASCII text` (or `UTF-8 text`).

Run:

```bash
awk -F'\t' '!/^#/ && NF != 4 && NF != 0 { print "BAD LINE: " $0 }' go/internal/charlie/hyphence/testdata/rfc_vectors.txt
```

Expected: empty output (every non-comment, non-empty line has exactly 4 tab-separated fields).

**Step 4: Commit**

```bash
git add go/internal/charlie/hyphence/testdata/rfc_vectors.txt
git commit -m "test(hyphence): add testdata/rfc_vectors.txt for RFC conformance

Scaffolding for the RFC 0001 conformance test (added next commit). Six
starter test vectors covering parse-ok, parse-error-missing-separator,
and parse-error-invalid-boundary. Format is tab-separated:
name\\tinput-b64\\toutcome\\texpected-blob-b64.

Tracks #115.

:clown: by Clown — https://github.com/amarbel-llc/clown"
```

---

## Task 4: Add RFC conformance test (TDD)

**Promotion criteria:** N/A.

**Files:**
- Create: `go/internal/charlie/hyphence/rfc_conformance_test.go`

**Step 1: Write the failing test**

Create `go/internal/charlie/hyphence/rfc_conformance_test.go`:

```go
//go:build test

package hyphence

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"testing"
)

// Test vectors live in testdata/rfc_vectors.txt; format is documented
// in docs/rfcs/0001-hyphence.md. Every conforming implementation MUST
// agree with these outcomes.
func TestRFCConformance_HyphenceTestVectors(t *testing.T) {
	bites, err := os.ReadFile("testdata/rfc_vectors.txt")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}

	for lineNo, raw := range strings.Split(string(bites), "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) != 4 {
			t.Errorf("line %d: want 4 tab-separated fields, got %d", lineNo+1, len(parts))
			continue
		}

		name, inputB64, outcome, expectedB64 := parts[0], parts[1], parts[2], parts[3]

		input, err := base64.StdEncoding.DecodeString(inputB64)
		if err != nil {
			t.Errorf("%s: decode input b64: %v", name, err)
			continue
		}

		t.Run(name, func(t *testing.T) {
			runRFCVector(t, input, outcome, expectedB64)
		})
	}
}

func runRFCVector(t *testing.T, input []byte, outcome, expectedB64 string) {
	t.Helper()

	var blobCapture bytes.Buffer

	decoder := Decoder[*TypedBlob[struct{}]]{
		Metadata:      TypedMetadataCoder[struct{}]{},
		Blob:          testBlobDecoder{},
		BlobTeeWriter: &blobCapture,
	}

	typedBlob := &TypedBlob[struct{}]{}
	reader := bufio.NewReader(bytes.NewReader(input))

	_, err := decoder.DecodeFrom(typedBlob, reader)

	switch outcome {
	case "parse-ok":
		if err != nil {
			t.Fatalf("expected parse-ok, got error: %v", err)
		}

		var expected []byte
		if expectedB64 != "" && expectedB64 != "-" {
			expected, err = base64.StdEncoding.DecodeString(expectedB64)
			if err != nil {
				t.Fatalf("decode expected b64: %v", err)
			}
		}

		if !bytes.Equal(blobCapture.Bytes(), expected) {
			t.Errorf("blob mismatch: got %q, want %q", blobCapture.String(), string(expected))
		}

	case "parse-error-missing-separator":
		if err == nil {
			t.Fatal("expected parse-error-missing-separator, got nil")
		}
		if !errors.Is(err, errMissingNewlineAfterBoundary) {
			t.Errorf("expected errMissingNewlineAfterBoundary, got: %v", err)
		}

	case "parse-error-invalid-boundary":
		if err == nil {
			t.Fatal("expected parse-error-invalid-boundary, got nil")
		}
		if !errors.Is(err, errBoundaryInvalid) {
			t.Errorf("expected errBoundaryInvalid, got: %v", err)
		}

	default:
		t.Fatalf("unknown outcome %q", outcome)
	}
}
```

**Step 2: Run the test to verify it executes**

Run:

```bash
just test-go ./internal/charlie/hyphence/... -run TestRFCConformance -v
```

Expected: each subtest runs. Some MAY fail if the vector file's expected outcomes don't match the implementation — fix the vectors (testdata) rather than the implementation, since the implementation is the spec's reference.

If `parse-error-invalid-boundary` doesn't fire on the `boundary-no-newline` vector, the input may need adjusting: `---` without a newline returns EOF before the boundary check, which is a different code path. Adjust the vector to `---x\n` (boundary chars but a non-newline after) which exercises the actual `errBoundaryInvalid` path.

**Step 3: Once green, commit**

```bash
git add go/internal/charlie/hyphence/rfc_conformance_test.go
git commit -m "test(hyphence): add RFC 0001 conformance test driver

TestRFCConformance_HyphenceTestVectors reads testdata/rfc_vectors.txt
and asserts every vector matches the documented outcome. Errors are
identified via errors.Is against the package's typed sentinels
(errMissingNewlineAfterBoundary, errBoundaryInvalid). Adding new test
vectors to the file is sufficient to add new conformance cases — no
test-code changes needed.

Tracks #115.

:clown: by Clown — https://github.com/amarbel-llc/clown"
```

---

## Task 5: Draft `docs/rfcs/0001-hyphence.md`

**Promotion criteria:** N/A.

**Files:**
- Create: `docs/rfcs/0001-hyphence.md`

This is the project's first RFC. Use the **eng:rfc** skill to draft the body — it covers the MUST/SHOULD/MAY conventions, structure (Status, Abstract, Notational Conventions, Grammar, Decoder/Encoder behavior, Versioning, Test Vectors), and the numbering pattern (`docs/rfcs/NNNN-title-with-dashes.md`).

**Step 1: Invoke the eng:rfc skill**

Run the skill via Skill tool:

```
Skill: eng:rfc
Args: Draft docs/rfcs/0001-hyphence.md as madder's first RFC, formalizing the hyphence format. Source the substance from docs/man.7/hyphence.md (after the rebrand from Task 2) and the test vectors at go/internal/charlie/hyphence/testdata/rfc_vectors.txt. Status: proposed -> accepted on merge. Cross-reference the man page for descriptive prose; this RFC is the prescriptive document.
```

The skill will produce the doc. Save it at the right path.

**Step 2: Required RFC sections (the skill will cover these — verify the draft has them)**

1. **Status / metadata** — front matter with `status: proposed`, `date: 2026-05-02`.
2. **Abstract** — what hyphence is.
3. **Notational conventions** — RFC 2119 MUST/SHOULD/MAY.
4. **Document grammar** — formal grammar for boundary lines, metadata lines, body separator.
5. **Metadata line types** — normative spec for `#`, `-`, `@`, `!`, `<`, `%`.
6. **Decoder behavior** — strict mode normative; lenient mode an implementation accommodation.
7. **Encoder behavior** — canonical line order MUST be followed when emitting.
8. **Versioning** — type-string-driven; format itself unversioned.
9. **Examples** — same set as the man page.
10. **Test vectors** — references `go/internal/charlie/hyphence/testdata/rfc_vectors.txt`; describes the file format.

**Step 3: Verify the RFC is consistent with the man page**

Run:

```bash
grep -E 'MUST|SHOULD|MAY' docs/rfcs/0001-hyphence.md | head
```

Expected: at least 5 normative statements.

Run:

```bash
diff <(grep -E '^\*\*[#@!\\-<%]' docs/man.7/hyphence.md | sort) <(grep -E '^\*\*[#@!\\-<%]' docs/rfcs/0001-hyphence.md | sort)
```

Expected: any diff is intentional (RFC may add normative tightening; man page is descriptive).

**Step 4: Commit**

```bash
git add docs/rfcs/0001-hyphence.md
git commit -m "docs(rfc): add 0001-hyphence — formal spec with normative MUST/SHOULD/MAY

First RFC in the project; establishes docs/rfcs/NNNN-title.md as the
convention (zero-padded 4 digits, lowercase title-with-dashes, parallel
to ADRs in docs/decisions/).

The man page (docs/man.7/hyphence.md) stays the user-facing description;
this RFC is the prescriptive spec. Test vectors live at
go/internal/charlie/hyphence/testdata/rfc_vectors.txt and are
exercised by TestRFCConformance_HyphenceTestVectors.

Tracks #115.

:clown: by Clown — https://github.com/amarbel-llc/clown"
```

---

## Task 6: Run full pre-merge sanity, regenerate facades, merge

**Promotion criteria:** N/A.

**Files:**
- Run: `just vet-go && just test-go && just generate-facades`

**Step 1: Vet + test full suite**

Run:

```bash
just vet-go
just test-go
```

Expected: vet clean; all tests pass including the new `TestRFCConformance_HyphenceTestVectors` subtests and `TestDecoderBlobTeeWriterCapturesBlobContent`.

**Step 2: Regenerate facades**

Run:

```bash
just generate-facades
```

Expected: `pkgs/hyphence/main.go` regenerated. Likely a zero-line diff (no new exported symbols added in this PR), but harmless if dagnabit re-emits the file.

**Step 3: Stage any facade changes**

Run:

```bash
git status
```

If `pkgs/hyphence/main.go` shows as modified, stage it:

```bash
git add pkgs/hyphence/main.go
git commit --amend --no-edit
```

**Step 4: Merge via spinclass**

Use the spinclass MCP merge tool with `git_sync=true`. The pre-merge hook runs the full Go-race + bats + bats-net-cap suites. All should pass.

If the tool times out (known issue for ~minute-long pre-merge hooks), retry once. If it succeeds the second time, that's normal.

**Step 5: Verify on master**

Run:

```bash
git rev-parse master
git rev-parse origin/master
git log --oneline master -3
```

Expected: HEAD of master == HEAD of origin/master; the commit log shows the man-page rebrand, testdata, conformance test, and RFC commits in that order.

---

## Task 7: Close the loop on the issue

**Promotion criteria:** N/A.

**Files:**
- GitHub: comment on amarbel-llc/madder#115; close it.

**Step 1: Comment on #115**

Use `mcp__plugin_moxy_moxy__get-hubbed_issue-comment` (or `gh issue comment`):

> Closing as completed. Resolution shape diverged from the original ask:
>
> The RFC enforcements the issue described (BlobTeeWriter, AllowMissingSeparator, peekSeparatorLine, ReadBoundaryFromRingBuffer, PeekBoundaryFromPeeker, separator-discipline tests) were already at parity in madder when this issue was filed — the fork-and-drift had already converged. So the implementation work was the *spec* half: dropping dodder branding from the man page, promoting the body-separator warning to a normative MUST, and adding `docs/rfcs/0001-hyphence.md` as the project's first RFC, with a test-vector conformance suite that locks the spec to the implementation.
>
> Design doc: `docs/plans/2026-05-02-hyphence-ownership-design.md`. Implementation plan: `docs/plans/2026-05-02-hyphence-ownership.md`.
>
> Dodder coordination: filing a follow-up issue on amarbel-llc/dodder so they can drop their `internal/charlie/hyphence` fork and switch their 25 import sites to `pkgs/hyphence`. They'll also need `AllowMissingSeparator: true` for legacy on-disk reads predating dodder #41.

**Step 2: Close #115**

Use `mcp__plugin_moxy_moxy__get-hubbed_issue-close` with `reason: completed`.

**Step 3: File dodder coordination issue**

Use `mcp__plugin_moxy_moxy__get-hubbed-external_issue-create` against `amarbel-llc/dodder`:

> **Title:** Drop `internal/charlie/hyphence` fork; consume `madder/pkgs/hyphence`
>
> **Body:**
> Now that madder has shipped #115 (hyphence ownership convergence — RFC + spec docs in madder, code already at parity), dodder can drop its `internal/charlie/hyphence/` package entirely.
>
> ## Migration steps
>
> 1. Remove `dodder/internal/charlie/hyphence/`.
> 2. Replace 25 import sites: `dodder/internal/charlie/hyphence` → `github.com/amarbel-llc/madder/go/pkgs/hyphence`.
> 3. Audit dodder reads for legacy on-disk objects without the body separator (the data that motivated dodder #41). At those call sites, set `AllowMissingSeparator: true` on the relevant `Decoder`. Spot-check candidates: any path that reads pre-fix repo configs, blob store configs, workspace configs.
> 4. Run dodder's full test suite. Strict default should not break anything other than the explicitly-flagged legacy-read paths.
>
> ## Why now
>
> - madder/pkgs/hyphence was published in [madder #105](https://github.com/amarbel-llc/madder/issues/105).
> - The RFC and test vectors locking the format spec landed in [madder #115](https://github.com/amarbel-llc/madder/issues/115).
> - The implementation is already at parity — same struct fields, same methods, same line-by-line tests for separator discipline.

---

## Verification checkpoints

After **Task 2**: `git diff` on `docs/man.7/hyphence.md` shows only the rebranding lines; man page renders without mandoc errors.

After **Task 4**: `just test-go ./internal/charlie/hyphence/... -run TestRFCConformance -v` passes for every vector.

After **Task 5**: `docs/rfcs/0001-hyphence.md` exists, has front matter (`status: proposed`, `date: 2026-05-02`), at least 5 MUST/SHOULD/MAY statements, references the testdata file.

After **Task 6**: `just test-go` clean across the whole module; spinclass merge succeeds (5/5 TAP green).

After **Task 7**: madder #115 closed with `reason: completed`; new issue exists on amarbel-llc/dodder linking back.

---

## Notes for the implementer

- The full design context is at `docs/plans/2026-05-02-hyphence-ownership-design.md`. Read that first if anything in this plan is unclear.
- The eng:rfc skill is the right tool for Task 5; this plan deliberately doesn't try to inline an RFC body.
- Don't bother trying to use the eng:subagent-driven-development skill for this plan — the work is small (4 commits, ~half a day's work) and per-task subagents would be overhead. Linear execution by a single agent is fine.
- The man page rendering (`just debug-gen_man`) is just a sanity check; it doesn't have to be in the commit.
- The test-vector file format is designed so that **adding a new vector is one new line in the testdata file** — no test-code edit needed. Encourage future contributors to add vectors when they fix RFC-related bugs.
