# Config Digest Pins — Phase 2 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use eng:subagent-driven-development
> to implement this plan task-by-task.

**Goal:** Extend `blob_store_id.Id` to carry an optional digest suffix
(`name@blake2b256-…`). At resolve time, when an ID's digest is set,
assert it against the resolved store's Phase 1 self-check digest;
mismatch surfaces `markl.ErrNotEqual`, and a digest supplied against
a legacy (un-migrated) config surfaces a dedicated typed error that
points at `madder config-pin_digest`.

**Architecture:** Phase 1 landed the self-check (`@` line on every
fresh config, `DecodeAndVerify` on every read). Phase 2 layers a
parser-side change on `blob_store_id.Id` plus a single new
`AssertEqual` at the resolver seam (`blob_store_env.GetBlobStore`).
Nothing on disk changes: this FDR does **not** rewrite existing
receipts, configs, or inventory archive entries to embed digests in
the IDs they reference; emission is per-call-site opt-in future work.

**Tech Stack:** Go, `blob_store_id`, `markl` (FormatHash, Id, Equals,
ErrNotEqual), futility CLI framework, bats integration tests.

**Rollback:** The new `digest` field is zero-value-safe; setting it
to the zero `markl.Id` makes the suffix invisible in
`Canonical`/`MarshalText`. Every new code path is guarded on
`id.HasDigest()`, so reverting the resolver `AssertEqual` and the
parser's `@` handling restores prior behavior.

**FDR:** [`docs/features/0008-config-digest-pins.md`](../features/0008-config-digest-pins.md)
(§Phase 2: Digest Suffix on Blob-Store-IDs).

---

## Phase 1 implementation notes worth knowing

These are facts the Phase 1 implementation surfaced that aren't
obvious from the FDR alone. Read them before starting.

### 1. The on-disk `@` line drops the purpose prefix

The hyphence metadata coder uses `Id.String()` to render `BlobDigest`
on the wire (`go/internal/charlie/hyphence/coder_metadata.go:56-67`).
`Id.String()` returns `blech32(format, data)` **without** the
`<purpose>@` prefix (the prefix only appears in
`StringWithFormat`). So:

- The `@` line in a `blob_store-config` looks like
  `@ blake2b256-<blech32>`, **not**
  `@ madder-blob_store-config-digest-v1@blake2b256-<blech32>`.
- After decode, `typedConfig.BlobDigest.GetPurposeId()` is `""`.
  The format+data round-trip, but the purpose is dropped.

**Consequence for Phase 2:** `markl.AssertEqual` calls
`markl.Equals`, which compares `(format, data)` only — not purpose
(`go/internal/bravo/markl/util.go:244-268`). So an ID-side digest
parsed via `markl.Id.Set` (purpose may or may not be present in the
CLI input) and a config-side digest from `DecodeAndVerify` (purpose
will be empty) compare equal when the underlying bytes match. The
purpose mismatch is invisible to the resolver, which is what we
want.

### 2. The decoded `TypedConfig` is already reachable from the resolver

`BlobStoreInitialized` embeds `ConfigNamed`, which carries
`Config TypedConfig` directly (see
`go/internal/foxtrot/blob_stores/initialized.go` and
`go/internal/delta/blob_store_configs/named.go`). By the time
`GetBlobStore` is called, the config has already been read and
verified by `DecodeAndVerifyFromFile` (Phase 1 wiring), so:

- `blobStore.Config.BlobDigest` is the config's digest if it's
  migrated, or the null `markl.Id` if it's legacy.
- The resolver does **not** need to re-decode or re-hash anything.
  It just reads the field.

### 3. EncodeWithDigest/DecodeAndVerify are body-bytes-symmetric

Phase 1's read/write contract: `EncodeWithDigest` writes the body
**once** to a scratch buffer, hashes those bytes, and assembles the
output as `Boundary + metadata + Boundary + blank + bodyBuf`.
`DecodeAndVerify` locates the body via
`bytes.LastIndex(all, "---\n\n")` and hashes the raw on-disk body
bytes. No inner-encoder-determinism dependency.

