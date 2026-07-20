# `madder sftp-analyze-and-suggest-configs` Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use eng:subagent-driven-development to implement this plan task-by-task.

**Goal:** Ship `madder sftp-analyze-and-suggest-configs`, a read-only
command that probes a legacy SFTP blob store, generates candidate
`blob_store-config` files matching the on-disk encoding, sample-verifies
each candidate, and offers an interactive deep-verify + bootstrap flow.

**Architecture:** Three new pieces of code: a pure-function probe library
(`sftp_probe/`) tested via synthesize-forward / verify-backward unit
tests; a thin CLI wrapper (`india/commands/sftp_analyze_and_suggest_configs.go`)
that owns flags, SFTP walk, and `huh` prompts; and a small
`madder-test-craft-legacy-blob` `_test`-tagged binary used only by bats
fixtures. End-to-end coverage in a new bats file under `zz-tests_bats/`.

**Tech Stack:** Go 1.22+, `github.com/pkg/sftp`, `golang.org/x/crypto/ssh`,
`charmbracelet/huh` (already wrapped at `internal/futility/huh`),
`age` (already wired through `markl` family `agex25519`), bats, the
existing test SFTP server `madder-test-sftp-server`.

**Rollback:** Single-commit revert. Greenfield package + greenfield
command + greenfield bats file + greenfield man page. The only
behavior touching existing code is calling
`blob_stores.WriteRemoteConfig` (already exists) — no edits to
existing files are required for the *core* feature, only to register
the new command in `india/commands/init.go`-style registries and to
plumb the test binary into nix. Both are tiny additive edits.

**Source of truth:** `docs/plans/2026-05-08-sftp-analyze-and-suggest-configs-design.md`
(commit `80ed2aa`). This plan implements that design verbatim. If
this plan and the design conflict, the design wins; flag the conflict
with the user before resolving.

**Conventions inherited from the repo:**
- Go tests use `-tags test`. Run via `just test-go` or
  `just test-go ./path/...`. Do NOT run bare `go test ./...` (will fail
  with `undefined: ui.T`).
- Build via `just build-go` (Go-only, fast) or `just build` (full nix
  build, needed before bats).
- Vet via `just vet-go`. Run after every implementation step.
- Bats tests run via `just test-bats-targets <file>.bats` (rebuilds via
  nix, then runs). Use `just test-bats-tags net_cap` for the
  network-capable lane.
- Commit signatures: `:clown: signed off by Clown
  <https://github.com/amarbel-llc/clown>` per the project's CLAUDE.md.

**Key references for the implementing engineer:**
- Existing analog: `init.go`'s `runDiscover` (`go/internal/india/commands/init.go:243+`)
  — calls `blob_stores.DiscoverRemoteConfig` then
  `WriteRemoteConfig`. Our wrapper does the first call, samples,
  verifies, then optionally calls the second.
- SFTP store machinery: `go/internal/foxtrot/blob_stores/store_remote_sftp.go`.
  Construction pattern at line 89 (`makeSftpStore`), readRemoteConfig
  at 172, allBlobs walker at 391-500 (mirror this for sampling).
- `blob_io.MakeConfig` signature at
  `go/internal/foxtrot/blob_io/main.go:14`. Takes a hash format, a
  path-join func, an `IOWrapper` for compression, and a `MarklId` for
  encryption (nil → no encryption).
- `LegacyCompressionRef("zstd") -> "madder-codec-zstd-v1@zstd"` at
  `go/internal/bravo/plugins/legacy.go:22`. Resolves a legacy
  compression-type string to a plugin ref. The plugin then exposes an
  `IOWrapper` — see existing call sites in
  `internal/foxtrot/blob_stores/store_inventory_archive.go` and
  similar.
- `huh` prompts wrapper: `go/internal/futility/huh/prompter.go`.
  `Prompter{}.Confirm(msg) (bool, error)` is the only call we need.
- Existing test binary pattern: `go/cmd/madder-test-sftp-server/main.go`
  — used only by bats; not shipped. Mirror its shape.
- Bats SFTP fixture: `zz-tests_bats/lib/sftp.bash` —
  `start_sftp_server`, `init_sftp_test_store`, `stop_sftp_server`.

---

## Phase A — Pure probe library (TDD-first)

The library has no SFTP dependency, no UI dependency, no I/O dependency
beyond `io.Reader`. It must compile and pass `just vet-go` after every
task in this phase.

### Task A1: Package skeleton + `Stage` enum + `Candidate` type

**Files:**
- Create: `go/internal/foxtrot/sftp_probe/main.go`
- Create: `go/internal/foxtrot/sftp_probe/stage.go`
- Create: `go/internal/foxtrot/sftp_probe/candidate.go`
- Create: `go/internal/foxtrot/sftp_probe/CLAUDE.md`

**Step 1: Write the failing test**

Create `go/internal/foxtrot/sftp_probe/stage_test.go`:

```go
package sftp_probe

import "testing"

func TestStageString(t *testing.T) {
	cases := []struct {
		stage Stage
		want  string
	}{
		{StageOK, "ok"},
		{StageDecrypt, "decrypt"},
		{StageDecompress, "decompress"},
		{StageHashMismatch, "hash_mismatch"},
	}

	for _, tc := range cases {
		if got := tc.stage.String(); got != tc.want {
			t.Errorf("Stage(%d).String() = %q, want %q", tc.stage, got, tc.want)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `just test-go ./internal/foxtrot/sftp_probe/...`
Expected: FAIL — package doesn't exist.

**Step 3: Write minimal implementation**

`go/internal/foxtrot/sftp_probe/main.go`:

```go
// Package sftp_probe contains pure-function probes that classify
// SFTP blob bytes against candidate blob_store_configs.Config
// values without any SFTP, UI, or filesystem dependency.
//
// See docs/plans/2026-05-08-sftp-analyze-and-suggest-configs-design.md
// for the design. The package's API surface is documented in stage.go,
// candidate.go, verify.go, and aggregate.go.
package sftp_probe
```

`go/internal/foxtrot/sftp_probe/stage.go`:

```go
package sftp_probe

type Stage int

const (
	StageOK Stage = iota
	StageDecrypt
	StageDecompress
	StageHashMismatch
)

func (s Stage) String() string {
	switch s {
	case StageOK:
		return "ok"
	case StageDecrypt:
		return "decrypt"
	case StageDecompress:
		return "decompress"
	case StageHashMismatch:
		return "hash_mismatch"
	default:
		return "unknown"
	}
}
```

`go/internal/foxtrot/sftp_probe/candidate.go`:

```go
package sftp_probe

import (
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_io"
)

// Candidate is one (compression, encryption) hypothesis about a blob
// store's encoding. StoreConfig is the on-disk form; IOConfig is the
// reader-pipeline form for verification. Label is for display.
type Candidate struct {
	StoreConfig blob_store_configs.Config
	IOConfig    blob_io.Config
	Label       string
}
```

`go/internal/foxtrot/sftp_probe/CLAUDE.md`:

```markdown
# sftp_probe

Pure-function probe library for sftp-analyze-and-suggest-configs.
No SFTP, no UI, no filesystem. Takes bytes / typed configs in,
returns verdicts.

## Key types

- `Stage` (enum: OK / Decrypt / Decompress / HashMismatch) — where
  in the read pipeline a verification attempt failed (or didn't).
- `Candidate` — one (compression, encryption) hypothesis. Holds
  both the `blob_store_configs.Config` (for emission to disk) and
  the `blob_io.Config` (for verification).
- `SampleResult` / `Aggregate` — per-attempt and per-candidate
  rollups.

## Key functions

- `EnumerateCandidates(layout, keys) []Candidate` — combinatorial
  cross-product of {none, gzip, zlib, zstd} × {none, age+keyᵢ}.
- `VerifySample(reader, expectedHex, candidate) SampleResult` —
  attempts decode through the candidate's pipeline, hashes the
  result, compares to expectedHex.
- `Rank(aggregates) []Aggregate` — sort by Verified desc, ties
  broken by stage diversity.

## Design

See `docs/plans/2026-05-08-sftp-analyze-and-suggest-configs-design.md`
section "Detection model — verify, don't sniff".
```

**Step 4: Run test to verify it passes**

Run: `just test-go ./internal/foxtrot/sftp_probe/...`
Expected: PASS.

Run: `just vet-go`. Expected: clean.

**Step 5: Commit**

```bash
git add go/internal/foxtrot/sftp_probe/
git commit -m "feat(sftp_probe): scaffold package with Stage and Candidate types"
```

---

### Task A2: `VerifySample` — case 1 (none/none success)

**Files:**
- Create: `go/internal/foxtrot/sftp_probe/verify.go`
- Create: `go/internal/foxtrot/sftp_probe/verify_test.go`

**Step 1: Write the failing test**

In `verify_test.go`:

```go
package sftp_probe

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_io"
)

// candidateNoneNoneSha256 is the simplest candidate: no compression,
// no encryption, sha256 hash. The test in TestVerifySample_NoneNone_OK
// hands it cleartext bytes and asserts the verifier accepts them
// when the path-digest matches.
func candidateNoneNoneSha256(t *testing.T) Candidate {
	t.Helper()
	// blob_io.DefaultConfig is the identity bundle (no compression,
	// no encryption, default sha256). Reuse it directly to avoid
	// duplicating MakeConfig wiring in tests.
	return Candidate{
		IOConfig: blob_io.DefaultConfig,
		Label:    "none/none",
		// StoreConfig is unused in pure verify; leave nil.
	}
}

