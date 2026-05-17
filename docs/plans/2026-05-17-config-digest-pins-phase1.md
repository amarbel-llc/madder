# Config Digest Pins — Phase 1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use eng:subagent-driven-development
> to implement this plan task-by-task.

**Goal:** Every `blob_store-config` carries an `@ <markl-id>` line in
its hyphence metadata; read paths verify it; legacy configs are
trusted silently; a migration command and a `madder list` discovery
surface close the UX loop.

**Architecture:** Hyphence's `TypedMetadataCoder` already writes the
`@ <markl-id>` line when `TypedBlob.BlobDigest` is non-null
(`go/internal/charlie/hyphence/coder_metadata.go:56-67`) and parses
it on read (`coder_metadata.go:26`). All blob_store-config writes
funnel through a single `Coder.EncodeTo` call in
`Init.InitBlobStore` (`go/internal/golf/command_components/init.go:55-66`).
The body-bytes-only digest is computed by tee'ing the encoded body
into a `blake2b256` hasher around the existing
`blob_store_configs.Coder.EncodeTo` / `.DecodeFrom` calls. A new
purpose `madder-blob_store-config-digest-v1` is added to
`go/internal/bravo/markl/purposes.go` + `markl_registrations/main.go`.

**Tech Stack:** Go, hyphence typed-blob coder, `markl.FormatHash` /
`markl.AssertEqual`, futility CLI framework, bats integration tests.

**Rollback:** Each task is one revert. The digest field on
`TypedBlob` is already zero-value-safe. The read-side `AssertEqual`
is the only new failure mode in production code; revert that single
call to restore prior behavior. `@` lines left in configs on disk
are harmless to a reverted binary (hyphence metadata coder already
keys on `@` for `BlobDigest` and the field is unused if nothing
re-hashes).

**FDR:** [`docs/features/0008-config-digest-pins.md`](../features/0008-config-digest-pins.md)

---

## Task 1: Register the `madder-blob_store-config-digest-v1` purpose

**Promotion criteria:** N/A (purely additive vocabulary).

**Files:**
- Modify: `go/internal/bravo/markl/purposes.go` (add constant)
- Modify: `go/internal/charlie/markl_registrations/main.go`
  (add `PurposeBlobStoreConfigDigestV1Opts` + append to `AllPurposes`)
- Test: `go/internal/charlie/markl_registrations/registrations_test.go`

**Step 1: Write the failing test**

Append to `go/internal/charlie/markl_registrations/registrations_test.go`:

```go
func TestBlobStoreConfigDigestV1Registered(t *testing.T) {
	purpose := markl.GetPurpose(markl.PurposeBlobStoreConfigDigestV1)
	if purpose.GetPurposeType() != markl.PurposeTypeBlobDigest {
		t.Fatalf(
			"expected PurposeTypeBlobDigest, got %v",
			purpose.GetPurposeType(),
		)
	}
}
```

**Step 2: Run test to verify it fails**

```
just go-test ./go/internal/charlie/markl_registrations/...
```

Expected: undefined `markl.PurposeBlobStoreConfigDigestV1` compile
error. If the helper isn't called `go-test`, fall back to
`go test ./go/internal/charlie/markl_registrations/...` from the
`go/` directory.

**Step 3: Add the constant**

In `go/internal/bravo/markl/purposes.go`, in the existing const block,
add a new section after the `// Blob Digests` block:

```go
	// Blob-Store-Config Digests
	PurposeBlobStoreConfigDigestV1 = "madder-blob_store-config-digest-v1"
```

**Step 4: Add the registration**

In `go/internal/charlie/markl_registrations/main.go`, add the opts
var alongside the others:

```go
	PurposeBlobStoreConfigDigestV1Opts = markl.RegisterPurposeOpts{
		Id:   markl.PurposeBlobStoreConfigDigestV1,
		Type: markl.PurposeTypeBlobDigest,
		FormatIds: []string{
			markl.FormatIdHashBlake2b256,
		},
	}
```

Then append to `AllPurposes`:

```go
	PurposeBlobStoreConfigDigestV1Opts,
```

**Step 5: Run test to verify it passes**

```
go test ./go/internal/charlie/markl_registrations/...
```

Expected: PASS.

**Step 6: Commit**

```bash
git add go/internal/bravo/markl/purposes.go \
        go/internal/charlie/markl_registrations/main.go \
        go/internal/charlie/markl_registrations/registrations_test.go
git commit -m "markl: register madder-blob_store-config-digest-v1 purpose

Phase 1 of FDR-0008. Refs: #194.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 2: Helper — compute body digest while encoding

The hyphence `Coder.EncodeTo` takes a `*TypedBlob[Config]` whose
`BlobDigest` is read by `TypedMetadataCoder.EncodeTo` for the `@`
line. We need to compute the body digest **before** the metadata
section is written. The cleanest seam is a thin wrapper that
encodes once to a discard-and-hash sink (to compute the digest),
then re-encodes for real with `BlobDigest` populated.

This task adds that helper and proves the round-trip on its own
before any callers switch over.

**Promotion criteria:** Old `Coder.EncodeTo` direct usage in
`Init.InitBlobStore` is replaced by the new helper in Task 4. The
helper itself is the only sanctioned write path after Task 4 lands.

**Files:**
- Create: `go/internal/delta/blob_store_configs/digest.go`
- Test: `go/internal/delta/blob_store_configs/digest_test.go`

**Step 1: Write the failing test**

Create `go/internal/delta/blob_store_configs/digest_test.go`:

```go
package blob_store_configs