**Consequence for Phase 2:** none directly — Phase 2 doesn't touch
this path. But if a Phase 2 task adds an emission call site
(persisting a digest-bearing ID in a receipt, etc.), it should use
`Canonical()` to render the ID, and the digest will come from
whatever in-memory `Id` value the call site already has. Don't try
to re-derive the digest from disk at emit time.

### 4. `Id.String()` is the lookup key, `Id.Canonical()` is the wire form

`Id.String()` is used as the `BlobStoreMap` key (discovery-side
construction in `foxtrot/blob_stores/main.go:33,57`; resolver
lookup in `foxtrot/blob_store_env/main.go:185`; multi-store
dedup in `foxtrot/blob_stores/multi_builder.go:175`) and as the
sort key in several commands. **Discovery never produces
digest-bearing IDs**, so every map key is bare `[prefix]name`.

This plan therefore keeps `String()` bare (no digest, no behavior
change) and pushes the digest-bearing rendering into `Canonical()`.
The FDR text form (`[prefix]name[@digest]`) is what `Canonical`
emits; the FDR does not mandate that `String()` match. The split
keeps the entire `String()`-based code surface — map keys,
sorting, completion output, JSON id fields — untouched, and
restricts Phase 2's render-side change to `Canonical` +
`MarshalText` + the on-disk wire form (which delegates through
`MarshalText`).

### 5. `blob_store_id.Id.Set` is whitespace-sensitive about prefixes

The current `Set` parser (`main.go:85`) splits on `value[0]`:

- `.` (one or more) → CWD location, `cwdDepth = dots - 1`
- `/`, `_`, `~`, `%` → matching XDG scope
- Otherwise → XDG user

The name charset is implicit: anything after the prefix character(s)
is the name. Phase 2's grammar (FDR §Phase 2 text form) extends this
with `@<markl-id>` as an unambiguous suffix because `@` is not a
prefix character and is not in the name charset (`[a-zA-Z0-9_-]`).
**Split on the first `@` first**, then route the left side through
the existing prefix/name parser unchanged. Don't try to interleave.

### 6. `init_store` uses `.default` (CWD), not `default` (XDG user)

The bats helper in `zz-tests_bats/lib/common.bash:44` calls
`madder init -encryption none .default`. The blob_store_id is
`.default` (Cwd location), and the BlobStoreMap is keyed by
`.default` (`Id.String()` form). When writing bats tests for
Phase 2 that reference the store explicitly, use `.default` — not
`default`. The same trap caught the Phase 1 bats tests for
`config-pin_digest` and `list`; see the post-mortem in those bats
files for the pattern.

---

## Task 1: Extend `Id` struct with `digest` field + accessors

**Promotion criteria:** Field exists and is zero-value-safe; accessors
round-trip. No behavior change yet (parser/renderer untouched in this
task).

**Files:**
- Modify: `go/internal/alfa/blob_store_id/main.go`
- Test: `go/internal/alfa/blob_store_id/main_test.go` (create if absent)

**Step 1: Write the failing test**