func TestVerifySample_NoneNone_OK(t *testing.T) {
	cleartext := []byte("hello probe")
	digest := sha256.Sum256(cleartext)
	expectedHex := hex.EncodeToString(digest[:])

	cand := candidateNoneNoneSha256(t)

	got := VerifySample(bytes.NewReader(cleartext), expectedHex, cand)

	if !got.Ok {
		t.Fatalf("VerifySample returned Ok=false; want true. Stage=%s Err=%v",
			got.Stage, got.Err)
	}
	if got.Stage != StageOK {
		t.Errorf("Stage = %s, want %s", got.Stage, StageOK)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `just test-go ./internal/foxtrot/sftp_probe/...`
Expected: FAIL — `VerifySample` undefined.

**Step 3: Write minimal implementation**

`go/internal/foxtrot/sftp_probe/verify.go`:

```go
package sftp_probe

import (
	"bytes"
	"encoding/hex"
	"io"

	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_io"
	"code.linenisgreat.com/purse-first/libs/dewey/bravo/errors"
)

type SampleResult struct {
	Ok    bool
	Stage Stage
	Err   error
}

// VerifySample feeds blobReader through candidate.IOConfig's
// reader pipeline (decrypt → decompress → digest), compares the
// produced hex digest to expectedDigestHex, and reports the
// verdict.
//
// The function is pure: no SFTP, no filesystem, no global state.
// The caller is responsible for buffering the reader if it needs
// to be replayed against another candidate.
func VerifySample(
	blobReader io.Reader,
	expectedDigestHex string,
	candidate Candidate,
) (result SampleResult) {
	defer func() {
		if r := recover(); r != nil {
			result = SampleResult{
				Ok:    false,
				Stage: StageDecrypt, // default; refined per phase below
				Err:   errors.Errorf("panic during VerifySample: %v", r),
			}
		}
	}()

	// blob_io.NewReader requires an io.ReadSeeker. Buffer the
	// reader once so the pipeline can seek if it needs to.
	buf, err := io.ReadAll(blobReader)
	if err != nil {
		return SampleResult{Stage: StageDecrypt, Err: errors.Wrap(err)}
	}

	reader, err := blob_io.NewReader(candidate.IOConfig, bytes.NewReader(buf))
	if err != nil {
		// Decode-time failures from NewReader come from the
		// encryption WrapReader stage. Classify accordingly.
		return SampleResult{Stage: StageDecrypt, Err: errors.Wrap(err)}
	}
	defer reader.Close()

	if _, err := io.Copy(io.Discard, reader); err != nil {
		return SampleResult{Stage: StageDecompress, Err: errors.Wrap(err)}
	}

	gotHex := hex.EncodeToString(reader.GetDigest().GetBytes())
	if gotHex != expectedDigestHex {
		return SampleResult{Stage: StageHashMismatch}
	}

	return SampleResult{Ok: true, Stage: StageOK}
}
```

NOTE: the `reader.GetDigest()` call assumes `blob_io.Reader` exposes
the digest after reading. Verify this in the `blob_io` package
before writing; if the method is named differently, mirror what
the existing SFTP read path does at
`store_remote_sftp.go:560-577`. Adjust import / call site
accordingly. Do NOT plumb a separate hasher; the pipeline already
hashes.

**Step 4: Run test to verify it passes**

Run: `just test-go ./internal/foxtrot/sftp_probe/...`
Expected: PASS.

Run: `just vet-go`. Expected: clean.

**Step 5: Commit**

```bash
git add go/internal/foxtrot/sftp_probe/verify.go go/internal/foxtrot/sftp_probe/verify_test.go
git commit -m "feat(sftp_probe): VerifySample for none/none candidates"
```

---

### Task A3: `VerifySample` — compression cases (zstd / gzip / zlib)

**Files:**
- Modify: `go/internal/foxtrot/sftp_probe/verify_test.go`

**Step 1: Add three table-driven cases**

Append to `verify_test.go`:

```go
import (
	"code.linenisgreat.com/madder/go/internal/bravo/plugins"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_io"
)

// candidateForLegacyCompression builds a Candidate whose IOConfig
// uses the named legacy compression (one of "none", "gzip", "zlib",
// "zstd"). Encryption is none. Hash is sha256.
func candidateForLegacyCompression(t *testing.T, comp string) Candidate {
	t.Helper()
	ref, err := plugins.LegacyCompressionRef(comp)
	if err != nil {
		t.Fatalf("LegacyCompressionRef(%q): %v", comp, err)
	}
	wrapper, err := plugins.GetCompressionWrapper(ref) // see note
	if err != nil {
		t.Fatalf("GetCompressionWrapper(%q): %v", ref, err)
	}
	cfg := blob_io.MakeConfig(
		blob_store_configs.DefaultHashType,
		nil, // funcJoin unused for verification
		wrapper,
		nil, // no encryption
	)
	return Candidate{IOConfig: cfg, Label: comp + "/none"}
}

func TestVerifySample_CompressionRoundTrips(t *testing.T) {
	cleartext := []byte("hello probe — non-trivial content for compression to do work")
	digest := sha256.Sum256(cleartext)
	expectedHex := hex.EncodeToString(digest[:])

	for _, comp := range []string{"none", "gzip", "zlib", "zstd"} {
		t.Run(comp, func(t *testing.T) {
			cand := candidateForLegacyCompression(t, comp)

			// Forward: encode cleartext through the candidate's IO
			// config to produce on-disk bytes.
			var encoded bytes.Buffer
			w, err := blob_io.NewWriter(cand.IOConfig, &encoded)
			if err != nil {
				t.Fatalf("NewWriter: %v", err)
			}
			if _, err := w.Write(cleartext); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if err := w.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			// Backward: VerifySample on the same candidate must
			// accept the encoded bytes.
			got := VerifySample(bytes.NewReader(encoded.Bytes()), expectedHex, cand)
			if !got.Ok {
				t.Fatalf("Ok=false; Stage=%s Err=%v", got.Stage, got.Err)
			}
		})
	}
}
```

**NOTE on `plugins.GetCompressionWrapper`:** if a function with that
exact name doesn't exist, locate the existing call site that
resolves a legacy compression ref to an `interfaces.IOWrapper` —
likely in `internal/foxtrot/blob_stores/store_inventory_archive.go`
where compression is wired in. Mirror that pattern. If the
resolution requires a registry/plugin host, expose a small public
helper from the `plugins` package that test code can call without
spinning up the full host.

**Step 2: Run test to verify it fails (or passes if NewReader/NewWriter just work)**

Run: `just test-go ./internal/foxtrot/sftp_probe/... -run CompressionRoundTrips -v`

If `none` passes but `gzip/zlib/zstd` fail with plugin-resolution
errors, that's the expected red. The `none` case may pass on
existing implementation alone.

**Step 3: Write whatever helper the test needs**

If `plugins.GetCompressionWrapper` had to be created, add it to
`internal/bravo/plugins/legacy.go` exposing the lookup that the
existing inventory-archive store uses. Add a unit test for the new
helper in `legacy_test.go`.

**Step 4: Re-run, expect green**

Run: `just test-go ./internal/foxtrot/sftp_probe/... -v`
Expected: PASS for all four compression types.

Run: `just vet-go`. Expected: clean.

**Step 5: Commit**

```bash
git add go/internal/foxtrot/sftp_probe/verify_test.go go/internal/bravo/plugins/
git commit -m "feat(sftp_probe): VerifySample handles all legacy compression types"
```

---

### Task A4: `VerifySample` — encryption with `-key` (age round-trip)

**Files:**
- Modify: `go/internal/foxtrot/sftp_probe/verify_test.go`

**Step 1: Add an age-keyed round-trip case**

Append to `verify_test.go`:

```go
import (
	"code.linenisgreat.com/madder/go/internal/bravo/markl"
)

// generateAgeKeyForTest produces a fresh age-x25519 private key as
// a markl.Id. The public-key half is implicit (markl IDs carry both).
func generateAgeKeyForTest(t *testing.T) markl.Id {
	t.Helper()
	var key markl.Id
	if err := key.GeneratePrivateKey(
		nil,
		markl.FormatIdAgeX25519Sec,
		markl.PurposeMadderPrivateKeyV1,
	); err != nil {
		t.Fatalf("GeneratePrivateKey: %v", err)
	}
	return key
}

func candidateForCompressionAndKey(
	t *testing.T,
	comp string,
	key *markl.Id,
) Candidate {
	t.Helper()
	ref, _ := plugins.LegacyCompressionRef(comp)
	wrapper, _ := plugins.GetCompressionWrapper(ref)
	var enc domain_interfaces.MarklId
	if key != nil {
		enc = key
	}
	cfg := blob_io.MakeConfig(
		blob_store_configs.DefaultHashType,
		nil,
		wrapper,
		enc,
	)
	label := comp + "/none"
	if key != nil {
		label = comp + "/age"
	}
	return Candidate{IOConfig: cfg, Label: label}
}

func TestVerifySample_AgeRoundTrip(t *testing.T) {
	cleartext := []byte("hello age — encrypted blob bytes")
	digest := sha256.Sum256(cleartext)
	expectedHex := hex.EncodeToString(digest[:])

	key := generateAgeKeyForTest(t)
	cand := candidateForCompressionAndKey(t, "zstd", &key)

	var encoded bytes.Buffer
	w, _ := blob_io.NewWriter(cand.IOConfig, &encoded)
	w.Write(cleartext)
	w.Close()

	got := VerifySample(bytes.NewReader(encoded.Bytes()), expectedHex, cand)
	if !got.Ok {
		t.Fatalf("Ok=false; Stage=%s Err=%v", got.Stage, got.Err)
	}
}
```

**Step 2: Run, expect pass without further code changes**

The infrastructure is already in place from A2. This test just
exercises the encryption arm of `MakeConfig`. If it fails, the
issue is in plumbing the key through `MakeConfig`'s `encryption
domain_interfaces.MarklId` parameter — debug there.

Run: `just test-go ./internal/foxtrot/sftp_probe/... -run AgeRoundTrip -v`
Expected: PASS.

**Step 3: Commit**

```bash
git add go/internal/foxtrot/sftp_probe/verify_test.go
git commit -m "test(sftp_probe): age-encryption round-trip"
```

---

### Task A5: `VerifySample` — failure-classification cases (rows 7–12)

**Files:**
- Modify: `go/internal/foxtrot/sftp_probe/verify_test.go`

**Step 1: Add the negative cases as a table**

```go
func TestVerifySample_FailureClassification(t *testing.T) {
	plaintext := []byte("hello probe — non-trivial content")
	digest := sha256.Sum256(plaintext)
	expectedHex := hex.EncodeToString(digest[:])

	keyA := generateAgeKeyForTest(t)
	keyB := generateAgeKeyForTest(t)

	type forward struct {
		comp string
		key  *markl.Id
	}
	type cand struct {
		comp string
		key  *markl.Id
	}

	cases := []struct {
		name      string
		forward   forward
		candidate cand
		wantStage Stage
	}{
		{"row7-zstd-as-gzip",
			forward{"zstd", nil}, cand{"gzip", nil},
			StageDecompress},
		{"row8-zstd-as-none",
			forward{"zstd", nil}, cand{"none", nil},
			StageHashMismatch},
		{"row9-ageK1-as-ageK2",
			forward{"none", &keyA}, cand{"none", &keyB},
			StageDecrypt},
		{"row10-zstd-ageK1-as-zstd-ageK2",
			forward{"zstd", &keyA}, cand{"zstd", &keyB},
			StageDecrypt},
		{"row11-plain-as-ageK1",
			forward{"none", nil}, cand{"none", &keyA},
			StageDecrypt},
		// row 12 in the design accepts either Decrypt or Decompress
		// — the pipeline order determines which fires first; assert
		// non-Ok plus stage ∈ {Decrypt, Decompress}.
		{"row12-zstd-ageK1-as-none-none",
			forward{"zstd", &keyA}, cand{"none", nil},
			-1 /* sentinel: any non-OK non-mismatch stage */},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fwd := candidateForCompressionAndKey(t, tc.forward.comp, tc.forward.key)
			c := candidateForCompressionAndKey(t, tc.candidate.comp, tc.candidate.key)

			var encoded bytes.Buffer
			w, _ := blob_io.NewWriter(fwd.IOConfig, &encoded)
			w.Write(plaintext)
			w.Close()

			got := VerifySample(bytes.NewReader(encoded.Bytes()), expectedHex, c)
			if got.Ok {
				t.Fatalf("expected Ok=false; got %+v", got)
			}

			if tc.wantStage == -1 {
				// row 12: any non-OK, non-HashMismatch stage acceptable.
				if got.Stage == StageOK || got.Stage == StageHashMismatch {
					t.Errorf("row12 expected Decrypt or Decompress; got %s",
						got.Stage)
				}
				return
			}

			if got.Stage != tc.wantStage {
				t.Errorf("Stage = %s, want %s (Err=%v)",
					got.Stage, tc.wantStage, got.Err)
			}
		})
	}
}
```

**Step 2: Run, refine `VerifySample` if any case classifies wrong**

Run: `just test-go ./internal/foxtrot/sftp_probe/... -run FailureClassification -v`

Expected fail modes that need `VerifySample` adjustments:

- Row 9/10/11: if `NewReader` returns an error AFTER the encryption
  layer attaches (i.e. err comes from inside `Read`, not `NewReader`),
  the `io.Copy` path fires first and we'd classify as Decompress.
  Inspect the error: if it wraps `age` package errors, classify as
  Decrypt regardless of which Go-level call returned it. Use
  `errors.Is`/`errors.As` against age error types.
- Row 7: gzip will reject zstd magic bytes during `Read`, so we'd
  hit `io.Copy` returning a decompression error. Classify
  Decompress. Confirm the error is from gzip, not age.

The classification rule:

```go
// If the error chain mentions the age package or contains "age:" or
// has a markl decode error type, it's a decrypt-stage failure.
// Otherwise, if it surfaces from the io.Copy phase, it's
// decompress.
```

Implement the classification helper in `verify.go`:

```go
func classifyReadError(err error) Stage {
	if err == nil {
		return StageOK
	}
	// age errors surface from the encryption WrapReader. Inspect
	// the error chain.
	if isAgeError(err) {
		return StageDecrypt
	}
	return StageDecompress
}