import (
	"bytes"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	_ "github.com/amarbel-llc/madder/go/internal/charlie/markl_registrations"
)

// TestEncodeWithDigestRoundTrip: encoding a config through
// EncodeWithDigest produces output whose @ line carries the
// blake2b256 digest of the body bytes.
func TestEncodeWithDigestRoundTrip(t *testing.T) {
	cfg := defaultLocalHashBucketedConfigForTest(t)
	typedConfig := &TypedConfig{
		Type: hyphence.MakeType("toml-blob_store_config-v3"),
		Blob: cfg,
	}

	var buf bytes.Buffer
	if _, err := EncodeWithDigest(typedConfig, &buf); err != nil {
		t.Fatalf("EncodeWithDigest: %v", err)
	}

	// Decode and confirm the @ line round-trips.
	decoded := &TypedConfig{}
	if _, err := Coder.DecodeFrom(decoded, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("Coder.DecodeFrom: %v", err)
	}

	if decoded.BlobDigest.IsNull() {
		t.Fatal("expected non-null BlobDigest after round-trip")
	}

	if decoded.BlobDigest.GetMarklFormat().GetMarklFormatId() != markl.FormatIdHashBlake2b256 {
		t.Fatalf(
			"expected blake2b256 digest, got %v",
			decoded.BlobDigest.GetMarklFormat().GetMarklFormatId(),
		)
	}
}

// defaultLocalHashBucketedConfigForTest returns a minimally-valid
// TomlLocalHashBucketedV3 config suitable for round-trip tests. If
// blob_store_configs already exposes a helper for this, prefer that
// over hand-rolling; otherwise hand-roll the simplest valid shape.
func defaultLocalHashBucketedConfigForTest(t *testing.T) Config {
	t.Helper()
	// TODO: replace with the package's preferred test-config builder
	// if one exists; otherwise construct a TomlV3 with sane defaults.
	doc, err := charlie_bsc.DecodeTomlV3(nil)
	if err != nil {
		t.Fatalf("DecodeTomlV3(nil): %v", err)
	}
	return doc.Data()
}
```

(If `charlie_bsc` isn't imported in this package, add it as
`charlie_bsc "github.com/amarbel-llc/madder/go/internal/charlie/blob_store_configs"`.
If the package already exposes a fixture helper, use that and delete
the local helper.)

**Step 2: Run test to verify it fails**

```
go test ./go/internal/delta/blob_store_configs/...
```

Expected: `EncodeWithDigest` undefined.

**Step 3: Implement `EncodeWithDigest`**

Create `go/internal/delta/blob_store_configs/digest.go`:

```go
package blob_store_configs

