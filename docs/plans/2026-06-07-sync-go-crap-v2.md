# sync go-crap v2 (ndjson-crap) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use eng:subagent-driven-development to implement this plan task-by-task.

**Goal:** madder `sync` emits ndjson-crap by default when piped (tracer bullet for consuming go-crap v2 via the RFC 0001 flake lane).

**Architecture:** New `crap` value on the shared `-format` flag; sync resolves `auto` → crap when piped via a new `ResolvePipedDefault` helper; a third `syncSink` impl (`syncCrapSink`) wraps `ndjsoncrap.Writer`. Legacy tap/ndjson/json lanes untouched. Design: `docs/plans/2026-06-07-sync-go-crap-v2-design.md`.

**Tech Stack:** Go 1.26, `github.com/amarbel-llc/crap/go-crap/ndjsoncrap`, flake-input-go_mod (RFC 0001) consumer wiring, bats.

**Rollback:** One line — sync reverts from `ResolvePipedDefault(os.Stdout, FormatCRAP)` to `Resolve(os.Stdout)`. `-format ndjson` is the immediate consumer-side escape hatch.

**Final gate:** the user's personal sync check against the built binary. Do NOT call merge-this-session before that gate passes.

---

## Task 1: Verify the go-crap version theory and wire the dependency

**Promotion criteria:** N/A (purely additive dep).

**Files:**
- Modify: `flake.nix` (inputs block near line 54; gomod import near line 110)
- Modify: `go/gomod.nix` (function head line 24, `goFlakeInputs` map line 37)
- Modify: `go/go.mod`, `go/go.sum`, `go/gomod2nix.toml` (via tools, not by hand)

**Step 1: Verify the version theory.** The design predicts the `go-crap/v2.0` tag is unusable by Go tooling (invalid semver; module path lacks `/v2`). Run, from `go/`:

```
mcp hamster.go-get module=github.com/amarbel-llc/crap/go-crap@5f5a10b0db4d cwd=go
```

Expected: resolves to a pseudo-version like `v0.0.0-20260608001025-5f5a10b0db4d`. Also confirm the tag form fails: `hamster.go-get module=github.com/amarbel-llc/crap/go-crap@v2.0.0` → expect a "no matching versions" / invalid version error. Record both outcomes. If the pseudo-version ALSO fails, STOP and report to the user — do not improvise.

**Step 2: File the upstream crap issue** (only if Step 1 confirmed the theory) via `/eng:file-issue`: repo `amarbel-llc/crap`, title "go-crap/v2.0 tag is not consumable by Go tooling (needs go-crap/v2.0.0 + /v2 module path)". Include the exact error from the `@v2.0.0` attempt and the working pseudo-version. Add a TaskCreate entry for the followup (per CLAUDE.md mid-sequence issue rule).

**Step 3: Add the flake input.** In `flake.nix`, after the `tap` stanza (line 54), mirroring its comment style:

```nix
    # Sourced via goFlakeInputs (see madder#208) so a crap bump only
    # touches flake.lock — no go.mod / gomod2nix.toml lockstep edits.
    crap = {
      url = "github:amarbel-llc/crap";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
      inputs.bats.follows = "bats";
    };
```

Check crap's actual `flake.nix` inputs first (igloo, nixpkgs-master, utils, bats observed 2026-06-07) and only add `follows` for inputs that exist. Add `crap` to the flake's function args and to the `gomod = import ./go/gomod.nix { inherit … tap tommy crap … }` call (flake.nix:110-117).