Append to `main_test.go` (create with `//go:build test` header if it
doesn't exist):

```go
//go:build test

package blob_store_id

import (
	"testing"

	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	_ "github.com/amarbel-llc/madder/go/internal/charlie/markl_registrations"
)

func TestId_WithDigest_RoundTrip(t *testing.T) {
	var digest markl.Id
	if err := digest.SetMarklId(
		markl.FormatIdHashBlake2b256,
		make([]byte, 32),
	); err != nil {
		t.Fatalf("SetMarklId: %v", err)
	}

	id := Make("default").WithDigest(digest)

	if !id.HasDigest() {
		t.Fatal("HasDigest = false, want true")
	}

	got := id.GetDigest()
	if got.GetMarklFormat().GetMarklFormatId() != markl.FormatIdHashBlake2b256 {
		t.Errorf("digest format = %v, want blake2b256",
			got.GetMarklFormat().GetMarklFormatId())
	}

	if Make("default").HasDigest() {
		t.Error("zero-value digest should report HasDigest = false")
	}
}
```

**Step 2: Run test to verify it fails**

```
just test-go -run TestId_WithDigest_RoundTrip ./internal/alfa/blob_store_id/...
```

Expected: `WithDigest` undefined.

**Step 3: Extend the struct + add accessors**

In `go/internal/alfa/blob_store_id/main.go`, add the markl import
and extend `Id`:

```go
import (
	"encoding"
	"fmt"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/0/xdg_location_type"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

type Id struct {
	location xdg_location_type.Typee
	id       string
	cwdDepth uint
	digest   markl.Id // FDR-0008 Phase 2; zero value = no digest
}

// GetDigest returns the digest suffix on this Id, or the null
// markl.Id when no digest is set. See FDR-0008 Phase 2.
func (id Id) GetDigest() markl.Id {
	return id.digest
}

// HasDigest reports whether this Id carries an FDR-0008 Phase 2
// digest suffix.
func (id Id) HasDigest() bool {
	return !id.digest.IsNull()
}

// WithDigest returns a copy of id with its digest field set.
func (id Id) WithDigest(digest markl.Id) Id {
	id.digest = digest
	return id
}
```

**Step 4: Run test to verify it passes**

```
just test-go -run TestId_WithDigest_RoundTrip ./internal/alfa/blob_store_id/...
```

Expected: PASS.

**Step 5: Commit**

```bash
git add go/internal/alfa/blob_store_id/main.go \
        go/internal/alfa/blob_store_id/main_test.go
git commit -m "blob_store_id: add digest field + accessors

Phase 2 of FDR-0008 scaffold. Adds GetDigest, HasDigest, WithDigest;
no parser/renderer changes yet — those land in the next task.
Refs: #194, #198.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 2: Parser + Canonical/MarshalText — keep `String()` bare

**Promotion criteria:** Round-trip is byte-identical for IDs with
and without digests via `Canonical` + `Set`. `String()` continues
to return the bare form (no digest), so existing map-key and
sort-key call sites are unaffected. A malformed `@<markl-id>`
suffix is a hard parse error.

**Files:**
- Modify: `go/internal/alfa/blob_store_id/main.go`
- Test: `go/internal/alfa/blob_store_id/main_test.go`

**Step 1: Write the failing tests**

Append to `main_test.go`:

```go
func TestId_Set_ParsesDigestSuffix(t *testing.T) {
	cases := []struct {
		input      string
		wantName   string
		wantCwd    bool
		wantDigest string // expected GetMarklFormatId
	}{
		{
			input:      "default@blake2b256-9ft3m74lwx9aq4nx7vrft96xeku0scrz8ymyljh9phpvfzv4kdgsmwxsws",
			wantName:   "default",
			wantDigest: markl.FormatIdHashBlake2b256,
		},
		{
			input:      ".archive@blake2b256-9ft3m74lwx9aq4nx7vrft96xeku0scrz8ymyljh9phpvfzv4kdgsmwxsws",
			wantName:   "archive",
			wantCwd:    true,
			wantDigest: markl.FormatIdHashBlake2b256,
		},
		{
			input:    "default",
			wantName: "default",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.input, func(t *testing.T) {
			var id Id
			if err := id.Set(c.input); err != nil {
				t.Fatalf("Set(%q): %v", c.input, err)
			}
			if id.GetName() != c.wantName {
				t.Errorf("GetName = %q, want %q", id.GetName(), c.wantName)
			}
			gotCwd := id.GetLocationType() == xdg_location_type.Cwd
			if gotCwd != c.wantCwd {
				t.Errorf("Cwd = %v, want %v", gotCwd, c.wantCwd)
			}
			if c.wantDigest == "" {
				if id.HasDigest() {
					t.Errorf("HasDigest = true, want false")
				}
				return
			}
			if !id.HasDigest() {
				t.Fatalf("HasDigest = false, want true")
			}
			gotFmt := id.GetDigest().GetMarklFormat().GetMarklFormatId()
			if gotFmt != c.wantDigest {
				t.Errorf("digest format = %q, want %q", gotFmt, c.wantDigest)
			}
		})
	}
}

func TestId_Canonical_RoundTripsDigest(t *testing.T) {
	const input = ".archive@blake2b256-9ft3m74lwx9aq4nx7vrft96xeku0scrz8ymyljh9phpvfzv4kdgsmwxsws"

	var id Id
	if err := id.Set(input); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got := id.Canonical()
	if got != input {
		t.Errorf("Canonical round-trip: got %q, want %q", got, input)
	}
}

// String() MUST NOT include the digest suffix — it is the
// BlobStoreMap key and is used as a sort key in many places.
// Discovery never produces digest-bearing IDs, so the map is
// always keyed by the bare form; if String() started returning the
// digest-bearing form, every digest-bearing CLI arg would miss in
// the map. See "Phase 1 implementation notes #4".
func TestId_String_OmitsDigest(t *testing.T) {
	const input = ".archive@blake2b256-9ft3m74lwx9aq4nx7vrft96xeku0scrz8ymyljh9phpvfzv4kdgsmwxsws"

	var id Id
	if err := id.Set(input); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got := id.String()
	const want = ".archive"
	if got != want {
		t.Errorf("String() = %q, want %q (bare form, no digest)", got, want)
	}
}

func TestId_Set_RejectsMalformedDigest(t *testing.T) {
	var id Id
	err := id.Set("default@not-a-real-markl-id")
	if err == nil {
		t.Fatal("Set: expected error on malformed digest, got nil")
	}
}

func TestId_MarshalText_RoundTrip(t *testing.T) {
	const input = "default@blake2b256-9ft3m74lwx9aq4nx7vrft96xeku0scrz8ymyljh9phpvfzv4kdgsmwxsws"

	var src Id
	if err := src.Set(input); err != nil {
		t.Fatalf("Set: %v", err)
	}

	bites, err := src.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}

	var dst Id
	if err := dst.UnmarshalText(bites); err != nil {
		t.Fatalf("UnmarshalText: %v", err)
	}

	if dst.Canonical() != src.Canonical() {
		t.Errorf("round-trip: got %q, want %q",
			dst.Canonical(), src.Canonical())
	}
}
```

**Step 2: Run tests to verify they fail**

```
just test-go -run TestId_Set_ParsesDigestSuffix ./internal/alfa/blob_store_id/...
```

Expected: tests fail (digest is empty after Set).

**Step 3: Implement the parser + Canonical**

In `Id.Set` (lines 85-133), wrap the existing body with a split on
the first `@`:

```go
func (id *Id) Set(value string) (err error) {
	if len(value) == 0 {
		err = errors.Errorf("empty blob_store_id")
		return err
	}

	// FDR-0008 Phase 2: split on the first `@`. The name charset
	// ([a-zA-Z0-9_-]) excludes `@`, so the first occurrence is
	// unambiguously the digest separator.
	left, digestText, hasDigest := strings.Cut(value, "@")
	if hasDigest {
		if err = id.digest.Set(digestText); err != nil {
			err = errors.Wrapf(err,
				"blob_store_id digest: %q", digestText)
			return err
		}
	} else {
		id.digest = markl.Id{}
	}

	value = left

	// ...existing prefix/name parsing logic on `value` unchanged...
}
```

Leave `Id.String` (lines 59-75) untouched — it returns the bare
`[prefix]name` form and is used as the BlobStoreMap key in many
places (see "Phase 1 implementation notes #4").

Update `Id.Canonical` (lines 77-83) to emit the digest-bearing
wire form:

```go
func (id Id) Canonical() string {
	id.cwdDepth = 0
	bare := id.String()
	if id.digest.IsNull() {
		return bare
	}
	return bare + "@" + id.digest.String()
}
```

`MarshalText` already delegates to `Canonical`, so it inherits the
suffix automatically. `UnmarshalText` already delegates to `Set`,
so it parses the suffix automatically.

> NOTE: `markl.Id.String()` returns `blech32(format, data)` without
> the purpose prefix (see "Phase 1 implementation notes #1"). That
> matches the FDR's text-form grammar
> (`markl-id = format "-" blech32-data`), so the round-trip is
> well-defined. If a future call site needs the purpose-bearing
> form, it should use `markl.Id.StringWithFormat` instead.

**Step 4: Run tests to verify they pass**

```
just test-go ./internal/alfa/blob_store_id/...
```

Expected: PASS.

**Step 5: Commit**

```bash
git add go/internal/alfa/blob_store_id/main.go \
        go/internal/alfa/blob_store_id/main_test.go