import (
	"bytes"
	"io"

	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// DigestPurpose is the markl purpose stamped on the @ line of every
// migrated blob_store-config.
const DigestPurpose = markl.PurposeBlobStoreConfigDigestV1

// DigestHash is the hash family used to compute the body digest.
// Phase 1 hard-codes blake2b256; revisit if a sha256-only store
// ever needs self-check.
var DigestHash markl.FormatHash = markl.FormatHashBlake2b256

// EncodeWithDigest renders typedConfig to w with a populated
// BlobDigest covering the body bytes. It is the only sanctioned
// write path for blob_store-config files after FDR-0008 Phase 1.
//
// Mechanism: render the body to a scratch buffer via Coder.EncodeTo,
// hash the *body* portion (the bytes that the inner Blob coder
// emits, not the metadata wrap), stamp typedConfig.BlobDigest with
// the resulting markl-id, then re-render to w. The double-encode
// keeps the hash input well-defined (body bytes only) and avoids
// reworking the hyphence coder's metadata-first emission order.
func EncodeWithDigest(
	typedConfig *TypedConfig,
	w io.Writer,
) (n int64, err error) {
	// Pass 1: render body bytes only, no metadata wrap, no hash on the
	// metadata section. Coder.Blob is the inner CoderTypeMap that
	// emits the toml-encoded payload; it does not write metadata.
	var bodyBuf bytes.Buffer
	if Coder.Blob == nil {
		err = errors.ErrorWithStackf("Coder.Blob is nil")
		return n, err
	}

	bw := newBufWriter(&bodyBuf)
	if _, err = Coder.Blob.EncodeTo(&typedConfig.Blob, bw); err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	if err = bw.Flush(); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	// Hash the body bytes under the configured DigestHash.
	digest, err := DigestHash.GetMarklIdForBytes(bodyBuf.Bytes())
	if err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	if err = digest.SetPurpose(DigestPurpose); err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	typedConfig.BlobDigest = digest

	// Pass 2: render through the full coder so the @ line and the
	// metadata header are emitted around the same body bytes.
	if n, err = Coder.EncodeTo(typedConfig, w); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	return n, err
}
```

You'll need a small `newBufWriter` shim if the inner coder takes a
`*bufio.Writer`. Check the signature on
`hyphence.CoderTypeMapWithoutType.EncodeTo` — if it accepts a plain
`io.Writer`, drop the bufWriter dance.

> NOTE: `EncodeTo` on the inner coder takes a `*bufio.Writer` (see
> `hyphence.CoderTypeMap`). If `Coder.Blob.EncodeTo`'s second arg is
> `*bufio.Writer`, build one with `bufio.NewWriter(&bodyBuf)` and
> flush before measuring.

**Step 4: Run test to verify it passes**

```
go test ./go/internal/delta/blob_store_configs/...
```

Expected: PASS.

**Step 5: Add a tamper test**

Append to `digest_test.go`:

```go
// TestEncodeWithDigestDetectsTamper: mutate one byte of an
// encoded config; the next Coder.DecodeFrom must surface the
// mismatch. (The mismatch detection itself is wired in Task 3;
// this test will gain teeth once Task 3 lands.)
func TestEncodeWithDigestDetectsTamper(t *testing.T) {
	cfg := defaultLocalHashBucketedConfigForTest(t)
	typedConfig := &TypedConfig{
		Type: hyphence.MakeType("toml-blob_store_config-v3"),
		Blob: cfg,
	}

	var buf bytes.Buffer
	if _, err := EncodeWithDigest(typedConfig, &buf); err != nil {
		t.Fatalf("EncodeWithDigest: %v", err)
	}

	// Find the body (after the second hyphence Boundary + blank line)
	// and flip one byte. The exact offset is implementation-coupled;
	// use bytes.LastIndex on the boundary marker.
	bs := buf.Bytes()
	boundary := []byte("\n---\n\n")
	idx := bytes.LastIndex(bs, boundary)
	if idx < 0 {
		t.Fatalf("could not locate body start in encoded output")
	}
	bodyStart := idx + len(boundary)
	if bodyStart >= len(bs) {
		t.Fatalf("body is empty in encoded output")
	}
	bs[bodyStart] ^= 0x01

	// Task 3 will wire the assertion; for now we only document the
	// expected post-Task-3 behavior. After Task 3 lands, change this
	// to assert a markl.ErrNotEqual.
	decoded := &TypedConfig{}
	_, _ = Coder.DecodeFrom(decoded, bytes.NewReader(bs))
	// Intentionally no assertion: Task 3 adds the failure mode.
}
```

**Step 6: Commit**

```bash
git add go/internal/delta/blob_store_configs/digest.go \
        go/internal/delta/blob_store_configs/digest_test.go
git commit -m "blob_store_configs: add EncodeWithDigest helper

Phase 1 of FDR-0008. EncodeWithDigest renders a config with its
BlobDigest populated (body-bytes digest under blake2b256, stamped
with madder-blob_store-config-digest-v1). The full read-side
assertion lands in the next task. Refs: #194.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 3: Read-side self-check — verify @ line on decode

**Promotion criteria:** N/A — once this lands, the FDR's Phase 1
read-path semantics are live.

**Files:**
- Create: `go/internal/delta/blob_store_configs/verify.go`
- Modify: `go/internal/delta/blob_store_configs/digest_test.go`
  (upgrade the Task 2 tamper test to assert `markl.ErrNotEqual`)

The hyphence `Coder.DecodeFrom` reads metadata first (populating
`BlobDigest`), then the body. We need to capture the body bytes
during decode to re-hash them. The simplest way: wrap the
underlying reader so a hasher tees the post-boundary bytes.

**Step 1: Write the failing test**

Upgrade the tamper test in `digest_test.go`:

```go
func TestEncodeWithDigestDetectsTamper(t *testing.T) {
	cfg := defaultLocalHashBucketedConfigForTest(t)
	typedConfig := &TypedConfig{
		Type: hyphence.MakeType("toml-blob_store_config-v3"),
		Blob: cfg,
	}

	var buf bytes.Buffer
	if _, err := EncodeWithDigest(typedConfig, &buf); err != nil {
		t.Fatalf("EncodeWithDigest: %v", err)
	}

	bs := buf.Bytes()
	boundary := []byte("\n---\n\n")
	idx := bytes.LastIndex(bs, boundary)
	if idx < 0 {
		t.Fatalf("could not locate body start in encoded output")
	}
	bodyStart := idx + len(boundary)
	bs[bodyStart] ^= 0x01

	decoded := &TypedConfig{}
	_, err := DecodeAndVerify(decoded, bytes.NewReader(bs))
	if err == nil {
		t.Fatal("expected mismatch error, got nil")
	}
	var notEqual markl.ErrNotEqual
	if !errors.As(err, &notEqual) {
		t.Fatalf("expected markl.ErrNotEqual, got %T: %v", err, err)
	}
	if notEqual.Expected.IsNull() || notEqual.Actual.IsNull() {
		t.Fatal("expected both Expected and Actual to be populated")
	}
}

// TestDecodeAndVerifyAcceptsLegacy: a config with no @ line
// (pre-FDR-0008) is trusted silently.
func TestDecodeAndVerifyAcceptsLegacy(t *testing.T) {
	cfg := defaultLocalHashBucketedConfigForTest(t)
	typedConfig := &TypedConfig{
		Type: hyphence.MakeType("toml-blob_store_config-v3"),
		Blob: cfg,
	}

	// Encode via the legacy coder (no @ line).
	var buf bytes.Buffer
	if _, err := Coder.EncodeTo(typedConfig, &buf); err != nil {
		t.Fatalf("Coder.EncodeTo: %v", err)
	}

	decoded := &TypedConfig{}
	if _, err := DecodeAndVerify(decoded, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("DecodeAndVerify on legacy config: %v", err)
	}
	if !decoded.BlobDigest.IsNull() {
		t.Fatal("legacy config should not have BlobDigest populated")
	}
}

// TestDecodeAndVerifyRoundTrip: encode via EncodeWithDigest, decode
// via DecodeAndVerify, no error, BlobDigest populated.
func TestDecodeAndVerifyRoundTrip(t *testing.T) {
	cfg := defaultLocalHashBucketedConfigForTest(t)
	typedConfig := &TypedConfig{
		Type: hyphence.MakeType("toml-blob_store_config-v3"),
		Blob: cfg,
	}

	var buf bytes.Buffer
	if _, err := EncodeWithDigest(typedConfig, &buf); err != nil {
		t.Fatalf("EncodeWithDigest: %v", err)
	}

	decoded := &TypedConfig{}
	if _, err := DecodeAndVerify(decoded, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("DecodeAndVerify: %v", err)
	}
	if decoded.BlobDigest.IsNull() {
		t.Fatal("BlobDigest should be populated after round-trip")
	}
}
```

Add `"errors"` to the test file's imports (the stdlib one, for
`errors.As`).