func isAgeError(err error) bool {
	// The age package's exported errors include age.NoIdentityMatchError
	// and age.IncorrectArmorError (filagosft/age v1 names). If the
	// markl wrapper rewraps them, walk the error chain and match
	// substrings as a fallback. Confirm by reading
	// internal/bravo/markl/format_family_agex25519.go.
	return strings.Contains(err.Error(), "age") ||
		strings.Contains(err.Error(), "no identity matched")
}
```

Adjust `VerifySample` to call `classifyReadError(err)` instead of
hard-coding `StageDecompress`.

**Step 3: Re-run, expect green**

Run: `just test-go ./internal/foxtrot/sftp_probe/... -run FailureClassification -v`
Expected: all 6 sub-cases PASS.

Run: `just vet-go`. Expected: clean.

**Step 4: Commit**

```bash
git add go/internal/foxtrot/sftp_probe/
git commit -m "feat(sftp_probe): classify VerifySample failures by stage"
```

---

### Task A6: `VerifySample` panic recovery

**Files:**
- Modify: `go/internal/foxtrot/sftp_probe/verify_test.go`

**Step 1: Add a panicking-reader test**

```go
type panickingReader struct{}

func (panickingReader) Read([]byte) (int, error) { panic("kaboom") }

func TestVerifySample_PanicRecovers(t *testing.T) {
	cand := candidateNoneNoneSha256(t)
	got := VerifySample(panickingReader{}, "deadbeef", cand)
	if got.Ok {
		t.Fatal("expected non-OK after panic")
	}
	if got.Err == nil || !strings.Contains(got.Err.Error(), "panic") {
		t.Errorf("expected error to mention panic; got %v", got.Err)
	}
}
```

**Step 2: Run; should pass thanks to existing `defer recover()` in A2**

If it doesn't, fix the `defer` block in `verify.go` to capture
panics from any phase, not just `NewReader`.

Run: `just test-go ./internal/foxtrot/sftp_probe/... -run PanicRecovers -v`
Expected: PASS.

**Step 3: Commit**

```bash
git add go/internal/foxtrot/sftp_probe/verify_test.go
git commit -m "test(sftp_probe): VerifySample recovers panics"
```

---

### Task A7: `EnumerateCandidates`

**Files:**
- Create: `go/internal/foxtrot/sftp_probe/candidates.go`
- Create: `go/internal/foxtrot/sftp_probe/candidates_test.go`

**Step 1: Write the failing test**

```go
package sftp_probe

import (
	"testing"

	"code.linenisgreat.com/madder/go/internal/bravo/markl"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_stores"
)

func TestEnumerateCandidates_NoKeys(t *testing.T) {
	layout := blob_stores.DiscoveredConfig{
		HashTypeId: "sha256",
		MultiHash:  false,
		Buckets:    []int{2},
	}
	got := EnumerateCandidates(layout, nil)
	if len(got) != 4 {
		t.Errorf("want 4 candidates (1 enc × 4 comp); got %d", len(got))
	}
	wantLabels := map[string]bool{
		"none/none": false, "gzip/none": false,
		"zlib/none": false, "zstd/none": false,
	}
	for _, c := range got {
		wantLabels[c.Label] = true
	}
	for label, ok := range wantLabels {
		if !ok {
			t.Errorf("missing candidate label %q", label)
		}
	}
}

func TestEnumerateCandidates_TwoKeys(t *testing.T) {
	layout := blob_stores.DiscoveredConfig{HashTypeId: "sha256", Buckets: []int{2}}
	keyA := generateAgeKeyForTest(t)
	keyB := generateAgeKeyForTest(t)
	got := EnumerateCandidates(layout, []markl.Id{keyA, keyB})
	want := 4 * (1 + 2)
	if len(got) != want {
		t.Errorf("want %d candidates; got %d", want, len(got))
	}
}

func TestEnumerateCandidates_StableOrder(t *testing.T) {
	layout := blob_stores.DiscoveredConfig{HashTypeId: "sha256", Buckets: []int{2}}
	keyA := generateAgeKeyForTest(t)
	a := EnumerateCandidates(layout, []markl.Id{keyA})
	b := EnumerateCandidates(layout, []markl.Id{keyA})
	if len(a) != len(b) {
		t.Fatalf("len mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Label != b[i].Label {
			t.Errorf("position %d: %q vs %q", i, a[i].Label, b[i].Label)
		}
	}
}
```

**Step 2: Run, expect FAIL (function doesn't exist)**

**Step 3: Implement**

```go
package sftp_probe

import (
	"fmt"

	"code.linenisgreat.com/madder/go/internal/bravo/markl"
	"code.linenisgreat.com/madder/go/internal/bravo/plugins"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_io"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_stores"
)

var legacyCompressionTypes = []string{"none", "gzip", "zlib", "zstd"}