git commit -m "blob_store_id: parse + render \`@<markl-id>\` digest suffix

Phase 2 of FDR-0008. Id.Set splits on the first \`@\` (the name
charset excludes \`@\`, so this is unambiguous), routing the right
side through markl.Id.Set; the left side is parsed by the existing
prefix/name logic unchanged. Id.Canonical (and through it
MarshalText) renders the suffix when present. Id.String stays
bare to preserve every existing BlobStoreMap-key call site.

Refs: #194, #198.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 3: `Less` ordering — digest as the final tie-breaker

**Promotion criteria:** Two IDs differing only in their digest sort
deterministically. Existing `Less` behavior for IDs without digests
is unchanged.

**Files:**
- Modify: `go/internal/alfa/blob_store_id/main.go`
- Test: `go/internal/alfa/blob_store_id/main_test.go`

**Step 1: Write the failing invariant test**

Append to `main_test.go`:

```go
import (
	"bytes"
	// ...
)

func TestId_Less_DigestTieBreaker(t *testing.T) {
	// Two IDs that differ ONLY in digest bytes must sort
	// deterministically. The exact ordering doesn't matter for the
	// invariant; only that Less(a, b) != Less(b, a) when a != b.
	var d1, d2 markl.Id
	bytes1 := make([]byte, 32)
	bytes1[0] = 0x01
	bytes2 := make([]byte, 32)
	bytes2[0] = 0x02

	if err := d1.SetMarklId(markl.FormatIdHashBlake2b256, bytes1); err != nil {
		t.Fatal(err)
	}
	if err := d2.SetMarklId(markl.FormatIdHashBlake2b256, bytes2); err != nil {
		t.Fatal(err)
	}

	a := Make("default").WithDigest(d1)
	b := Make("default").WithDigest(d2)

	if a.Less(b) == b.Less(a) {
		t.Fatal("Less is not antisymmetric for digest-only-differing ids")
	}

	// Direction: lexicographic on digest bytes.
	want := bytes.Compare(bytes1, bytes2) < 0
	if a.Less(b) != want {
		t.Errorf("Less direction: a.Less(b) = %v, want %v",
			a.Less(b), want)
	}
}