**Step 2: Run tests to verify they fail**

```
go test ./go/internal/delta/blob_store_configs/...
```

Expected: `DecodeAndVerify` undefined.

**Step 3: Implement `DecodeAndVerify`**

Create `go/internal/delta/blob_store_configs/verify.go`:

```go
package blob_store_configs

import (
	"bytes"
	"io"

	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// DecodeAndVerify decodes a blob_store-config from r and, if its
// metadata carries a BlobDigest, re-hashes the body bytes and
// asserts the digests match. A config with no @ line is trusted
// silently (pre-FDR-0008 back-compat). A mismatch returns
// markl.ErrNotEqual carrying both digests.
//
// Implementation: buffer the whole input, run Coder.DecodeFrom on
// the buffered bytes (which populates BlobDigest from the metadata),
// then re-encode the decoded *body* via the inner Blob coder and
// hash that. Buffering the whole config is acceptable because
// blob_store-config files are bounded in size (KB-scale, not MB).
func DecodeAndVerify(
	typedConfig *TypedConfig,
	r io.Reader,
) (n int64, err error) {
	all, err := io.ReadAll(r)
	if err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	n = int64(len(all))

	if _, err = Coder.DecodeFrom(typedConfig, bytes.NewReader(all)); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	if typedConfig.BlobDigest.IsNull() {
		// Legacy config: no @ line, no assertion.
		return n, err
	}

	// Re-encode the body to obtain the canonical byte sequence the
	// digest covers, then re-hash.
	var bodyBuf bytes.Buffer
	bw := bufio.NewWriter(&bodyBuf)
	if _, err = Coder.Blob.EncodeTo(&typedConfig.Blob, bw); err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	if err = bw.Flush(); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	computed, err := DigestHash.GetMarklIdForBytes(bodyBuf.Bytes())
	if err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	if err = computed.SetPurpose(DigestPurpose); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	if err = markl.AssertEqual(typedConfig.BlobDigest, computed); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	return n, err
}
```

Add `"bufio"` to the imports.

> NOTE: The "re-encode the body to obtain the canonical byte
> sequence" approach assumes the inner Blob coder is **deterministic**
> for a given Config value. Verify against
> `go/internal/charlie/blob_store_configs/toml_*` encoders before
> relying on this. If any encoder emits non-deterministic output
> (map ordering, timestamp drift), this approach needs the
> alternative: tee the raw post-boundary bytes through a hasher
> during the original decode pass. The alternative is a hyphence
> coder change, not a blob_store_configs change, so deal with it as
> a sub-task here if necessary.

**Step 4: Run tests to verify they pass**

```
go test ./go/internal/delta/blob_store_configs/...
```

Expected: PASS.

**Step 5: Run the wider package suite for regressions**

```
go test ./go/internal/delta/... ./go/internal/charlie/...
```

Expected: PASS. If anything fails, investigate — Task 3 should be
read-side-only and additive.

**Step 6: Commit**

```bash
git add go/internal/delta/blob_store_configs/verify.go \
        go/internal/delta/blob_store_configs/digest_test.go
git commit -m "blob_store_configs: add DecodeAndVerify (read-side self-check)

Phase 1 of FDR-0008. DecodeAndVerify re-hashes the body bytes of a
decoded config and asserts equality with the @ line's digest via
markl.AssertEqual. Legacy configs (no @ line) are trusted silently.

Refs: #194.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 4: Switch `Init.InitBlobStore` to `EncodeWithDigest`

Every `init-*` command funnels through
`Init.InitBlobStore` (`go/internal/golf/command_components/init.go:17-69`).
Swap the single `Coder.EncodeTo` call for `EncodeWithDigest`.

**Promotion criteria:** Every `init-*` bats test passes and emits a
config containing an `@` line. After this lands, no madder write
path emits a digestless config.

**Files:**
- Modify: `go/internal/golf/command_components/init.go:55-66`
- Test: `zz-tests_bats/init.bats` (new test)

**Step 1: Write the failing bats test**

Append to `zz-tests_bats/init.bats`:

```bash
function init_default_config_has_digest_line { # @test
  # FDR-0008 Phase 1: every fresh config carries an @ line in its
  # hyphence metadata header with a blake2b256 digest under the
  # madder-blob_store-config-digest-v1 purpose.
  init_store

  local config=".madder/local/share/blob_stores/default/blob_store-config"
  [[ -f $config ]] || fail "expected config at $config"

  run grep -E '^@ madder-blob_store-config-digest-v1@blake2b256-' "$config"
  assert_success
}