**Step 4: Add the consumer mapping.** In `go/gomod.nix`: add `crap,` to the function head (line 24 area) and to `goFlakeInputs` (crap's `go-pkgs` is built from the `./go-crap` subtree — no `subPath`):

```nix
    "github.com/amarbel-llc/crap/go-crap" = {
      src = crap.packages.${system}.go-pkgs;
    };
```

**Step 5: Lock + regenerate.** Run `chix.flake-lock` (no args — locks the new input), then `just gomod2nix`, then `just tidy`. Expected: `flake.lock` gains a `crap` node; go.mod gains the pseudo-version require (it may land in the indirect block until Task 4 imports it — fine).

**Step 6: Compile check.** `hamster.go-build` (defaults, `./...`). Expected: PASS (no imports yet, this validates go.sum/gomod consistency).

**Step 7: Commit.** `grit.add` paths `["flake.nix", "flake.lock", "go/gomod.nix", "go/go.mod", "go/go.sum", "go/gomod2nix.toml"]`; commit:

```
build(deps): add crap flake input via goFlakeInputs (go-crap v2 pseudo-version)
```

(Standard Clown sign-off trailer on every commit in this plan.)

---

## Task 2: output_format — FormatCRAP + ResolvePipedDefault

**Promotion criteria:** N/A (additive).

**Files:**
- Modify: `go/internal/charlie/output_format/output_format.go`
- Test: `go/internal/charlie/output_format/output_format_test.go` (create)
- Regenerate: `go/pkgs/output_format/` via `just generate-facades`

**Step 1: Write the failing test** (`output_format_test.go`, package `output_format`). Note `Resolve`/`ResolvePipedDefault` take `*os.File` and TTY detection can't be faked with a plain file — split the logic: a private `resolveFor(isTTY bool, piped Format)` carries the decision table, `ResolvePipedDefault` does the isatty probe and delegates. Test the decision table:

```go
package output_format

import "testing"

func TestResolveFor(t *testing.T) {
	cases := []struct {
		name     string
		format   Format
		isTTY    bool
		piped    Format
		expected Format
	}{
		{"auto tty", FormatAuto, true, FormatCRAP, FormatTAP},
		{"auto piped crap default", FormatAuto, false, FormatCRAP, FormatCRAP},
		{"auto piped ndjson default", FormatAuto, false, FormatNDJSON, FormatNDJSON},
		{"explicit ndjson wins on tty", FormatNDJSON, true, FormatCRAP, FormatNDJSON},
		{"explicit crap wins piped", FormatCRAP, false, FormatNDJSON, FormatCRAP},
		{"explicit tap wins piped", FormatTAP, false, FormatCRAP, FormatTAP},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if actual := c.format.resolveFor(c.isTTY, c.piped); actual != c.expected {
				t.Errorf("expected %q, got %q", c.expected, actual)
			}
		})
	}
}

func TestSetAcceptsCrap(t *testing.T) {
	var f Format
	if err := f.Set("crap"); err != nil {
		t.Fatalf("Set(crap): %v", err)
	}
	if f != FormatCRAP {
		t.Errorf("expected %q, got %q", FormatCRAP, f)
	}
}
```

**Step 2: Run to verify failure.** `just test-go ./internal/charlie/output_format/...` — expected: FAIL (`FormatCRAP`, `resolveFor` undefined). (Always `just test-go`, never bare `go test` — the lane carries the required `test` build tag.)

**Step 3: Implement.** In `output_format.go`: add `FormatCRAP = Format("crap")` to the const block; add it to `Set`'s switch, `GetCLICompletion` (`"ndjson-crap: one JSON record per line (see crap-present(1))"`), and `FlagDescription` (`"output format: auto (default), tap, json, ndjson, or crap"`); restructure:

```go
// ResolvePipedDefault collapses auto into tap on a TTY and into piped
// otherwise. Non-auto values are returned unchanged. Commands whose piped
// default has migrated to ndjson-crap pass FormatCRAP; Resolve keeps the
// legacy ndjson piped default for everyone else.
func (f Format) ResolvePipedDefault(stdout *os.File, piped Format) Format {
	fd := stdout.Fd()
	isTTY := isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
	return f.resolveFor(isTTY, piped)
}

func (f Format) resolveFor(isTTY bool, piped Format) Format {
	if f != FormatAuto {
		return f
	}
	if isTTY {
		return FormatTAP
	}
	return piped
}
```

and `Resolve` becomes `return f.ResolvePipedDefault(stdout, FormatNDJSON)`. Update the package doc comment (auto default paragraph) to mention the per-command piped default.

**Step 4: Run tests.** `just test-go ./internal/charlie/output_format/...` — expected: PASS.

**Step 5: Facade regen.** `just generate-facades`, then `just lint-facades` — expected: clean. Check `go/pkgs/output_format/` diff is additive only (cutting-garden consumes this facade).

**Step 6: Commit.** All of the above + regenerated facade: `feat(output_format): add crap format value and per-command piped default`

---

## Task 3: syncSink grows summary(); counts move out of notice()

**Promotion criteria:** N/A (refactor, no behavior change).

**Files:**
- Modify: `go/internal/india/commands/sync.go` (interface ~line 122, impls, deferred block ~line 316)

**Step 1: Refactor.** Add to `syncSink`:

```go
	// summary reports final counts. TAP renders a comment; JSON prints a
	// human line to stderr; crap emits the Summary record.
	summary(succeeded, failed, ignored, total int)
```

Impls — `syncTapSink`:

```go
func (s *syncTapSink) summary(succeeded, failed, ignored, total int) {
	s.tw.Comment("Successes: %d, Failures: %d, Ignored: %d, Total: %d",
		succeeded, failed, ignored, total)
}
```

(`tap.Writer.Comment` is printf-style per the existing `notice` impl.) `syncJsonSink`:

```go
func (s *syncJsonSink) summary(succeeded, failed, ignored, total int) {
	fmt.Fprintf(s.errOut, "Successes: %d, Failures: %d, Ignored: %d, Total: %d\n",
		succeeded, failed, ignored, total)
}
```

In `runStore`'s deferred func (line 316-330), replace the `sink.notice(fmt.Sprintf("Successes: …"))` call with `sink.summary(blobImporter.Counts.Succeeded, blobImporter.Counts.Failed, blobImporter.Counts.Ignored, blobImporter.Counts.Total)`. `notice` stays for "limit hit, stopping".

**Step 2: Verify no behavior change.** `just test-go ./internal/india/...` then `just test-bats-targets sync.bats` — expected: PASS (output byte-identical).

**Step 3: Commit.** `refactor(sync): route final counts through a summary sink method`

---

## Task 4: syncCrapSink + piped default flip

**Promotion criteria:** legacy ndjson record shape retired only per the design's promotion criteria (other commands adopted + one clean release) — NOT in this change.

**Files:**
- Modify: `go/internal/india/commands/sync.go`
- Test: `go/internal/india/commands/sync_crap_sink_test.go` (create)

**Step 1: Write the failing test.** Round-trip through `ndjsoncrap.NewReader` (the package's own idiom — tolerant reader, `Next()` until `io.EOF`):

```go
package commands

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/amarbel-llc/crap/go-crap/ndjsoncrap"
)

// fakeId satisfies the minimal MarklId surface the sink formats. Check
// domain_interfaces.MarklId's method set; if a test fake already exists in
// this package or a markl test helper is importable under `-tags test`,
// use that instead of redefining one.

func TestSyncCrapSinkStream(t *testing.T) {
	var buf bytes.Buffer
	sink := newSyncCrapSink(&buf, io.Discard)

	sink.transferred(testMarklId(t, "ok-blob"), 42)
	sink.failed(testMarklId(t, "bad-blob"), 0, errors.New("boom"))
	sink.listError(errors.New("list failed"))
	sink.summary(1, 2, 0, 3)
	sink.finalize()

	var records []ndjsoncrap.Record
	r := ndjsoncrap.NewReader(&buf)
	for {
		rec, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		records = append(records, rec)
	}

	// Meta header, 3 tests, summary.
	if len(records) != 5 {
		t.Fatalf("expected 5 records, got %d: %#v", len(records), records)
	}
	if _, ok := records[0].(ndjsoncrap.Meta); !ok {
		t.Errorf("record 0: expected Meta, got %T", records[0])
	}
	first, ok := records[1].(ndjsoncrap.Test)
	if !ok || !first.OK || first.N != 1 {
		t.Errorf("record 1: expected passing Test n=1, got %#v", records[1])
	}
	second, ok := records[2].(ndjsoncrap.Test)
	if !ok || second.OK || second.Diagnostic["error"] != "boom" {
		t.Errorf("record 2: expected failing Test with error diag, got %#v", records[2])
	}
	last, ok := records[4].(ndjsoncrap.Summary)
	if !ok || last.Passed != 1 || last.Failed != 2 || last.Total != 3 {
		t.Errorf("record 4: expected Summary{1,2,_,3}, got %#v", records[4])
	}
}
```

`testMarklId` — construct a real MarklId the way neighboring tests in `internal/india` or `internal/bravo/markl` do (search for existing test constructors first; do not invent a parallel helper — per repo feedback, reuse or file an upstream-helper issue).

**Step 2: Run to verify failure.** `just test-go ./internal/india/commands/...` — expected: FAIL (`newSyncCrapSink` undefined).

**Step 3: Implement** in `sync.go`:

```go
// syncCrapSink emits ndjson-crap result-family records (go-crap v2).
// Routing parity note: runStore never calls exists() — already-present
// blobs surface as transferred with size 0 (see the IsErrBlobAlreadyExists
// branch). The exists() impl below is for interface completeness; the
// skip-directive mapping activates if/when runStore routes it.
type syncCrapSink struct {
	buf    *bufio.Writer
	w      *ndjsoncrap.Writer
	errOut io.Writer
	n      int
}

func newSyncCrapSink(out io.Writer, errOut io.Writer) *syncCrapSink {
	buf := bufio.NewWriter(out)
	sink := &syncCrapSink{buf: buf, w: ndjsoncrap.NewWriter(buf), errOut: errOut}
	_ = sink.w.Write(ndjsoncrap.Meta{Title: "sync", Source: "madder"})
	return sink
}

func (s *syncCrapSink) test(t ndjsoncrap.Test) {
	s.n++
	t.N = s.n
	_ = s.w.Write(t)
}

func (s *syncCrapSink) transferred(id domain_interfaces.MarklId, bytesWritten int64) {
	s.test(ndjsoncrap.Test{
		Description: formatSyncTestPoint(id, bytesWritten),
		OK:          true,
		Diagnostic:  map[string]any{"state": syncStateTransferred, "size": bytesWritten},
	})
}

func (s *syncCrapSink) exists(id domain_interfaces.MarklId) {
	s.test(ndjsoncrap.Test{
		Description: formatSyncTestPoint(id, 0),
		OK:          true,
		Directive:   &ndjsoncrap.Directive{Kind: "skip", Reason: syncStateExists},
		Diagnostic:  map[string]any{"state": syncStateExists},
	})
}

func (s *syncCrapSink) failed(id domain_interfaces.MarklId, bytesWritten int64, err error) {
	diag := map[string]any{"state": syncStateFailed, "error": err.Error()}
	if bytesWritten > 0 {
		diag["size"] = bytesWritten
	}
	s.test(ndjsoncrap.Test{
		Description: formatSyncTestPoint(id, bytesWritten),
		Diagnostic:  diag,
	})
}

func (s *syncCrapSink) listError(err error) {
	s.test(ndjsoncrap.Test{
		Description: "(unknown blob)",
		Diagnostic:  map[string]any{"state": syncStateListError, "error": err.Error()},
	})
}

func (s *syncCrapSink) notice(msg string) {
	fmt.Fprintln(s.errOut, msg)
}

func (s *syncCrapSink) bailOut(msg string) {
	_ = s.w.Write(ndjsoncrap.Bailout{Message: msg})
}

func (s *syncCrapSink) summary(succeeded, failed, ignored, total int) {
	_ = s.w.Write(ndjsoncrap.Summary{
		Passed:    succeeded,
		Failed:    failed,
		Skipped:   ignored,
		Total:     total,
		PlanCount: total,
		Valid:     true,
	})
}

func (s *syncCrapSink) finalize() {
	_ = s.buf.Flush()
}
```

Before settling `Summary.PlanCount`/`Valid` semantics, read `docs/ndjson-crap-schema.md` in the crap repo (get-hubbed content-get) and match it; adjust the unit test if the schema says otherwise. Wire the switch in `runStore` (line 239-250):

```go
	switch cmd.Format.ResolvePipedDefault(os.Stdout, output_format.FormatCRAP) {
	case output_format.FormatCRAP:
		sink = newSyncCrapSink(os.Stdout, os.Stderr)
	case output_format.FormatJSON, output_format.FormatNDJSON:
		// … existing json branch unchanged …
	default:
		sink = &syncTapSink{tw: tap.NewWriter(os.Stdout)}
	}
```

**Step 4: Run tests.** `just test-go ./internal/india/commands/...` — expected: PASS. Then `hamster.go-build` for the full tree.

**Step 5: File the loose-end issue** via `/eng:file-issue` (madder repo): "syncSink.exists() is unreachable — runStore folds IsErrBlobAlreadyExists into transferred". Reference the routing-parity comment. TaskCreate entry.

**Step 6: Commit.** `feat(sync): emit ndjson-crap by default when piped (-format crap)` — body references the design doc and notes `-format ndjson` as the legacy opt-out. Include `git add` of the new test file (nix dirty-tree rule: untracked files are invisible to nix builds).

---

## Task 5: bats coverage

**Files:**
- Modify: `zz-tests_bats/sync.bats` (the `sync_json_auto_detects` test, lines 51-68)

**Step 1: Update the piped-default test** (rename to match new reality) and add the opt-out test:

```bash
function sync_crap_auto_detects { # @test

  # Default auto-format under `run` (no TTY) must emit ndjson-crap.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "sync-crap-test" >"$blob"
  run_madder write -format tap "$blob"
  assert_success

  run_madder init -hash_type-id sha256 -encryption none .sha256
  assert_success

  run_madder sync .default .sha256
  assert_success
  assert_output --partial '"type":"crap"'
  assert_output --partial '"type":"summary"'
  refute_output --partial 'TAP version 14'
}

function sync_ndjson_opt_out { # @test

  # -format ndjson keeps the legacy {id,state,size,error} records.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "sync-ndjson-test" >"$blob"
  run_madder write -format tap "$blob"
  assert_success

  run_madder init -hash_type-id sha256 -encryption none .sha256
  assert_success

  run_madder sync -format ndjson .default .sha256
  assert_success
  assert_output --partial '"state":"transferred"'
  refute_output --partial '"type":"crap"'
}
```

**Step 2: Run.** `just test-bats-targets sync.bats` — expected: PASS.

**Step 3: Commit.** `test(sync): pin crap piped default and ndjson opt-out in bats`

---

## Task 6: docs

**Files:**
- Modify: `go/internal/india/commands/sync.go` (`GetDescription().Long`, line 56-75)
- Modify: whatever `rg -i 'ndjson' docs/man.1 docs/man.7` surfaces about sync's output format (verify before editing; man sources may be generated — check `just debug-gen_man madder.1` flow first)

**Step 1:** Rewrite the Long description's output paragraph: defaults to TAP on a TTY and **ndjson-crap** when piped (`crap-present`-compatible; see crap's spec); `-format ndjson`/`json` keeps the legacy per-record JSON (`id`, `state`, `size`, `error`); `-format` forces any encoding.

**Step 2:** Update man-page sync references found by the rg sweep. If man content is generated from `GetDescription`, Step 1 already covers it — verify with `just debug-gen_man madder.1` and grep for "ndjson-crap".

**Step 3: Commit.** `docs(sync): document ndjson-crap piped default and legacy opt-out`

---

## Task 7: follow-up issue + gate

**Step 1:** File the adoption follow-up via `/eng:file-issue` (madder repo): "Adopt -format crap piped default in fsck, write, pack-blobs, list (fold into shared Resolve)" — reference the design doc's promotion criteria and this tracer. TaskCreate entry.

**Step 2: Build the real binary.** `just build` (nix). Remember: new files must be `git add`ed (done at each commit) or the nix build won't see them.

**Step 3: THE GATE.** Hand off to the user for their personal sync check against `result/bin/madder`. Suggest the loop: `result/bin/madder sync … | crap-present` if they have crap's binary, plus a bare TTY run to confirm TAP is unchanged. **Wait for their verdict.**

**Step 4:** Only after the gate passes: pre-merge attestation (`nothing-but-the-truth`) + `merge-this-session` (its hook runs the full `just` lane — do not run `just` redundantly first).