func TestId_Less_BareIdsUnchanged(t *testing.T) {
	a := Make("alpha")
	b := Make("bravo")
	if !a.Less(b) || b.Less(a) {
		t.Errorf("bare-id ordering regressed: a.Less(b)=%v b.Less(a)=%v",
			a.Less(b), b.Less(a))
	}
}
```

**Step 2: Run tests to verify the digest-tie-breaker test fails**

```
just test-go -run TestId_Less_DigestTieBreaker ./internal/alfa/blob_store_id/...
```

Expected: fails (both `Less` return false for digest-only-differing
IDs because the existing `Less` returns at the `cwdDepth` step).

**Step 3: Extend `Less`**

```go
func (id Id) Less(otherId Id) bool {
	if id.location != otherId.location {
		return id.location < otherId.location
	}
	if id.id != otherId.id {
		return id.id < otherId.id
	}
	if id.cwdDepth != otherId.cwdDepth {
		return id.cwdDepth < otherId.cwdDepth
	}
	// FDR-0008 Phase 2: digest as the final tie-breaker.
	// Compares the data-bytes of the markl.Id lexicographically;
	// null digests sort first.
	return bytes.Compare(id.digest.GetBytes(), otherId.digest.GetBytes()) < 0
}
```

Add `"bytes"` to the imports.

**Step 4: Run tests to verify they pass**

```
just test-go -run TestId_Less ./internal/alfa/blob_store_id/...
```

Expected: PASS.

**Step 5: Commit**

```bash
git add go/internal/alfa/blob_store_id/main.go \
        go/internal/alfa/blob_store_id/main_test.go