function init_inventory_archive_config_has_digest_line { # @test
  # Same as above for the inventory-archive init path.
  init_store
  run_madder init-inventory-archive -encryption none .archive
  assert_success

  local config=".madder/local/share/blob_stores/archive/blob_store-config"
  # Path may vary; adjust to whatever init-inventory-archive actually
  # writes. If the prefix-form needs translation, use the bats helper.
  [[ -f $config ]] || fail "expected config at $config"

  run grep -E '^@ madder-blob_store-config-digest-v1@blake2b256-' "$config"
  assert_success
}
```

Use whatever recipe runs the bats suite. From the justfile, the
likely commands are one of `just test-bats`, `just bats`, or
`just go-bats-tests`. List them first:

```
just --list | grep -iE 'bats|test'
```

Then run the new tests only:

```
just <bats-recipe-name> init
```

**Step 2: Run tests to verify they fail**

The new tests should fail because the config does not yet contain
an `@` line. If the migration command line in the test path is
wrong, fix the path first (the test failure should be "no such
file" then "no match", not "no such command").

**Step 3: Make the swap**

In `go/internal/golf/command_components/init.go`, change line 60:

```go
				_, err := blob_store_configs.Coder.EncodeTo(config, w)
				return err
```

to:

```go
				_, err := blob_store_configs.EncodeWithDigest(config, w)
				return err
```

**Step 4: Run the bats tests again**

```
just <bats-recipe-name> init
```

Expected: PASS, including the new digest-line tests.

**Step 5: Run the full bats init suite**

```
just <bats-recipe-name>
```

Expected: PASS for everything. The `init_default_config_is_read_only`
test at `init.bats:12-22` still passes — mode bits are unchanged.

**Step 6: Commit**

```bash
git add go/internal/golf/command_components/init.go \
        zz-tests_bats/init.bats
git commit -m "init: emit @ digest line in fresh blob_store-configs

Phase 1 of FDR-0008. Init.InitBlobStore is the single write seam for
every init-* command; switching its blob_store_configs.Coder.EncodeTo
call to EncodeWithDigest causes every fresh config to carry a body-
bytes digest under madder-blob_store-config-digest-v1.

Refs: #194.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 5: Wire `DecodeAndVerify` into every config read path

The decode call sites today are scattered across the foxtrot blob
stores and golf command_components. Each call to
`blob_store_configs.Coder.DecodeFrom` becomes a call to
`DecodeAndVerify`.

**Promotion criteria:** Once this lands, every blob_store-config
read is tamper-detected. The old `Coder.DecodeFrom` should appear
only inside `DecodeAndVerify` itself and inside tests.

**Files:**
- Modify: every file in the grep output below.
- Test: `go test ./...` and the full bats suite.

**Step 1: Inventory the call sites**

```
rg -n 'blob_store_configs\.Coder\.DecodeFrom|bsc\.Coder\.DecodeFrom' go/
```

Expected hits (from earlier exploration): files in
`go/internal/foxtrot/blob_stores/`,
`go/internal/golf/command_components/`,
`go/internal/india/commands/`, and any cutting-garden command.
Capture the full list before editing.

**Step 2: Add a placeholder failing test**

Create `zz-tests_bats/config_digest_verify.bats`:

```bash
setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=config_digest

function decode_detects_tampered_config { # @test
  # FDR-0008 Phase 1: any madder command that reads a
  # blob_store-config must refuse a tampered one.
  init_store

  local config=".madder/local/share/blob_stores/default/blob_store-config"
  [[ -f $config ]] || fail "expected config at $config"

  # The config is 0444. Re-chmod, mutate, re-chmod back.
  chmod 0644 "$config"
  # Flip one byte deep in the body. The exact offset is brittle;
  # appending whitespace is enough to invalidate the body digest
  # while keeping the toml parseable.
  printf '\n# tamper\n' >> "$config"
  chmod 0444 "$config"

  run_madder list
  assert_failure
  assert_output --partial 'expected'
  assert_output --partial 'actual'
}
```

**Step 3: Run the bats test to verify it fails (no failure yet)**

```
just <bats-recipe-name> config_digest
```

Expected: FAIL — `madder list` still succeeds because no read site
calls `DecodeAndVerify` yet.

**Step 4: Switch every call site**

For each file from Step 1, replace
`blob_store_configs.Coder.DecodeFrom(...)` with
`blob_store_configs.DecodeAndVerify(...)`. The signatures are
identical (`*TypedConfig, io.Reader`).

Some call sites may pass a `bufio.Reader` or other typed reader —
`DecodeAndVerify` takes `io.Reader`, so the type checks should hold.

**Step 5: Run the full Go test suite**

```
go test ./...
```

Expected: PASS. Anything that fails is either:
- A test that hand-constructs a config without `EncodeWithDigest` —
  acceptable, those configs go through the legacy code-path
  (no `@` line, trusted silently).
- A test that pre-populates a tampered fixture — change the test
  to either use `EncodeWithDigest` or expect the new error.

**Step 6: Run the full bats suite**

```
just <bats-recipe-name>
```

Expected: PASS, including `decode_detects_tampered_config`.

**Step 7: Commit**