// EnumerateCandidates returns the cross-product of legacy
// compression types × {no encryption} ∪ {age + each user-provided
// key}, all hashed with sha256. Order is deterministic: outer loop
// is encryption (none, then keys in input order), inner loop is
// compression in legacyCompressionTypes order.
func EnumerateCandidates(
	layout blob_stores.DiscoveredConfig,
	keys []markl.Id,
) []Candidate {
	out := make([]Candidate, 0, 4*(1+len(keys)))

	// First: no encryption × all compression types.
	for _, comp := range legacyCompressionTypes {
		out = append(out, makeCandidate(layout, comp, nil, "none"))
	}
	// Then: each key × all compression types.
	for i, key := range keys {
		keyTag := fmt.Sprintf("age-key%d", i+1)
		k := key // copy to avoid loop-variable aliasing on the &k below
		for _, comp := range legacyCompressionTypes {
			out = append(out, makeCandidate(layout, comp, &k, keyTag))
		}
	}
	return out
}

func makeCandidate(
	layout blob_stores.DiscoveredConfig,
	comp string,
	key *markl.Id,
	keyTag string,
) Candidate {
	ref, _ := plugins.LegacyCompressionRef(comp)
	wrapper, _ := plugins.GetCompressionWrapper(ref)

	var enc domain_interfaces.MarklId
	if key != nil {
		enc = key
	}

	ioCfg := blob_io.MakeConfig(
		blob_store_configs.DefaultHashType,
		nil,
		wrapper,
		enc,
	)

	storeCfg := &blob_store_configs.DefaultType{
		HashTypeId:      blob_store_configs.HashType(layout.HashTypeId),
		HashBuckets:     layout.Buckets,
		CompressionType: comp,
	}
	if key != nil {
		storeCfg.Encryption = []markl.Id{*key}
	}

	return Candidate{
		StoreConfig: storeCfg,
		IOConfig:    ioCfg,
		Label:       comp + "/" + keyTag,
	}
}
```

**Step 4: Run, expect green**

Run: `just test-go ./internal/foxtrot/sftp_probe/... -run EnumerateCandidates -v`
Expected: PASS.

Run: `just vet-go`. Expected: clean.

**Step 5: Commit**

```bash
git add go/internal/foxtrot/sftp_probe/candidates.go go/internal/foxtrot/sftp_probe/candidates_test.go
git commit -m "feat(sftp_probe): EnumerateCandidates"
```

---

### Task A8: `Aggregate` and `Rank`

**Files:**
- Create: `go/internal/foxtrot/sftp_probe/aggregate.go`
- Create: `go/internal/foxtrot/sftp_probe/aggregate_test.go`

**Step 1: Write tests**

```go
package sftp_probe

import "testing"

func TestRank_VerifiedDescending(t *testing.T) {
	a := Aggregate{Candidate: Candidate{Label: "a"}, Verified: 5, Total: 10}
	b := Aggregate{Candidate: Candidate{Label: "b"}, Verified: 10, Total: 10}
	c := Aggregate{Candidate: Candidate{Label: "c"}, Verified: 3, Total: 10}
	got := Rank([]Aggregate{a, b, c})
	if got[0].Candidate.Label != "b" {
		t.Errorf("want b first; got %s", got[0].Candidate.Label)
	}
	if got[2].Candidate.Label != "c" {
		t.Errorf("want c last; got %s", got[2].Candidate.Label)
	}
}

func TestRank_StageDiversityTiebreak(t *testing.T) {
	// Both verified=0/10. The single-stage failure ranks higher
	// (more diagnosable than the multi-stage flailer).
	single := Aggregate{
		Candidate: Candidate{Label: "single"},
		Verified:  0, Total: 10,
		Stages: map[Stage]int{StageDecrypt: 10},
	}
	multi := Aggregate{
		Candidate: Candidate{Label: "multi"},
		Verified:  0, Total: 10,
		Stages: map[Stage]int{StageDecrypt: 4, StageDecompress: 4, StageHashMismatch: 2},
	}
	got := Rank([]Aggregate{multi, single})
	if got[0].Candidate.Label != "single" {
		t.Errorf("want single first (single failure stage); got %s", got[0].Candidate.Label)
	}
}

func TestRank_EmptyDoesNotPanic(t *testing.T) {
	got := Rank(nil)
	if got != nil && len(got) != 0 {
		t.Errorf("want empty; got %v", got)
	}
}

func TestAggregateAdd(t *testing.T) {
	var agg Aggregate
	agg.Add(SampleResult{Ok: true, Stage: StageOK})
	agg.Add(SampleResult{Ok: false, Stage: StageDecrypt})
	if agg.Verified != 1 {
		t.Errorf("Verified=%d want 1", agg.Verified)
	}
	if agg.Total != 2 {
		t.Errorf("Total=%d want 2", agg.Total)
	}
	if agg.Stages[StageOK] != 1 || agg.Stages[StageDecrypt] != 1 {
		t.Errorf("Stages = %v", agg.Stages)
	}
}
```

**Step 2: Run, expect FAIL**

**Step 3: Implement**

```go
package sftp_probe

import "sort"

type Aggregate struct {
	Candidate Candidate
	Verified  int
	Total     int
	Stages    map[Stage]int
}

func (a *Aggregate) Add(r SampleResult) {
	if a.Stages == nil {
		a.Stages = make(map[Stage]int)
	}
	a.Total++
	if r.Ok {
		a.Verified++
	}
	a.Stages[r.Stage]++
}

// Rank sorts aggregates by Verified desc, ties broken by stage
// diversity: among aggregates with equal Verified, the one with
// fewer distinct failure stages ranks higher (single-stage
// failures are more diagnosable).
func Rank(in []Aggregate) []Aggregate {
	out := append([]Aggregate(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Verified != out[j].Verified {
			return out[i].Verified > out[j].Verified
		}
		di := distinctFailureStages(out[i])
		dj := distinctFailureStages(out[j])
		return di < dj
	})
	return out
}

func distinctFailureStages(a Aggregate) int {
	n := 0
	for stage, count := range a.Stages {
		if stage == StageOK || count == 0 {
			continue
		}
		n++
	}
	return n
}
```

**Step 4: Run, expect green; vet clean**

**Step 5: Commit**

```bash
git add go/internal/foxtrot/sftp_probe/aggregate.go go/internal/foxtrot/sftp_probe/aggregate_test.go
git commit -m "feat(sftp_probe): Aggregate and Rank"
```

---

## Phase B — `madder-test-craft-legacy-blob` fixture binary

A small `_test`-tagged Go binary used by bats to materialize legacy-shaped
blobs deterministically. Lives under `go/cmd/madder-test-craft-legacy-blob/`
mirroring `madder-test-sftp-server`.

### Task B1: Skeleton + flag parsing

**Files:**
- Create: `go/cmd/madder-test-craft-legacy-blob/main.go`
- Create: `go/cmd/madder-test-craft-legacy-blob/main_test.go`

**Step 1: Failing test**

```go
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestHelp asserts the binary prints flag help on -h without
// crashing. This is a smoke test before we wire flag handling.
func TestHelp(t *testing.T) {
	bin := buildHelper(t)
	cmd := exec.Command(bin, "-h")
	cmd.Stderr = os.Stderr
	out, _ := cmd.Output()
	// `flag` package writes help to stderr by default and exits non-zero
	// on -h; we just confirm the binary built and ran.
	_ = out
}