git commit -m "blob_store_id: digest as final Less tie-breaker

Phase 2 of FDR-0008. After (location, id, cwdDepth), Less compares
the digest bytes lexicographically. Two IDs differing only in their
digest now sort deterministically. Null digests sort first.
Refs: #194, #198.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 4: Resolver `AssertEqual` + `ErrIdDigestVsLegacyConfig`

**Promotion criteria:** When a CLI arg parses to an `Id` with
`HasDigest()`, the resolver either succeeds (digests match), fails
with `markl.ErrNotEqual` (digests differ), or fails with the new
typed error (config is legacy / un-migrated).

**Files:**
- Create: `go/internal/alfa/blob_store_id/errors.go`
- Modify: `go/internal/foxtrot/blob_store_env/main.go`

**Step 1: Add the typed error**

Create `go/internal/alfa/blob_store_id/errors.go`:

```go
package blob_store_id

import (
	"fmt"
)

// ErrIdDigestVsLegacyConfig is returned when a blob-store-id carries
// a digest suffix but the resolved store's config is legacy (no `@`
// line — pre-FDR-0008). Silently trusting an ID-supplied digest
// against an un-digestable config defeats the point of the suffix,
// so the error is hard and points the user at the migration command.
type ErrIdDigestVsLegacyConfig struct {
	Id string
}

func (err ErrIdDigestVsLegacyConfig) Error() string {
	return fmt.Sprintf(
		"blob-store-id %q supplied a digest but the store's config "+
			"is unmigrated. Run `madder config-pin_digest %s` to mint "+
			"a digest, then retry.",
		err.Id, err.Id,
	)
}

func (err ErrIdDigestVsLegacyConfig) Is(target error) bool {
	_, ok := target.(ErrIdDigestVsLegacyConfig)
	return ok
}
```

**Step 2: Wire the resolver**

In `go/internal/foxtrot/blob_store_env/main.go`'s `GetBlobStore`
(around line 182):

```go
func (env BlobStoreEnv) GetBlobStore(
	blobStoreId blob_store_id.Id,
) blob_stores.BlobStoreInitialized {
	// Per Phase 1 implementation notes #4, the map is keyed by the
	// bare String() form — discovery never produces digest-bearing
	// IDs, and String() always returns the bare form even when
	// blobStoreId carries a digest. Lookup is unchanged from
	// Phase 1.
	key := blobStoreId.String()

	blobStore, ok := env.blobStores[key]
	if !ok {
		available := slices.Collect(maps.Keys(env.blobStores))
		sort.Strings(available)
		errors.ContextCancelWithBadRequestf(
			env,
			"blob store not found: %q (available: %v)",
			key, available,
		)
		return blob_stores.BlobStoreInitialized{}
	}

	// FDR-0008 Phase 2: if the ID carries a digest, assert it
	// against the resolved config's Phase 1 digest. A legacy config
	// (BlobDigest is null) with an ID-supplied digest is a hard
	// typed error.
	if blobStoreId.HasDigest() {
		configDigest := blobStore.Config.BlobDigest
		if configDigest.IsNull() {
			env.Cancel(blob_store_id.ErrIdDigestVsLegacyConfig{
				Id: key,
			})
			return blob_stores.BlobStoreInitialized{}
		}
		idDigest := blobStoreId.GetDigest()
		if err := markl.AssertEqual(&idDigest, &configDigest); err != nil {
			env.Cancel(errors.Wrapf(err,
				"blob-store-id digest does not match resolved store %q",
				key,
			))
			return blob_stores.BlobStoreInitialized{}
		}
	}

	return blobStore
}
```