```bash
git add -A
git commit -m "blob_store_configs: route every config read through DecodeAndVerify

Phase 1 of FDR-0008. Every site that previously called
blob_store_configs.Coder.DecodeFrom now calls DecodeAndVerify, so
tamper detection fires on every read. Legacy configs (no @ line)
remain trusted silently for back-compat.

Refs: #194.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 6: New CLI command — `madder config-pin_digest`

**Promotion criteria:** Released as the documented migration path
for legacy configs. Removable only if Phase 1 is itself reverted.

**Files:**
- Create: `go/internal/india/commands/config_pin_digest.go`
- Create: `zz-tests_bats/config_pin_digest.bats`
- Modify: `docs/man.1/madder.md` (if it lists subcommands; verify)

**Step 1: Write the failing bats test**

Create `zz-tests_bats/config_pin_digest.bats`:

```bash
setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=config_pin_digest

function config_pin_digest_mints_on_legacy { # @test
  # Construct a legacy config (no @ line), run config-pin_digest,
  # confirm the config now has one.
  init_store

  local config=".madder/local/share/blob_stores/default/blob_store-config"
  chmod 0644 "$config"
  # Strip any @ line to simulate a pre-FDR-0008 config.
  sed -i.bak '/^@ /d' "$config" && rm "$config.bak"
  chmod 0444 "$config"

  run grep -E '^@ madder-blob_store-config-digest-v1@' "$config"
  assert_failure  # confirm legacy shape

  run_madder config-pin_digest default
  assert_success

  run grep -E '^@ madder-blob_store-config-digest-v1@blake2b256-' "$config"
  assert_success
}

function config_pin_digest_idempotent { # @test
  init_store

  run_madder config-pin_digest default
  assert_success

  local config=".madder/local/share/blob_stores/default/blob_store-config"
  local before
  before=$(cat "$config")

  run_madder config-pin_digest default
  assert_success

  local after
  after=$(cat "$config")
  assert_equal "$before" "$after"
}

function config_pin_digest_all { # @test
  init_store
  run_madder init-inventory-archive -encryption none .archive
  assert_success

  # Strip @ lines from both configs.
  for config in \
    .madder/local/share/blob_stores/default/blob_store-config \
    .madder/local/share/blob_stores/archive/blob_store-config
  do
    chmod 0644 "$config"
    sed -i.bak '/^@ /d' "$config" && rm "$config.bak"
    chmod 0444 "$config"
  done

  run_madder config-pin_digest --all
  assert_success

  for config in \
    .madder/local/share/blob_stores/default/blob_store-config \
    .madder/local/share/blob_stores/archive/blob_store-config
  do
    run grep -E '^@ madder-blob_store-config-digest-v1@blake2b256-' "$config"
    assert_success
  done
}

function config_pin_digest_rejects_no_target { # @test
  init_store
  run_madder config-pin_digest
  assert_failure
  assert_output --partial 'specify --all or one or more blob-store-ids'
}

function config_pin_digest_rejects_both_modes { # @test
  init_store
  run_madder config-pin_digest --all default
  assert_failure
  assert_output --partial '--all and explicit ids are mutually exclusive'
}
```

**Step 2: Run the bats tests to verify they fail**

```
just <bats-recipe-name> config_pin_digest
```

Expected: FAIL — command not registered.

**Step 3: Implement the command**

Create `go/internal/india/commands/config_pin_digest.go`:

```go
package commands

import (
	"io"

	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/charlie/files"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

func init() {
	utility.AddCmd("config-pin_digest", &ConfigPinDigest{})
}

// ConfigPinDigest re-emits the named blob_store-config files with
// their @ digest line populated. Idempotent: a config that already
// has a matching @ line is decoded and re-emitted byte-identical.
type ConfigPinDigest struct {
	command_components.EnvBlobStore

	All bool
}

var (
	_ interfaces.CommandComponentWriter = (*ConfigPinDigest)(nil)
	_ futility.CommandWithParams        = (*ConfigPinDigest)(nil)
)

func (cmd *ConfigPinDigest) GetParams() []futility.Param {
	return []futility.Param{
		{
			Name:        "blob-store-id",
			Description: "blob-store-id(s) to migrate; omit when --all is set",
			Repeats:     true,
		},
	}
}

func (cmd ConfigPinDigest) GetDescription() futility.Description {
	return futility.Description{
		Short: "mint or refresh the @ digest line on blob_store-config files",
		Long: "Re-emits the named blob_store-config files with their " +
			"@ digest line populated. Idempotent: a config that already " +
			"carries a matching digest is re-emitted byte-identical. " +
			"Required reading: docs/features/0008-config-digest-pins.md.",
	}
}

func (cmd *ConfigPinDigest) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.BoolVar(&cmd.All, "all", false,
		"migrate every blob_store-config under the active XDG roots")
}

func (cmd ConfigPinDigest) Run(req futility.Request) {
	args := req.GetPositionalArgs()

	if cmd.All && len(args) > 0 {
		req.Cancel(errors.Errorf(
			"--all and explicit ids are mutually exclusive",
		))
		return
	}
	if !cmd.All && len(args) == 0 {
		req.Cancel(errors.Errorf(
			"specify --all or one or more blob-store-ids",
		))
		return
	}

	envBlobStore := cmd.MakeEnvBlobStore(req)
	blobStoreMap := envBlobStore.GetBlobStores()

	var targets []blob_stores.BlobStoreInitialized
	if cmd.All {
		for _, bs := range blobStoreMap {
			targets = append(targets, bs)
		}
	} else {
		for _, arg := range args {
			var id blob_store_id.Id
			if err := id.Set(arg); err != nil {
				req.Cancel(errors.Wrapf(err,
					"invalid blob-store-id: %q", arg))
				return
			}
			bs, ok := blobStoreMap[id]
			if !ok {
				req.Cancel(errors.Errorf(
					"no such blob store: %q", arg))
				return
			}
			targets = append(targets, bs)
		}
	}

	for _, target := range targets {
		if err := cmd.migrate(target); err != nil {
			req.Cancel(errors.Wrapf(err,
				"failed to migrate %q",
				target.Path.GetId(),
			))
			return
		}
	}
}