func buildHelper(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "craft")
	cmd := exec.Command("go", "build", "-tags", "test", "-o", bin, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build: %v", err)
	}
	return bin
}
```

**Step 2: Run, expect FAIL — package doesn't compile**

**Step 3: Implement minimal skeleton**

```go
//go:build test
// +build test

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	var (
		comp    = flag.String("compression", "none", "none|gzip|zlib|zstd")
		enc     = flag.String("encryption", "none", "none|age")
		recip   = flag.String("recipient", "", "age recipient pubkey if -encryption=age")
		content = flag.String("content", "-", "source file or '-' for stdin")
		out     = flag.String("out", "", "destination path (required)")
	)
	flag.Parse()

	if *out == "" {
		fmt.Fprintln(os.Stderr, "must pass -out <path>")
		os.Exit(64)
	}

	_ = comp
	_ = enc
	_ = recip
	_ = content

	if err := run(*comp, *enc, *recip, *content, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(comp, enc, recip, contentPath, outPath string) error {
	var src io.Reader = os.Stdin
	if contentPath != "-" {
		f, err := os.Open(contentPath)
		if err != nil {
			return err
		}
		defer f.Close()
		src = f
	}

	dst, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	// TODO: wire compression/encryption in B3/B4. For now passthrough.
	_, err = io.Copy(dst, src)
	return err
}
```

**Step 4: Run, expect PASS**

Run: `just test-go ./cmd/madder-test-craft-legacy-blob/...`

**Step 5: Commit**

```bash
git add go/cmd/madder-test-craft-legacy-blob/
git commit -m "feat(madder-test-craft-legacy-blob): skeleton with passthrough"
```

---

### Task B2-B4: wire compression and encryption (single combined step)

**Files:**
- Modify: `go/cmd/madder-test-craft-legacy-blob/main.go`
- Modify: `go/cmd/madder-test-craft-legacy-blob/main_test.go`

**Step 1: Failing test — compression round trip**

```go
func TestCraft_ZstdRoundTrip(t *testing.T) {
	bin := buildHelper(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "out.bin")

	cmd := exec.Command(bin, "-compression", "zstd", "-out", out)
	cmd.Stdin = bytes.NewReader([]byte("hello craft"))
	if err := cmd.Run(); err != nil {
		t.Fatalf("craft run: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// zstd magic
	if !bytes.HasPrefix(data, []byte{0x28, 0xb5, 0x2f, 0xfd}) {
		t.Errorf("expected zstd magic prefix; got % x", data[:4])
	}
}
```

**Step 2: Run, expect FAIL — passthrough binary doesn't compress**

**Step 3: Implement compression and encryption**

Replace `run()` in `main.go` with a version that uses
`blob_io.NewWriter` (which already wires compression and
encryption from a `blob_io.Config`). Construct the `Config` from
the flags, exactly mirroring what `sftp_probe.makeCandidate` does
in Task A7. Reuse the helper if practical.

```go
// (sketch) — use plugins.LegacyCompressionRef, plugins.GetCompressionWrapper,
// markl.SetFromPath for the recipient if -encryption=age.
// Then blob_io.MakeConfig + blob_io.NewWriter.
```

The recipient path (when `-encryption=age`) is a public-key file;
load via `markl.SetFromPath` matching the `setEncryptionFlagDefinition`
pattern in `internal/charlie/blob_store_configs/encryption.go`.

**Step 4: Run, expect PASS for zstd test; add gzip, zlib, age tests**

Add table-driven tests for gzip / zlib / none, and a single age
round-trip test that:
- generates a key in a `t.TempDir()`,
- writes the public-key half to disk,
- runs the binary with `-encryption age -recipient <pubkey-path>`,
- asserts the output starts with `age-encryption.org/v1\n`.

**Step 5: Commit**

```bash
git add go/cmd/madder-test-craft-legacy-blob/
git commit -m "feat(madder-test-craft-legacy-blob): wire compression and age encryption"
```

---

### Task B5: Wire into nix build

**Files:**
- Modify: `go/default.nix` (likely; mirror what `madder-test-sftp-server` does)
- Possibly: `flake.nix` if outputs are listed there

**Step 1: Locate the existing test-server's build entry**

```bash
grep -n 'madder-test-sftp-server' go/default.nix flake.nix
```

**Step 2: Add a sibling entry for `madder-test-craft-legacy-blob`**

Mirror the test-server's nix derivation: same source root, same Go
build args, just a different cmd path. Confirm with:

```bash
nix build .#madder-test-craft-legacy-blob
ls -l result/bin/
```

The bats `require_bin` path treats `MADDER_TEST_CRAFT_LEGACY_BLOB`
as the env override; emit it from the build the same way
`MADDER_TEST_SFTP_SERVER` is emitted. This may also require a tiny
update to the test runners' env block in the `justfile` recipes
under `test-bats-targets`.

**Step 3: Verify build, smoke test**

```bash
just build
ls result/bin/madder-test-craft-legacy-blob
result/bin/madder-test-craft-legacy-blob -h
```

**Step 4: Commit**

```bash
git add go/default.nix flake.nix justfile
git commit -m "build: package madder-test-craft-legacy-blob into nix output"
```

---

## Phase C — Command wrapper

The wrapper lives at `go/internal/india/commands/sftp_analyze_and_suggest_configs.go`.
It owns flags, SSH dial, sampling, the probe-library invocation, output
emission, the huh prompts, and the bootstrap call.

We build it in slices that compile and run end-to-end after each step,
even when behavior is incomplete. Each task adds exactly one capability.

### Task C1: Skeleton, flag definitions, command registration

**Files:**
- Create: `go/internal/india/commands/sftp_analyze_and_suggest_configs.go`
- Modify: `go/internal/india/commands/CLAUDE.md` (add a one-line entry)

**Step 1: Failing test**

There is no existing test file for india/commands beyond
`main_test.go`. Add a minimal flag-definition smoke test in a new
file `go/internal/india/commands/sftp_analyze_and_suggest_configs_test.go`:

```go
package commands

import (
	"strings"
	"testing"

	"code.linenisgreat.com/madder/go/internal/futility"
)

func TestSftpAnalyzeRegistered(t *testing.T) {
	cmd, ok := utility.GetCmd("sftp-analyze-and-suggest-configs")
	if !ok {
		t.Fatal("sftp-analyze-and-suggest-configs not registered")
	}
	desc := cmd.GetDescription()
	if !strings.Contains(desc.Short, "analyze") {
		t.Errorf("short desc lacks 'analyze': %q", desc.Short)
	}
	// Verify it implements futility.CommandWithFlags so flags exist.
	_, ok = cmd.(futility.CommandWithFlags)
	if !ok {
		t.Error("command does not implement futility.CommandWithFlags")
	}
}
```

NOTE: confirm `utility.GetCmd` exists; the existing init pattern
uses `utility.AddCmd`. If a getter doesn't exist, use the existing
`main_test.go` style — find `Init` cmd in the registry the same
way the existing tests do.

**Step 2: Run, expect FAIL**

Run: `just test-go ./internal/india/commands/... -run SftpAnalyzeRegistered -v`

**Step 3: Implement skeleton**

```go
package commands

import (
	"code.linenisgreat.com/madder/go/internal/futility"
	"code.linenisgreat.com/madder/go/internal/golf/command_components"
	"code.linenisgreat.com/purse-first/libs/dewey/0/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("sftp-analyze-and-suggest-configs", &SftpAnalyzeAndSuggestConfigs{})
}

type SftpAnalyzeAndSuggestConfigs struct {
	command_components.EnvBlobStore

	sshHost          string
	remotePath       string
	knownHostsFile   string
	keyPaths         []string
	limit            int
	maxSampleBytes   int64
	emitTop          int
	yesToAll         bool
}

var _ futility.CommandWithFlags = (*SftpAnalyzeAndSuggestConfigs)(nil)

func (cmd SftpAnalyzeAndSuggestConfigs) GetDescription() futility.Description {
	return futility.Description{
		Short: "analyze a legacy SFTP blob store and suggest blob_store-config candidates",
		Long: "Read-only probe of a legacy SFTP remote without a " +
			"blob_store-config file. Samples blobs, generates candidate " +
			"configs, sample-verifies them through the existing reader " +
			"pipeline, and offers an interactive deep-verify and " +
			"bootstrap flow. Emits candidate files to $TMPDIR. " +
			"See sftp-analyze-and-suggest-configs(1) for the full " +
			"contract and exit-code policy.",
	}
}

func (cmd *SftpAnalyzeAndSuggestConfigs) SetFlagDefinitions(
	flags interfaces.CLIFlagDefinitions,
) {
	flags.StringVar(&cmd.sshHost, "ssh-host", "", "ssh_config Host alias")
	flags.StringVar(&cmd.remotePath, "remote-path", "",
		"remote root containing buckets")
	flags.StringVar(&cmd.knownHostsFile, "known-hosts-file", "",
		"optional; default $HOME/.ssh/known_hosts")
	flags.IntVar(&cmd.limit, "limit", 10, "samples to draw")
	flags.Int64Var(&cmd.maxSampleBytes, "max-sample-bytes", 1<<20,
		"skip blobs larger than this")
	flags.IntVar(&cmd.emitTop, "emit-top", 5,
		"max candidate files to write")
	flags.BoolVar(&cmd.yesToAll, "yes-to-all", false,
		"auto-confirm every prompt; combine with non-tty for scripted runs")
	flags.Func("key", "age private key path; repeatable",
		func(v string) error {
			cmd.keyPaths = append(cmd.keyPaths, v)
			return nil
		})
}

func (cmd SftpAnalyzeAndSuggestConfigs) Run(req futility.Request) {
	env := cmd.MakeEnvBlobStore(req)
	_ = env
	// Phase C2-onward fills this in. For now: register only.
	_ = values.MakeUri
}
```

**Step 4: Run, expect PASS**

Run: `just test-go ./internal/india/commands/... -run SftpAnalyzeRegistered -v`

Run: `just vet-go`. Expected: clean.

**Step 5: Commit**

```bash
git add go/internal/india/commands/sftp_analyze_and_suggest_configs.go go/internal/india/commands/sftp_analyze_and_suggest_configs_test.go go/internal/india/commands/CLAUDE.md
git commit -m "feat(india/commands): register sftp-analyze-and-suggest-configs skeleton"
```

---

### Task C2: SSH dial + remote-path validation

**Files:**
- Modify: `go/internal/india/commands/sftp_analyze_and_suggest_configs.go`

**Step 1: Implementation**

Use the existing `makeSSHClientForSFTPViaSSHConfig` helper at
`init.go:382+`. If it's not exported, lift it to a package-level
helper or call it directly (we're in the same package).

In `Run`:

```go
func (cmd SftpAnalyzeAndSuggestConfigs) Run(req futility.Request) {
	env := cmd.MakeEnvBlobStore(req)

	if err := cmd.validateFlags(env); err != nil {
		errors.ContextCancelWithBadRequestError(env, err)
		return
	}

	sshClient, err := makeSSHClientForSshConfigAlias(
		req,
		cmd.sshHost,
		cmd.knownHostsFile,
	)
	if err != nil { /* ... */ }
	defer sshClient.Close()

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil { /* ... */ }
	defer sftpClient.Close()

	// validate remote-path is a directory
	stat, err := sftpClient.Stat(cmd.remotePath)
	if err != nil { /* ... */ }
	if !stat.IsDir() {
		errors.ContextCancelWithBadRequestf(env,
			"-remote-path must be a directory; got file at %q", cmd.remotePath)
		return
	}

	env.GetUI().Printf("connected to %s; remote-path=%s",
		cmd.sshHost, cmd.remotePath)
}
```

`validateFlags` checks that `sshHost` and `remotePath` are
non-empty, `limit >= 1`, `emitTop >= 1`, `maxSampleBytes >= 1024`,
and that each `cmd.keyPaths[i]` parses as an age private key (load
via `markl.SetFromPath` matching the existing pattern).

**Step 2: Smoke test via integration (no unit test for this layer)**

We don't add a unit test for the SSH dial — bats does that.
Confirm the code builds and vets cleanly. Bats #11 (bad inputs)
covers this in Phase D.

**Step 3: Run vet and build**

Run: `just vet-go`. Expected: clean.
Run: `just build-go`. Expected: clean.

**Step 4: Commit**

```bash
git add go/internal/india/commands/sftp_analyze_and_suggest_configs.go
git commit -m "feat(sftp-analyze): connect via ssh_config and validate remote-path"
```

---

### Task C3: Sampling walk

**Files:**
- Modify: `go/internal/india/commands/sftp_analyze_and_suggest_configs.go`

**Step 1: Implementation sketch**

Add a private `scatterSample` method on `SftpAnalyzeAndSuggestConfigs`:

```go
type sample struct {
	relPath   string // path relative to remoteRoot, with leading dirs
	digestHex string
	buf       []byte
}

func (cmd SftpAnalyzeAndSuggestConfigs) scatterSample(
	sftpClient *sftp.Client,
	remoteRoot string,
	layout blob_stores.DiscoveredConfig,
	limit int,
) ([]sample, error)
```

Algorithm exactly per the design doc § 3 (random hex-prefix scatter,
bounded retries, buffered to `maxSampleBytes`). Reconstruct each
expected digest via `markl.SetHexStringFromRelPath`.

Note: `markl.SetHexStringFromRelPath` is used in
`store_remote_sftp.go:484`. Inspect that call site for the exact
signature and what `relPath` form it expects (with or without
bucket prefix included). Mirror that.

**Step 2: Vet, build**

No unit test here — covered by bats #2 (legacy zstd/none) which
exercises the full sample → verify path. Add a TODO comment in the
code referencing bats #2 as the integration test.

Run: `just vet-go && just build-go`. Expected: clean.

**Step 3: Commit**

```bash
git add go/internal/india/commands/sftp_analyze_and_suggest_configs.go
git commit -m "feat(sftp-analyze): scatter-sample blobs from remote"
```

---

### Task C4: Wire probe library, run aggregate, emit TAP plan

**Files:**
- Modify: `go/internal/india/commands/sftp_analyze_and_suggest_configs.go`

**Step 1: Implementation**

After sampling, call `sftp_probe.EnumerateCandidates` and run
`VerifySample` for each (sample, candidate) pair. Aggregate the
results, rank, then emit a TAP plan to stdout.

```go
import (
	tap "github.com/amarbel-llc/tap/go"
	"code.linenisgreat.com/madder/go/internal/foxtrot/sftp_probe"
)

// (inside Run)
keys, err := loadKeys(cmd.keyPaths)
if err != nil { /* ... */ }

candidates := sftp_probe.EnumerateCandidates(layout, keys)
aggregates := make([]sftp_probe.Aggregate, len(candidates))
for i, c := range candidates {
	aggregates[i].Candidate = c
}

for _, s := range samples {
	for i, c := range candidates {
		r := sftp_probe.VerifySample(bytes.NewReader(s.buf), s.digestHex, c)
		aggregates[i].Add(r)
	}
}

ranked := sftp_probe.Rank(aggregates)

tw := tap.NewWriter(os.Stdout)
for i, agg := range ranked {
	emitTAPLine(tw, i+1, agg)
}
tw.Plan(len(ranked))
```

`emitTAPLine` formats the TAP line per design § 4 with the YAML
diagnostic block. Don't emit candidate files yet — Task C5 wires
that. The bootstrap block in the YAML can include placeholder
`<not-yet-emitted>` for the path; C5 fills it in.

**Step 2: Vet, build, smoke**

Run: `just vet-go && just build-go`.

**Step 3: Commit**

```bash
git add go/internal/india/commands/sftp_analyze_and_suggest_configs.go
git commit -m "feat(sftp-analyze): rank candidates and emit TAP summary"
```

---

### Task C5: Candidate file emission

**Files:**
- Modify: `go/internal/india/commands/sftp_analyze_and_suggest_configs.go`

**Step 1: Implementation**

For each of the top `cmd.emitTop` candidates with `Verified > 0`,
write a hyphence-encoded file to
`$TMPDIR/madder-suggest-<runid>/candidate-<NN>-<comp>-<enc>.hyphence`.

Use the same encoding as `WriteRemoteConfig`:

```go
typedConfig := &hyphence.TypedBlob[blob_store_configs.Config]{
	Type: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigVCurrent).TypeStruct,
	Blob: candidate.StoreConfig,
}
blob_store_configs.Coder.EncodeTo(typedConfig, file)
```

Generate the runid via `fmt.Sprintf("%016x", time.Now().UnixNano())`
or the existing project convention if there is one (look for
similar runid patterns in `inventory_log` or `madder-test-sftp-server`).

Update the TAP YAML diagnostic to reference the actual file path
and to include the bootstrap block:

```yaml
bootstrap:
  - ssh '<sshHost>' test ! -e '<remotePath>/blob_store-config' \
      || { echo 'remote blob_store-config already exists; refusing'; exit 1; }
  - scp '<file path>' '<sshHost>:<remotePath>/blob_store-config'
  - ssh '<sshHost>' chmod 0444 '<remotePath>/blob_store-config'
```

**Step 2: Vet, build**

**Step 3: Commit**

```bash
git add go/internal/india/commands/sftp_analyze_and_suggest_configs.go
git commit -m "feat(sftp-analyze): emit candidate files with bootstrap block"
```

---

### Task C6: Existing-config validation

**Files:**
- Modify: `go/internal/india/commands/sftp_analyze_and_suggest_configs.go`

**Step 1: Implementation**

Before sampling (so we know if existing exists for emission gating),
attempt to open `<remotePath>/blob_store-config` and decode it:

```go
existingCandidate, hasExisting, existingDecodeErr :=
	tryReadExistingConfig(sftpClient, cmd.remotePath)
```

If `hasExisting` and `existingDecodeErr == nil`: prepend the
`existingCandidate` to the candidate slice with label `"existing"`.

If `hasExisting` and `existingDecodeErr != nil`: emit a top-of-output
banner via `env.GetUI().Printf` noting the unparseable bytes;
proceed with synthesized candidates only.

If `existingCandidate.StoreConfig` references unsupported fields
(e.g. encryption format we don't have a wrapper for): treat as
fatal; cancel the request before sampling. (This is the "exit 1"
case from the design.)

The "always sorts to position #1" requirement is handled in `Rank`:
add a special-case check at the top of the comparator so a
candidate with `Label == "existing"` sorts first regardless of
verified count. Add a unit test for this in `aggregate_test.go`.

**Step 2: Vet, build, run probe-library tests to confirm rank fix didn't break them**

Run: `just test-go ./internal/foxtrot/sftp_probe/...`

**Step 3: Commit**

```bash
git add go/internal/foxtrot/sftp_probe/ go/internal/india/commands/sftp_analyze_and_suggest_configs.go
git commit -m "feat(sftp-analyze): validate existing remote blob_store-config"
```

---

### Task C7-C9: huh prompts (deep-verify, bootstrap-anyway, bootstrap)

**Files:**
- Modify: `go/internal/india/commands/sftp_analyze_and_suggest_configs.go`

**Step 1: Implementation**

After ranking + TAP emission + candidate-file emission, run the
interactive flow per design § 7:

```go
func (cmd SftpAnalyzeAndSuggestConfigs) runInteractiveFlow(
	env command_components.BlobStoreEnv,
	sftpClient *sftp.Client,
	samples []sample,
	ranked []sftp_probe.Aggregate,
) (exitCode int) {
	if len(ranked) == 0 || ranked[0].Verified != ranked[0].Total {
		return 1
	}
	top := ranked[0]

	// huh #2: deep-verify?
	wantDeep, err := cmd.confirm("Deep-verify " + top.Candidate.Label +
		" against the full store?")
	if err != nil { /* ... */ }

	deepFailures := 0
	deepWalked := 0
	if wantDeep {
		deepWalked, deepFailures = cmd.runDeepVerify(sftpClient, samples, top.Candidate)
		if deepFailures > 0 {
			ok, err := cmd.confirm(fmt.Sprintf(
				"Deep-verify found %d failures of %d. Bootstrap anyway?",
				deepFailures, deepWalked))
			if err != nil { /* ... */ }
			if !ok {
				return 1
			}
		}
	}

	// huh #3: bootstrap?
	wantBootstrap, err := cmd.confirm("Bootstrap " + top.Candidate.Label +
		" to " + cmd.sshHost + ":" + cmd.remotePath + "?")
	if err != nil { /* ... */ }
	if !wantBootstrap {
		if deepFailures > 0 {
			return 2
		}
		return 0
	}

	if err := cmd.runBootstrap(sftpClient, top.Candidate); err != nil {
		return 1
	}

	if deepFailures > 0 {
		return 2
	}
	return 0
}

// confirm runs huh.Confirm if stdin is a tty, otherwise returns
// cmd.yesToAll.
func (cmd SftpAnalyzeAndSuggestConfigs) confirm(msg string) (bool, error) {
	if cmd.yesToAll {
		return true, nil
	}
	if !isTerminal(os.Stdin.Fd()) {
		return false, nil
	}
	return huhwrapper.Prompter{}.Confirm(msg)
}
```

`isTerminal` — use the existing repo's idiom (check
`internal/futility` or `dewey/.../ui` for an `IsTerminal` helper;
otherwise `golang.org/x/term`).

`runDeepVerify` walks the entire bucket tree and runs
`VerifySample` against each blob with the `top.Candidate`. Stream
progress to stderr (e.g. every 100 blobs print a count). Emit a
TAP `not ok 2 - deep-verify` block on completion if failures > 0,
or `ok 2 - deep-verify` if zero.

`runBootstrap` calls `blob_stores.WriteRemoteConfig` directly with
the candidate's `StoreConfig`. For the existing-fails overwrite
path, perform `sftpClient.Chmod(configPath, 0o644)` first, then
`WriteRemoteConfig`, then `Chmod 0o444`.

**Step 2: Vet, build**

No unit tests here — bats covers all interactive paths. Confirm
build:

```bash
just vet-go && just build-go
```

**Step 3: Commit**

```bash
git add go/internal/india/commands/sftp_analyze_and_suggest_configs.go
git commit -m "feat(sftp-analyze): interactive deep-verify + bootstrap flow via huh"
```

---

### Task C10: Exit-code mapping

**Files:**
- Modify: `go/internal/india/commands/sftp_analyze_and_suggest_configs.go`

**Step 1: Implementation**

Confirm the exit-code logic from C7-C9 produces:
- 0 — clean success or user-declined-bootstrap with no deep-verify failures
- 1 — connect failure, no candidate verified, existing-references-unknown,
  deep-verify-failures gate declined, bootstrap call errored
- 2 — bootstrap completed with consented deep-verify failures

Search for the existing pattern for exit-coding from a futility
command. If the framework calls `os.Exit(returnedCode)`, just
return the code. If it doesn't, use `errors.ContextCancel` with a
typed error that the framework maps to a code. Look at how
`madder fsck` reports failure to see the convention.

**Step 2: Vet, build**

**Step 3: Commit**

```bash
git add go/internal/india/commands/sftp_analyze_and_suggest_configs.go
git commit -m "feat(sftp-analyze): exit-code policy 0/1/2"
```

---

## Phase D — Bats integration tests

The bats file follows `sftp.bats` conventions. Each test owns its own
remote tree (built via `madder-test-craft-legacy-blob` and shell `mv`)
and asserts both stdout/exit and (for read-only tests) before/after
remote-tree snapshots.

### Task D1: Bats file scaffold + helper for tree construction

**Files:**
- Create: `zz-tests_bats/sftp_analyze_and_suggest_configs.bats`
- Create: `zz-tests_bats/lib/sftp_legacy.bash`

**Step 1: Failing test — bats #1 (verifies a freshly-init'd store)**

```bash
# zz-tests_bats/sftp_analyze_and_suggest_configs.bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  load "$(dirname "$BATS_TEST_FILE")/lib/sftp.bash"
  load "$(dirname "$BATS_TEST_FILE")/lib/sftp_legacy.bash"
  export output
  start_sftp_server
}

teardown() {
  stop_sftp_server
}

# bats file_tags=net_cap

function existing_verifies_does_not_emit_candidates { # @test
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello" >"$blob"
  run_madder write -format tap .sftp-test "$blob"
  assert_success

  # Run analyze without -yes-to-all (non-tty default → no candidate files).
  local tmpdir="$BATS_TEST_TMPDIR/suggest"
  mkdir -p "$tmpdir"
  TMPDIR="$tmpdir" run_madder sftp-analyze-and-suggest-configs \
    -ssh-host 127.0.0.1 \
    -remote-path "$BATS_TEST_TMPDIR/sftp-remote"
  assert_success
  assert_output --partial 'ok 1 - existing'

  # No candidate files on disk.
  local n
  n="$(ls -1 "$tmpdir"/madder-suggest-*/candidate-*.hyphence 2>/dev/null | wc -l || true)"
  [[ $n -eq 0 ]] || fail "expected zero candidate files; found $n"
}
```

NOTE: `-ssh-host 127.0.0.1` won't actually resolve via ssh_config —
that's a problem. The bats fixture exposes the server as
`127.0.0.1:$SFTP_PORT` and the existing `init-sftp-test-store`
uses the `init-sftp-explicit` flavor. **Our command is
ssh_config-only.** Two options:

1. Add an `init-sftp-via-ssh-config-test-store` helper to
   `lib/sftp.bash` that writes a per-test ssh_config file pointing
   `madder-test` at `127.0.0.1:$SFTP_PORT`, sets
   `$HOME/.ssh/config` (under `BATS_TEST_TMPDIR`-based `$HOME` to
   avoid polluting the user's real config), and runs the analyze
   command with `-ssh-host madder-test`.
2. Relax v1's "ssh_config-only" rule for bats — add a hidden
   `-host`/`-port`/`-user`/`-private-key-path` flag set that bats
   uses but humans don't.

Option 1 is cleaner and exercises the real code path. Add the
helper in `lib/sftp_legacy.bash`:

```bash
# write_test_ssh_config <alias> <host> <port> <user> <known_hosts>
# Writes an isolated ssh_config under $BATS_TEST_TMPDIR and sets
# $HOME so subsequent ssh_config lookups find it.
write_test_ssh_config() {
  local alias="$1" host="$2" port="$3" user="$4" known_hosts="$5"
  local fake_home="$BATS_TEST_TMPDIR/home"
  mkdir -p "$fake_home/.ssh"
  cat >"$fake_home/.ssh/config" <<EOF
Host $alias
  HostName $host
  Port $port
  User $user
  UserKnownHostsFile $known_hosts
  StrictHostKeyChecking yes
EOF
  export HOME="$fake_home"
}
```

Use this in test #1 above:

```bash
write_test_ssh_config madder-test 127.0.0.1 "$SFTP_PORT" testuser "$SFTP_KNOWN_HOSTS"
TMPDIR="$tmpdir" run_madder sftp-analyze-and-suggest-configs \
  -ssh-host madder-test \
  -remote-path "$BATS_TEST_TMPDIR/sftp-remote"
```

**Step 2: Run, expect FAIL (multiple things missing)**

Run: `just test-bats-targets sftp_analyze_and_suggest_configs.bats`

**Step 3: Iteratively fix until green**

The first run will fail at multiple points: command not registered
(should be fixed by C1), command crashes on no SSH client (C2),
missing helper, etc. Fix each as it surfaces. The point of bats #1
is to get end-to-end plumbing through the existing-config-verifies
path.

**Step 4: Commit when test #1 passes**

```bash
git add zz-tests_bats/sftp_analyze_and_suggest_configs.bats zz-tests_bats/lib/sftp_legacy.bash
git commit -m "test(bats): existing config verifies path for sftp-analyze"
```

---

### Task D2: Bats #2-4 — legacy layout detection (zstd/none, none/none, gzip/none)

**Files:**
- Modify: `zz-tests_bats/sftp_analyze_and_suggest_configs.bats`
- Modify: `zz-tests_bats/lib/sftp_legacy.bash`

**Step 1: Add a helper to materialize a legacy tree**

```bash
# craft_legacy_blob <comp> <encryption> <recipient_or_-> <content_path> <out_path>
craft_legacy_blob() {
  local bin="${MADDER_TEST_CRAFT_LEGACY_BLOB:-madder-test-craft-legacy-blob}"
  "$bin" -compression "$1" -encryption "$2" -recipient "$3" \
    -content "$4" -out "$5"
}

# place_legacy_blob_at_correct_path <root> <comp> <enc> <recipient_or_-> <content>
# Hashes <content> with sha256, writes <root>/<HH>/<rest> with the
# crafted bytes, where HH is the first two hex chars of the digest
# and <rest> is the remaining 62 chars.
place_legacy_blob_at_correct_path() {
  local root="$1" comp="$2" enc="$3" recip="$4" content="$5"

  local hex
  hex="$(printf "%s" "$content" | sha256sum | awk '{print $1}')"
  local prefix="${hex:0:2}"
  local rest="${hex:2}"
  mkdir -p "$root/$prefix"

  local content_path="$BATS_TEST_TMPDIR/.tmp-content-$$"
  printf "%s" "$content" >"$content_path"

  craft_legacy_blob "$comp" "$enc" "$recip" "$content_path" "$root/$prefix/$rest"
  rm "$content_path"
}
```

**Step 2: Add tests #2-4**

```bash
function detects_unencrypted_zstd_legacy { # @test
  local root="$BATS_TEST_TMPDIR/legacy-store"
  mkdir -p "$root"

  place_legacy_blob_at_correct_path "$root" zstd none - "blob 1 content"
  place_legacy_blob_at_correct_path "$root" zstd none - "blob 2 content"
  place_legacy_blob_at_correct_path "$root" zstd none - "blob 3 content"

  write_test_ssh_config madder-test 127.0.0.1 "$SFTP_PORT" testuser "$SFTP_KNOWN_HOSTS"
  local tmpdir="$BATS_TEST_TMPDIR/suggest"; mkdir -p "$tmpdir"

  TMPDIR="$tmpdir" run_madder sftp-analyze-and-suggest-configs \
    -ssh-host madder-test \
    -remote-path "$root" \
    -limit 3
  assert_success
  assert_output --partial 'verified=3/3'
  assert_output --partial 'zstd/none'

  # Candidate file was written.
  local n
  n="$(ls -1 "$tmpdir"/madder-suggest-*/candidate-*-zstd-none.hyphence 2>/dev/null | wc -l)"
  [[ $n -eq 1 ]] || fail "expected zstd/none candidate file; found $n"
}

function detects_uncompressed_unencrypted_legacy { # @test
  # ... mirror of #2 but with comp=none
}

function detects_gzip_legacy { # @test
  # ... mirror with comp=gzip
}
```

**Step 3: Run, fix any iteration issues, commit**

Run: `just test-bats-targets sftp_analyze_and_suggest_configs.bats`

```bash
git add zz-tests_bats/
git commit -m "test(bats): legacy layout detection (zstd, none, gzip)"
```

---

### Task D3: Bats #5-6 — encryption with-key, no-key, wrong-key

**Files:**
- Modify: `zz-tests_bats/sftp_analyze_and_suggest_configs.bats`

Add three tests using the age round-trip in `craft_legacy_blob`. The
key generation can use the existing `madder` CLI — there should be a
key-generation path via the `init -encryption generate` flow we saw
in `encryption.go`. If not, extend `madder-test-craft-legacy-blob`
with a `-generate-key <out-path>` mode that writes a fresh keypair.

```bash
function age_encrypted_with_key_provided { # @test ... }
function age_encrypted_no_key_fails { # @test ... }
function age_encrypted_wrong_key_fails { # @test ... }
```

Commit after each passes.

---

### Task D4: Bats #7 — read-only invariant

**Files:**
- Modify: `zz-tests_bats/sftp_analyze_and_suggest_configs.bats`

```bash
function probing_phase_is_read_only { # @test
  local root="$BATS_TEST_TMPDIR/legacy-store"
  mkdir -p "$root"
  for i in 1 2 3 4 5; do
    place_legacy_blob_at_correct_path "$root" zstd none - "blob $i"
  done

  # snapshot: path TAB inode TAB size TAB mtime
  local before="$BATS_TEST_TMPDIR/snap-before"
  ( cd "$root" && find . -type f -printf '%p\t%i\t%s\t%T@\n' | sort ) >"$before"

  write_test_ssh_config madder-test 127.0.0.1 "$SFTP_PORT" testuser "$SFTP_KNOWN_HOSTS"
  local tmpdir="$BATS_TEST_TMPDIR/suggest"; mkdir -p "$tmpdir"

  # No -yes-to-all → bootstrap doesn't happen → remote stays untouched.
  TMPDIR="$tmpdir" run_madder sftp-analyze-and-suggest-configs \
    -ssh-host madder-test \
    -remote-path "$root" \
    -limit 5

  local after="$BATS_TEST_TMPDIR/snap-after"
  ( cd "$root" && find . -type f -printf '%p\t%i\t%s\t%T@\n' | sort ) >"$after"

  diff "$before" "$after" || fail "remote tree changed during read-only run"
}
```

Commit.

---

### Task D5: Bats #8 — bootstrap end-to-end via `-yes-to-all`

**Files:**
- Modify: `zz-tests_bats/sftp_analyze_and_suggest_configs.bats`

```bash
function yes_to_all_bootstraps_top_candidate { # @test
  local root="$BATS_TEST_TMPDIR/legacy-store"
  mkdir -p "$root"
  place_legacy_blob_at_correct_path "$root" zstd none - "blob 1"
  place_legacy_blob_at_correct_path "$root" zstd none - "blob 2"

  write_test_ssh_config madder-test 127.0.0.1 "$SFTP_PORT" testuser "$SFTP_KNOWN_HOSTS"
  local tmpdir="$BATS_TEST_TMPDIR/suggest"; mkdir -p "$tmpdir"

  TMPDIR="$tmpdir" run_madder sftp-analyze-and-suggest-configs \
    -ssh-host madder-test \
    -remote-path "$root" \
    -limit 2 \
    -yes-to-all
  assert_success

  # On-remote config exists with mode 0444.
  [[ -e "$root/blob_store-config" ]] || fail "blob_store-config not written"
  local mode
  mode="$(stat -c '%a' "$root/blob_store-config")"
  [[ $mode == 444 ]] || fail "expected mode 444; got $mode"

  # Byte-identical to the candidate file.
  local cand
  cand="$(ls "$tmpdir"/madder-suggest-*/candidate-*-zstd-none.hyphence)"
  cmp "$root/blob_store-config" "$cand" || fail "remote config does not match candidate"
}
```

Commit.

---

### Task D6: Bats #9-12 — boundary cases

Add tests:
- #9 `-limit 3` against a 100-blob tree → exactly 3 SFTP `Open`
  calls. Instrumentation: tail the stderr file from
  `madder-test-sftp-server` (its `start_sftp_server` already
  redirects stderr to a file the test can grep). Count `Open`
  log lines for the bucket-paths in the tree.
- #10 `-emit-top 2` → exactly 2 candidate files on disk.
- #11 missing `-ssh-host`, missing `-remote-path`, missing
  `-key` file path each fail fast with a clear error.
- #12 empty buckets → exit 1 with "no blobs found".

Commit.

---

### Task D7: Bats #13-18 — existing-config cases

Tests:
- #13 working store + `-yes-to-all` → existing verifies AND
  alternative candidate files exist.
- #14 working store, hand-edit on-remote config to claim wrong
  compression, run via bats no-tty no-flag → existing fails 0/N,
  no candidate files written, exit 1.
- #15 same as #14 with `-yes-to-all` → correct candidate file
  written with overwrite bootstrap; on-remote config replaced.
- #16 same as #14 with stdin `<<< $'y\ny\ny\n'` → identical to #15.
  Verify via bats's stdin redirection.
- #17 working store + corrupted on-remote config → treated as
  "no existing config"; synthesized candidates emitted with
  non-overwrite bootstrap.
- #18 hand-craft existing config with unsupported encryption
  format → halt with explicit error; exit 1; no synthesized
  candidates.

Commit per group.

---

### Task D8: Bats #19-23 — deep-verify cases

Tests:
- #19 verifying tree, stdin `<<< $'y\ny\ny\n'` → deep-verify
  reports 100% verified, bootstrap runs, exit 0.
- #20 verifying tree with one corrupted blob (manually flip a byte
  in one bucket file), stdin `<<< $'y\ny\ny\n'` → deep-verify
  reports failures, huh #2.5 yes, bootstrap runs, exit 2.
- #21 same as #20, stdin `<<< $'y\ny\nn\n'` → deep-verify reports
  failures, huh #2.5 no, no bootstrap, exit 1.
- #22 `-yes-to-all` against partially-corrupted store → bootstrap
  runs (huh #2.5 auto-yes), exit 2.
- #23 huh-requires-tty regression: bats no-tty no-flag run does NOT
  panic and falls back to non-tty mode.

Commit per group.

---

## Phase E — Documentation

### Task E1: Man page

**Files:**
- Create: `docs/man.1/sftp-analyze-and-suggest-configs.md`

**Step 1: Write the man page**

Mirror the structure of an existing command's man page (e.g.
`docs/man.1/init.md` if it exists, or the closest analog). Include:

- NAME, SYNOPSIS, DESCRIPTION, OPTIONS, EXAMPLES, EXIT CODES,
  SEE ALSO.
- The exit-code table (0 / 1 / 2) prominently in EXIT CODES.
- Reference the design doc at
  `docs/plans/2026-05-08-sftp-analyze-and-suggest-configs-design.md`
  in SEE ALSO.

**Step 2: Confirm it renders**

Run: `just debug-gen_man sftp-analyze-and-suggest-configs.1`
Expected: clean output, no rendering errors.

**Step 3: Wire into the man-page generator if not auto-discovered**

Inspect `go/cmd/madder-gen_man/main.go` (or equivalent) — if it
discovers commands automatically from `utility.AddCmd`
registrations, no change needed. Otherwise add a registration.

**Step 4: Commit**

```bash
git add docs/man.1/sftp-analyze-and-suggest-configs.md
git commit -m "docs(man): sftp-analyze-and-suggest-configs(1)"
```

---

## Final Validation

### Task F1: Full test sweep

Run the full Go suite, the full bats suite (both lanes), and
verify the entire build chain:

```bash
just vet-go
just test-go
just build
just test-bats-targets sftp_analyze_and_suggest_configs.bats
```

All must pass. If any test fails, file the failure as a TODO and
fix before merging.

### Task F2: Smoke-test against real legacy stores

NOTE: this step is optional — real legacy stores live on remote
infrastructure and may not be safe to probe from CI. If the user
is around, ask them to run the command against their hosts
themselves and confirm:

- Read-only invariant holds (no `blob_store-config` magically
  appears on either remote).
- A candidate file is emitted that the user could plausibly
  bootstrap.

Document the actual real-store outcomes in a follow-up plan
document if the user wants a record.

### Task F3: Merge

Use `mcp__plugin_spinclass_spinclass__merge-this-session` with
`git_sync: true` per the project's spinclass workflow. The
pre-merge hook runs `just`, which exercises the entire test
matrix.

---

## Rollback procedure

If after merge the command misbehaves:

1. Revert the merge commit on `master` (single revert; the feature
   is greenfield + additive).
2. The reverted commit removes `internal/foxtrot/sftp_probe/`,
   `internal/india/commands/sftp_analyze_and_suggest_configs.go`,
   `cmd/madder-test-craft-legacy-blob/`,
   `zz-tests_bats/sftp_analyze_and_suggest_configs.bats`,
   `docs/man.1/sftp-analyze-and-suggest-configs.md`, and the
   command's registration. No other code paths are affected.
3. Any `blob_store-config` that was written to a remote during a
   bootstrap step is the user's data and is removed only by them
   (`ssh <host> rm <remote>/blob_store-config`). Bootstrap requires
   explicit consent (huh #3 or `-yes-to-all`); no silent writes.