Add `markl` to the imports if not already present.

> NOTE: `markl.AssertEqual` takes `domain_interfaces.MarklId`; both
> sides need a pointer (`&idDigest`, `&configDigest`) because `Id`
> satisfies the interface only via pointer receiver (see
> `go/internal/bravo/markl/id.go:14-17`). Mirror the pattern from
> `delta/blob_store_configs/verify.go:88`.

> NOTE: `markl.Equals` compares `(format, data)` only (not purpose),
> so the ID-side digest's purpose (which the user may or may not
> have typed) and the config-side digest's purpose (always empty
> after `DecodeAndVerify`; see Phase 1 implementation notes #1) are
> irrelevant to the comparison.

**Step 3: Run the Go test suite for regressions**

```
just test-go ./...
```

Expected: PASS. If a test fails because it constructed a
digest-bearing ID and pointed it at a fixture without a matching
on-disk digest, the test is exercising the new failure mode
correctly — either update the fixture or change the test to expect
the error.

**Step 4: Commit**

```bash
git add go/internal/alfa/blob_store_id/errors.go \
        go/internal/foxtrot/blob_store_env/main.go
git commit -m "blob_store_env: assert ID-supplied digest at resolve time

Phase 2 of FDR-0008. blob_store_env.GetBlobStore now asserts that
an ID-supplied digest (when present) matches the resolved store's
Phase 1 self-check digest. A mismatch returns markl.ErrNotEqual;
an ID-supplied digest against a legacy config returns the new
ErrIdDigestVsLegacyConfig pointing at \`madder config-pin_digest\`.

Refs: #194, #198.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 5: bats integration tests for end-to-end Phase 2 behavior

**Promotion criteria:** A digest-bearing store id round-trips through
`madder fsck` (or any command that takes a blob-store-id arg).
Mismatch fails with both digests in the message. Legacy-config
mismatch fails with the migration hint.

**Files:**
- Create: `zz-tests_bats/config_digest_phase2.bats`

**Step 1: Write the bats tests**

```bash
setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=config_digest_phase2

# A digest-bearing id matching the on-disk config resolves cleanly.
function phase2_matching_digest_resolves { # @test
  init_store

  local config=".madder/local/share/blob_stores/default/blob_store-config"
  local digest
  digest="$(grep -E '^@ blake2b256-' "$config" | awk '{print $2}')"
  [[ -n $digest ]] || fail "no @ digest line in $config"

  # Any command that takes a blob-store-id arg exercises the
  # resolver. fsck is the cleanest because it doesn't need data.
  run_madder fsck ".default@$digest"
  assert_success
}

# A digest that doesn't match the on-disk config fails with
# markl.ErrNotEqual surfacing both digests.
function phase2_mismatched_digest_refuses { # @test
  init_store

  # Syntactically valid blake2b256 digest of different content.
  local bogus="blake2b256-9ft3m74lwx9aq4nx7vrft96xeku0scrz8ymyljh9phpvfzv4kdgsmwxsws"

  run_madder fsck ".default@$bogus"
  assert_failure
  assert_output --partial 'digest does not match'
  assert_output --partial 'expected'
}