func (cmd ConfigPinDigest) migrate(
	target blob_stores.BlobStoreInitialized,
) error {
	configPath := target.Path.GetConfig()
	typedConfig := &blob_store_configs.TypedConfig{}

	f, err := files.OpenReadOnly(configPath)
	if err != nil {
		return errors.Wrap(err)
	}
	if _, err = blob_store_configs.DecodeAndVerify(typedConfig, f); err != nil {
		_ = f.Close()
		return errors.Wrap(err)
	}
	if err = f.Close(); err != nil {
		return errors.Wrap(err)
	}

	// Clear BlobDigest so EncodeWithDigest recomputes (covers the
	// case where the input was legacy and BlobDigest was null).
	typedConfig.BlobDigest = markl.Id{}

	return files.WriteImmutable(configPath, func(w io.Writer) error {
		_, err := blob_store_configs.EncodeWithDigest(typedConfig, w)
		return err
	})
}
```

> NOTE: The exact `files.OpenReadOnly` / `files.WriteImmutable` API
> calls may differ — confirm by reading `go/internal/charlie/files/`
> and matching the helpers used in `Init.InitBlobStore`. Also verify
> the `BlobStoreMap` key type before indexing with the parsed `Id`;
> if the map is keyed by `string` (canonical form), use
> `id.Canonical()` as the key.

**Step 4: Run the bats tests**

```
just <bats-recipe-name> config_pin_digest
```

Expected: PASS.

**Step 5: Run the full bats suite for regressions**

```
just <bats-recipe-name>
```

Expected: PASS.

**Step 6: Commit**

```bash
git add go/internal/india/commands/config_pin_digest.go \
        zz-tests_bats/config_pin_digest.bats
git commit -m "commands: add 'madder config-pin_digest' migration command

Phase 1 of FDR-0008. Mints or refreshes the @ digest line on
blob_store-config files. Accepts one or more blob-store-ids or
--all (mutually exclusive). Idempotent. Refs: #194.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 7: `madder list` — show digest in text/ndjson/json, flag legacy

**Promotion criteria:** Released as the documented discovery path
for the upgrade. Removable only if Phase 1 itself is reverted.

**Files:**
- Modify: `go/internal/india/commands/list.go:60-141`
- Test: `zz-tests_bats/list*.bats` (if a file exists; otherwise
  add cases to a new `list.bats`)

**Step 1: Write the failing bats test**

Check whether `zz-tests_bats/list.bats` exists:

```
ls zz-tests_bats/list*.bats 2>/dev/null
```

If it doesn't, create one:

```bash
setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=list

function list_shows_digest_for_migrated { # @test
  init_store
  run_madder list
  assert_success
  assert_output --regexp 'default@blake2b256-'
}

function list_flags_unmigrated_with_footer { # @test
  init_store
  local config=".madder/local/share/blob_stores/default/blob_store-config"
  chmod 0644 "$config"
  sed -i.bak '/^@ /d' "$config" && rm "$config.bak"
  chmod 0444 "$config"

  run_madder list
  assert_success
  assert_output --partial '(unmigrated)'
  assert_output --partial 'madder config-pin_digest default'
}

function list_no_footer_when_all_migrated { # @test
  init_store
  run_madder list
  assert_success
  refute_output --partial 'config-pin_digest'
  refute_output --partial '(unmigrated)'
}

function list_ndjson_includes_digest { # @test
  init_store
  run_madder list -format=ndjson
  assert_success
  assert_output --partial '"digest":"madder-blob_store-config-digest-v1@blake2b256-'
}

function list_ndjson_flags_legacy { # @test
  init_store
  local config=".madder/local/share/blob_stores/default/blob_store-config"
  chmod 0644 "$config"
  sed -i.bak '/^@ /d' "$config" && rm "$config.bak"
  chmod 0444 "$config"

  run_madder list -format=ndjson
  assert_success
  assert_output --partial '"digest_missing":true'
}
```

**Step 2: Run the tests to verify they fail**

```
just <bats-recipe-name> list
```

Expected: FAIL — `madder list` does not yet emit digests or
footers.

**Step 3: Modify `emitListText`, `emitListNDJSON`, `makeListRecord`**

In `go/internal/india/commands/list.go`:

```go
type listRecord struct {
	Id             string `json:"id"`
	Description    string `json:"description"`
	ConfigPath     string `json:"config_path"`
	Base           string `json:"base"`
	Digest         string `json:"digest,omitempty"`
	DigestMissing  bool   `json:"digest_missing,omitempty"`
}

func makeListRecord(blobStore blob_stores.BlobStoreInitialized) listRecord {
	rec := listRecord{
		Id:          blobStore.Path.GetId().String(),
		Description: blobStore.GetBlobStoreDescription(),
		ConfigPath:  blobStore.Path.GetConfig(),
		Base:        blobStore.Path.GetBase(),
	}
	if d := blobStoreConfigDigest(blobStore); !d.IsNull() {
		rec.Digest = d.String()
	} else {
		rec.DigestMissing = true
	}
	return rec
}

// blobStoreConfigDigest extracts the digest from an initialized
// blob store's already-decoded config. The BlobStoreInitialized
// struct should already hold the TypedConfig from discovery; if
// not, decode it on demand via DecodeAndVerify.
func blobStoreConfigDigest(
	blobStore blob_stores.BlobStoreInitialized,
) markl.Id {
	// TODO: thread the decoded TypedConfig through
	// BlobStoreInitialized so this lookup is O(1). For now,
	// re-read the config from disk.
	f, err := files.OpenReadOnly(blobStore.Path.GetConfig())
	if err != nil {
		return markl.Id{}
	}
	defer f.Close()
	var typedConfig blob_store_configs.TypedConfig
	if _, err := blob_store_configs.DecodeAndVerify(&typedConfig, f); err != nil {
		return markl.Id{}
	}
	return typedConfig.BlobDigest
}

func emitListText(
	envBlobStore command_components.BlobStoreEnv,
	blobStores blob_stores.BlobStoreMap,
) {
	var unmigrated []string
	for _, blobStore := range stableOrder(blobStores) {
		idStr := blobStore.Path.GetId().String()
		digest := blobStoreConfigDigest(blobStore)
		if !digest.IsNull() {
			idStr = idStr + "@" + digest.String()
		} else {
			idStr = idStr + " (unmigrated)"
			unmigrated = append(unmigrated, blobStore.Path.GetId().String())
		}
		envBlobStore.GetUI().Printf(
			"%s: %s # path: %s",
			idStr,
			blobStore.GetBlobStoreDescription(),
			envBlobStore.RelToCwdOrSame(blobStore.Path.GetConfig()),
		)
	}
	if len(unmigrated) > 0 {
		envBlobStore.GetUI().Printf("")
		envBlobStore.GetUI().Printf(
			"NOTE: %d store(s) above are missing tamper-detection digests.",
			len(unmigrated),
		)
		envBlobStore.GetUI().Printf("      Run this to migrate them:")
		envBlobStore.GetUI().Printf("")
		envBlobStore.GetUI().Printf(
			"        madder config-pin_digest %s",
			strings.Join(unmigrated, " "),
		)
	}
}
```

Add `"strings"`, `"github.com/amarbel-llc/madder/go/internal/bravo/markl"`,
`"github.com/amarbel-llc/madder/go/internal/charlie/files"`,
`"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"`
to the imports.

> NOTE: The digest-extraction via re-reading is wasteful. A
> follow-up should thread `TypedConfig` through
> `BlobStoreInitialized` so the field is available without an extra
> disk read. Captured as #TODO in the FDR's Future Work.

**Step 4: Run the list tests**

```
just <bats-recipe-name> list
```

Expected: PASS for all five tests.

**Step 5: Run the full bats suite**

```
just <bats-recipe-name>
```

Expected: PASS.

**Step 6: Commit**

```bash
git add go/internal/india/commands/list.go \
        zz-tests_bats/list.bats
git commit -m "list: show config digest, flag legacy, emit migration footer

Phase 1 of FDR-0008. `madder list` now shows the digest-bearing
ID form for migrated stores, marks legacy stores with
(unmigrated), and prints a copy-pasteable
'madder config-pin_digest …' footer listing exactly the unmigrated
store IDs when any are present. The ndjson/json modes gain
`digest` and `digest_missing` fields per FDR-0008.

Refs: #194, #174, #175.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 8: Documentation pass

**Files:**
- Modify: `docs/man.7/blob-store.md`
- Modify: `docs/man.1/madder.md` (if it lists subcommands)
- Modify: `docs/features/0008-config-digest-pins.md` (status →
  `experimental`)

**Step 1: Update `blob-store(7)`**

In `docs/man.7/blob-store.md`, add a new section before
`# CONCURRENCY AND DURABILITY`:

```markdown
# CONFIG TAMPER DETECTION

Every blob_store-config carries an `@` line in its hyphence
metadata header recording a `blake2b256` digest of the config's body
bytes (purpose `madder-blob_store-config-digest-v1`). On read, madder
recomputes the digest and refuses to use a config whose body does
not match. Legacy configs (no `@` line) are trusted silently for
backward compatibility; **`madder list`** flags them and prints a
copy-pasteable **`madder config-pin_digest`** command to migrate.

The digest covers the config's body only. Filesystem permissions,
the parent directory's identity, and the blob contents the config
points at are out of scope.

See FDR 0008 for the full design.
```

**Step 2: Promote the FDR status**

In `docs/features/0008-config-digest-pins.md` frontmatter, change
`status: proposed` to `status: experimental` once Tasks 1-7 are
merged.

**Step 3: Commit**

```bash
git add docs/man.7/blob-store.md docs/features/0008-config-digest-pins.md
git commit -m "docs: document config tamper detection in blob-store(7)

Promote FDR-0008 from proposed to experimental now that Phase 1 has
landed.

Refs: #194.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Out of scope (Phase 2 follow-up plan)

Not in this plan:

- `blob_store_id.Id` text-form extension (`name@<markl-id>`)
- Resolver-side `AssertEqual` against the ID's digest
- "Digest against unmigrated config" error
- Persisting digest-bearing IDs in receipts/inventory archives
- Absolute-path location type

Those land in a Phase 2 plan after this plan's tasks ship.