# An id with a digest against a legacy (un-migrated) config fails
# with the dedicated typed error pointing at config-pin_digest.
function phase2_digest_against_legacy_refuses { # @test
  init_store

  # Strip the @ line to simulate a pre-FDR-0008 config.
  local config=".madder/local/share/blob_stores/default/blob_store-config"
  chmod 0644 "$config"
  sed -i.bak '/^@ /d' "$config" && rm "$config.bak"
  chmod 0444 "$config"

  local bogus="blake2b256-9ft3m74lwx9aq4nx7vrft96xeku0scrz8ymyljh9phpvfzv4kdgsmwxsws"

  run_madder fsck ".default@$bogus"
  assert_failure
  assert_output --partial 'unmigrated'
  assert_output --partial 'config-pin_digest'
}
```

> NOTE: `init_store` writes `.default` (CWD location). Use `.default`,
> not `default`, when constructing the digest-bearing id arg — see
> Phase 1 implementation notes #6.

> NOTE: The `.#bats-config_digest_phase2` flake derivation is
> auto-generated from the `# bats file_tags=` directive. The new
> bats file must be `git add`-ed before nix can see it — `nix build`
> against a dirty tree only includes git-tracked files.

**Step 2: Run the bats suite for the new tag**

```bash
git add zz-tests_bats/config_digest_phase2.bats
just test-bats-tags config_digest_phase2
```

Expected: 3 of 3 pass.

**Step 3: Commit**

```bash
git commit -m "zz-tests_bats: end-to-end Phase 2 digest-bearing id tests

Three cases: matching digest resolves; mismatching digest surfaces
markl.ErrNotEqual; digest-against-legacy surfaces the typed error
pointing at \`madder config-pin_digest\`.

Refs: #194, #198.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 6: Documentation pass

**Files:**
- Modify: `docs/man.7/blob-store.md`

**Step 1: Document the digest suffix in blob-store(7)**

Extend the existing `# CONFIG TAMPER DETECTION` section (added in
Phase 1, commit `9a1a487`) with a subsection covering Phase 2:

```markdown
## ID DIGEST SUFFIX (Phase 2)

Blob-store-ids accept an optional `@<markl-id>` suffix that pins the
expected on-disk config digest:

    default@blake2b256-9ft3m74l...
    .archive@blake2b256-7q3w5h2x...

Resolution succeeds only when the suffix matches the store's
on-disk `@` digest line. A digest supplied against a legacy
(un-migrated) config is a hard error — run
**madder config-pin_digest** to migrate first.

Filenames containing literal `@` characters must be disambiguated
by an explicit `./` prefix, the same convention used today for
store IDs that look like filenames.
```

**Step 2: Commit**

```bash
git add docs/man.7/blob-store.md
git commit -m "docs: document Phase 2 id digest suffix in blob-store(7)

Refs: #194, #198.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

> NOTE: FDR-0008 stays at `status: experimental`. Promotion to
> `accepted` requires Phase 2 plus at least one persisted-reference
> call site (e.g. cutting-garden capture receipts) opting into
> emitting the digest-bearing form. Neither is in scope here; the
> promotion happens in a separate PR after that future call site
> lands.

---

## Out of scope (future work after Phase 2)

Not in this plan, called out so a fresh session doesn't expand
scope:

- **Persisting digest-bearing IDs.** Phase 2 governs how
  `Id.Canonical()` renders when the in-memory value happens to
  carry a digest. It does **not** sweep existing call sites
  (receipts, inventory archives, configs) to start emitting the
  digest-bearing form. Each future call site opts in at the call
  site, in its own PR.
- **Absolute-path location type.** The `.foo` vs `..foo` cwdDepth
  ambiguity has its own pending RFC; not addressed here.
- **Promotion to `accepted`.** Per FDR-0008's `promotion-criteria`,
  accepted status requires Phase 2 to ship **and** at least one
  persisted-reference call site (e.g. cutting-garden capture
  receipts) opts into emitting the digest-bearing form. That call
  site is not in this plan.
