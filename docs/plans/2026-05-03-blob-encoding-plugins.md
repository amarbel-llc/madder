# Blob encoding plugins (FDR 0004 v0) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use eng:subagent-driven-development to implement this plan task-by-task.

**Goal:** Replace `code.linenisgreat.com/purse-first/libs/dewey/delta/compression_type` with a madder-owned plugin registry. Pure under-the-hood refactor; no on-disk format change, no CLI surface change, no test behavior change. End state: madder no longer imports `dewey/delta/compression_type`.

**Architecture:** A `Plugin` (= `interfaces.IOWrapper` from `dewey/0/interfaces`) is a stream-to-stream transform. Madder owns four built-in plugins (`none`, `gzip`, `zlib`, `zstd`) at `go/internal/bravo/plugins/`, each in its own subpackage so the package leaf-name convention from FDR 0004 (`madder-codec-zstd-v1@zstd`) holds. A registry resolves `<type-tag>@<builtin-plugin-id>` → factory; a legacy translator maps the on-disk strings (`"zstd"`, `"gzip"`, ...) to plugin references. Existing TOML store configs stay byte-identical on disk; the in-memory representation goes through the new registry instead of the dewey enum.

**Tech Stack:** Go 1.26, `compress/gzip`, `compress/zlib`, `github.com/DataDog/zstd` (current dewey-side library; reuse), dagnabit (re-export generation), `interfaces.IOWrapper` from `dewey/0/interfaces` (we keep this; only `compression_type` goes away).

**Rollback:** Pure refactor. Revert the commit series; on-disk data and external behavior are untouched throughout, so rollback is a single `git revert` away with no migration concerns.

**Source design:** [`docs/features/0004-blob-encoding-plugins.md`](../features/0004-blob-encoding-plugins.md) (commits `4dbd8b6`, `6168de3`).

**Out of scope** (deferred or covered elsewhere):
- V4 store config schema with explicit `plugin-chain` field — separate FDR.
- Build-orchestration for content-addressed builtin-plugin-ids — FDR 0005.
- zstd-with-dict plugin and the `cg capture --zstd-dict` user surface — FDR 0010.
- Pipeline plugins (composing multiple transforms) — additive future work.
- Per-blob plugin marker / wire-format change — explicitly deferred in FDR 0004.

**Universal verification commands** (used at end of every slice):
- Go tests: `just test-go`  *(carries the required `test` build tag; bare `go test ./...` produces spurious `undefined: ui.T` failures)*
- Go build: `just build-go`
- Full nix build: `just build` *(end-of-implementation only)*
- Bats integration: `just test-bats` *(end-of-implementation only)*
- Status check: `git status --short` (verify no stray files)

**Final cleanup verification:**
- `rg --type go 'compression_type\.' go/` MUST return zero matches.
- `rg --type go 'dewey/delta/compression_type' go/` MUST return zero matches.
- `go/go.mod` still lists `dewey` (other dewey packages are still used); only the `compression_type` import is gone.

---

## Slice 1 — Plugin interface, registry, and four built-in plugins

Lands the new `bravo/plugins/` package with the registry, the four built-in plugins, and unit tests. Pure addition — no existing code changes. After this slice, `madder-codec-zstd-v1@zstd` resolves through the new registry to a working `interfaces.IOWrapper`.

### Task 1.1: Define the registry skeleton

**Files:**
- Create: `go/internal/bravo/plugins/main.go`
- Create: `go/internal/bravo/plugins/errors.go`
- Create: `go/internal/bravo/plugins/registry.go`
- Create: `go/internal/bravo/plugins/registry_test.go`

**Step 1: Write the failing test**

Create `go/internal/bravo/plugins/registry_test.go`:

```go
package plugins

import (
	"errors"
	"strings"
	"testing"

	"code.linenisgreat.com/purse-first/libs/dewey/0/interfaces"
)

func TestRegistry_RegisterAndResolve(t *testing.T) {
	r := newRegistry()
	stub := stubFactory(func() interfaces.IOWrapper { return nil })
	if err := r.Register("test-codec-v1@stub", stub); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, err := r.Resolve("test-codec-v1@stub")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != nil {
		t.Errorf("stub factory should produce nil; got %T", got)
	}
}

func TestRegistry_DuplicateRegisterFails(t *testing.T) {
	r := newRegistry()
	stub := stubFactory(func() interfaces.IOWrapper { return nil })
	if err := r.Register("test-codec-v1@dup", stub); err != nil {
		t.Fatalf("first register: %v", err)
	}
	err := r.Register("test-codec-v1@dup", stub)
	if !errors.Is(err, ErrAlreadyRegistered) {
		t.Errorf("expected ErrAlreadyRegistered, got %v", err)
	}
}

func TestRegistry_UnknownReferenceFails(t *testing.T) {
	r := newRegistry()
	_, err := r.Resolve("test-codec-v1@missing")
	if !errors.Is(err, ErrUnknownPlugin) {
		t.Errorf("expected ErrUnknownPlugin, got %v", err)
	}
	if !strings.Contains(err.Error(), "test-codec-v1@missing") {
		t.Errorf("error should mention the bad reference: %v", err)
	}
}

type stubFactory func() interfaces.IOWrapper

func (s stubFactory) New() interfaces.IOWrapper { return s() }
```

**Step 2: Run test to verify it fails**

Run: `just test-go`
Expected: FAIL with `undefined: newRegistry`, `undefined: ErrAlreadyRegistered`, `undefined: ErrUnknownPlugin`.

**Step 3: Write minimal implementation**

Create `go/internal/bravo/plugins/main.go`:

```go
// Package plugins is madder's blob-encoding plugin registry. Each
// plugin is a stream-to-stream transform satisfying
// interfaces.IOWrapper. Per FDR 0004, plugins are referenced by
// `<type-tag>@<builtin-plugin-id>`; v0 builtin-plugin-id is the leaf
// name of the Go package housing the plugin's factory.
package plugins

//go:generate dagnabit export

import (
	"code.linenisgreat.com/purse-first/libs/dewey/0/interfaces"
)

// Factory constructs a plugin instance. v0 plugins are non-parametric;
// future parametric plugins (e.g. zstd-with-dict in FDR 0010) accept
// configuration via a separate side-data interface.
type Factory interface {
	New() interfaces.IOWrapper
}
```

Create `go/internal/bravo/plugins/errors.go` (sentinels — `dewey/bravo/errors` doesn't export `New`, so we use stdlib `errors` here, matching the precedent in `internal/charlie/hyphence/document.go`):

```go
package plugins

import "errors"

var (
	// ErrAlreadyRegistered is returned by Registry.Register when the
	// reference is already registered.
	ErrAlreadyRegistered = errors.New("plugin already registered")

	// ErrUnknownPlugin is returned by Registry.Resolve when the
	// reference is not registered.
	ErrUnknownPlugin = errors.New("unknown plugin reference")
)
```

Then create `go/internal/bravo/plugins/registry.go`:

```go
package plugins

import (
	"sync"

	"code.linenisgreat.com/purse-first/libs/dewey/0/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/bravo/errors"
)

// registry is the in-process plugin index. The package-level Default
// registry is populated at init() by each plugin subpackage.
type registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

func newRegistry() *registry {
	return &registry{factories: map[string]Factory{}}
}

func (r *registry) Register(reference string, f Factory) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.factories[reference]; ok {
		return errors.Errorf("%w: %s", ErrAlreadyRegistered, reference)
	}
	r.factories[reference] = f
	return nil
}

func (r *registry) Resolve(reference string) (interfaces.IOWrapper, error) {
	r.mu.RLock()
	f, ok := r.factories[reference]
	r.mu.RUnlock()
	if !ok {
		return nil, errors.Errorf("%w: %s", ErrUnknownPlugin, reference)
	}
	return f.New(), nil
}

// Default is the package-level registry, populated by plugin
// subpackages at init time. Production callers use this.
var Default = newRegistry()

// MustRegister registers a plugin in the Default registry; panics on
// failure. Used from plugin subpackage init() functions where a
// duplicate registration is a programming error.
func MustRegister(reference string, f Factory) {
	if err := Default.Register(reference, f); err != nil {
		panic(err)
	}
}

// Resolve looks up reference in the Default registry. Returns
// (nil, error wrapping ErrUnknownPlugin) when absent.
func Resolve(reference string) (interfaces.IOWrapper, error) {
	return Default.Resolve(reference)
}
```

**Step 4: Run test to verify it passes**

Run: `just test-go`
Expected: PASS.

**Step 5: Regenerate dagnabit facade**

Run: `just generate-facades`
Expected: `go/pkgs/plugins/main.go` is created with re-exports for `Factory`, `ErrAlreadyRegistered`, `ErrUnknownPlugin`, `MustRegister`, `Resolve`, `Default`.

**Step 6: Commit**

```bash
git add go/internal/bravo/plugins/main.go \
    go/internal/bravo/plugins/errors.go \
    go/internal/bravo/plugins/registry.go \
    go/internal/bravo/plugins/registry_test.go \
    go/pkgs/plugins/main.go
git commit -m "feat(plugins): registry skeleton + sentinel errors

New package internal/bravo/plugins. Holds the global registry of
madder-owned blob-encoding plugins. v0 plugins are non-parametric;
future parametric plugins (zstd-with-dict, FDR 0010) layer on via a
separate side-data interface.

:clown:"
```

---

### Task 1.2: `none` plugin (passthrough)

**Files:**
- Create: `go/internal/bravo/plugins/none/none.go`
- Create: `go/internal/bravo/plugins/none/none_test.go`

**Step 1: Write the failing test**

Create `go/internal/bravo/plugins/none/none_test.go`:

```go
package none

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"code.linenisgreat.com/madder/go/internal/bravo/plugins"
)

func TestNone_RoundTrip(t *testing.T) {
	w, err := plugins.Resolve("madder-codec-none-v1@none")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if w == nil {
		t.Fatal("plugin instance is nil")
	}

	const input = "hello world"
	var encoded bytes.Buffer
	wc, err := w.WrapWriter(&encoded)
	if err != nil {
		t.Fatalf("WrapWriter: %v", err)
	}
	if _, err := io.WriteString(wc, input); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if got := encoded.String(); got != input {
		t.Errorf("none should be identity; encoded = %q, want %q", got, input)
	}

	rc, err := w.WrapReader(strings.NewReader(input))
	if err != nil {
		t.Fatalf("WrapReader: %v", err)
	}
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != input {
		t.Errorf("none should be identity; decoded = %q, want %q", got, input)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `just test-go`
Expected: FAIL — the registry has no entry for `madder-codec-none-v1@none`, so `Resolve` returns `ErrUnknownPlugin`.

**Step 3: Write minimal implementation**

Create `go/internal/bravo/plugins/none/none.go`:

```go
// Package none is the identity (passthrough) blob-encoding plugin.
// Reference: `madder-codec-none-v1@none`.
package none

import (
	"io"

	"code.linenisgreat.com/madder/go/internal/bravo/plugins"
	"code.linenisgreat.com/purse-first/libs/dewey/0/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/charlie/ohio"
)

const Reference = "madder-codec-none-v1@none"

func init() {
	plugins.MustRegister(Reference, factory{})
}

type factory struct{}

func (factory) New() interfaces.IOWrapper { return ohio.NopeIOWrapper{} }
```

**Step 4: Wire the registration into the build**

The plugin's init() runs only if the package is imported. Add a blank import in a central registration file so the Default registry is populated when madder starts. Create `go/internal/bravo/plugins/builtins/builtins.go`:

```go
// Package builtins blank-imports every built-in plugin so their init()
// functions register them in plugins.Default. Import this package once
// from cmd/* main.go's import block to populate the registry.
package builtins

import (
	_ "code.linenisgreat.com/madder/go/internal/bravo/plugins/none"
)
```

(Tests in the `none` package import `none` directly, so its init runs there too. The `builtins` package is for production binaries.)

**Step 5: Run test to verify it passes**

Run: `just test-go`
Expected: PASS — the `none_test.go` file imports the `none` package, which triggers init, which registers the plugin.

**Step 6: Regenerate facade and commit**

```bash
just generate-facades
git add go/internal/bravo/plugins/none/ \
    go/internal/bravo/plugins/builtins/ \
    go/pkgs/plugins/main.go
git commit -m "feat(plugins): add none (passthrough) plugin

Reference: madder-codec-none-v1@none. Wraps ohio.NopeIOWrapper.
Registers itself in plugins.Default at init time.

Adds plugins/builtins as the blank-import aggregator that
production binaries pull in to populate the registry; tests import
plugin packages directly so init runs without going through
builtins.

:clown:"
```

---

### Task 1.3: `gzip` plugin

**Files:**
- Create: `go/internal/bravo/plugins/gzip/gzip.go`
- Create: `go/internal/bravo/plugins/gzip/gzip_test.go`
- Modify: `go/internal/bravo/plugins/builtins/builtins.go`

**Step 1: Write the failing test**

Create `go/internal/bravo/plugins/gzip/gzip_test.go`:

```go
package gzip

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"code.linenisgreat.com/madder/go/internal/bravo/plugins"
)

func TestGzip_RoundTrip(t *testing.T) {
	w, err := plugins.Resolve("madder-codec-gzip-v1@gzip")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	const input = "the quick brown fox jumps over the lazy dog"
	var encoded bytes.Buffer
	wc, err := w.WrapWriter(&encoded)
	if err != nil {
		t.Fatalf("WrapWriter: %v", err)
	}
	if _, err := io.WriteString(wc, input); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if encoded.Len() == 0 {
		t.Fatal("encoded length zero — gzip writer didn't run")
	}
	// gzip magic bytes
	if !bytes.HasPrefix(encoded.Bytes(), []byte{0x1f, 0x8b}) {
		t.Errorf("encoded bytes missing gzip magic: %x", encoded.Bytes()[:2])
	}

	rc, err := w.WrapReader(strings.NewReader(encoded.String()))
	if err != nil {
		t.Fatalf("WrapReader: %v", err)
	}
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != input {
		t.Errorf("decode mismatch:\n got: %q\nwant: %q", got, input)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `just test-go`
Expected: FAIL with `ErrUnknownPlugin: madder-codec-gzip-v1@gzip`.

**Step 3: Write the implementation**

Create `go/internal/bravo/plugins/gzip/gzip.go`:

```go
// Package gzip is the gzip blob-encoding plugin.
// Reference: `madder-codec-gzip-v1@gzip`.
package gzip

import (
	stdgzip "compress/gzip"
	"io"

	"code.linenisgreat.com/madder/go/internal/bravo/plugins"
	"code.linenisgreat.com/purse-first/libs/dewey/0/interfaces"
)

const Reference = "madder-codec-gzip-v1@gzip"

func init() {
	plugins.MustRegister(Reference, factory{})
}

type factory struct{}

func (factory) New() interfaces.IOWrapper { return wrapper{} }

type wrapper struct{}

func (wrapper) WrapWriter(w io.Writer) (io.WriteCloser, error) {
	return stdgzip.NewWriter(w), nil
}

func (wrapper) WrapReader(r io.Reader) (io.ReadCloser, error) {
	return stdgzip.NewReader(r)
}
```

**Step 4: Add to builtins**

Modify `go/internal/bravo/plugins/builtins/builtins.go`:

```go
package builtins

import (
	_ "code.linenisgreat.com/madder/go/internal/bravo/plugins/gzip"
	_ "code.linenisgreat.com/madder/go/internal/bravo/plugins/none"
)
```

**Step 5: Run test to verify it passes**

Run: `just test-go`
Expected: PASS.

**Step 6: Regenerate facade and commit**

```bash
just generate-facades
git add go/internal/bravo/plugins/gzip/ \
    go/internal/bravo/plugins/builtins/builtins.go
git commit -m "feat(plugins): add gzip plugin

Reference: madder-codec-gzip-v1@gzip. Wraps compress/gzip.

:clown:"
```

---

### Task 1.4: `zlib` plugin

**Files:**
- Create: `go/internal/bravo/plugins/zlib/zlib.go`
- Create: `go/internal/bravo/plugins/zlib/zlib_test.go`
- Modify: `go/internal/bravo/plugins/builtins/builtins.go`

Follow the same pattern as Task 1.3, substituting `compress/zlib` for `compress/gzip`. Reference: `madder-codec-zlib-v1@zlib`.

**Test fixture:** zlib's two-byte header `0x78 0x9c` (default compression level) — assert the encoded output begins with this prefix in addition to round-tripping the same `the quick brown fox...` string.

**Commit message:**

```
feat(plugins): add zlib plugin

Reference: madder-codec-zlib-v1@zlib. Wraps compress/zlib.

:clown:
```

---

### Task 1.5: `zstd` plugin

**Files:**
- Create: `go/internal/bravo/plugins/zstd/zstd.go`
- Create: `go/internal/bravo/plugins/zstd/zstd_test.go`
- Modify: `go/internal/bravo/plugins/builtins/builtins.go`

**Step 1: Write the failing test**

Same shape as gzip. Reference: `madder-codec-zstd-v1@zstd`. zstd magic bytes are `0x28 0xb5 0x2f 0xfd` (RFC 8878 §3).

**Step 3: Write the implementation**

Mirror the existing dewey-side zstd routing (which uses `github.com/DataDog/zstd`):

```go
// Package zstd is the zstd blob-encoding plugin.
// Reference: `madder-codec-zstd-v1@zstd`.
package zstd

import (
	"io"

	"github.com/DataDog/zstd"
	"code.linenisgreat.com/madder/go/internal/bravo/plugins"
	"code.linenisgreat.com/purse-first/libs/dewey/0/interfaces"
)

const Reference = "madder-codec-zstd-v1@zstd"

func init() {
	plugins.MustRegister(Reference, factory{})
}

type factory struct{}

func (factory) New() interfaces.IOWrapper { return wrapper{} }

type wrapper struct{}

func (wrapper) WrapWriter(w io.Writer) (io.WriteCloser, error) {
	return zstd.NewWriter(w), nil
}

func (wrapper) WrapReader(r io.Reader) (io.ReadCloser, error) {
	// zstd.NewReader's Close releases CGo-owned native zstd context;
	// don't wrap in NopCloser (the existing dewey-side routing has
	// the same leak — separate follow-up).
	return zstd.NewReader(r), nil
}
```

**Commit message:**

```
feat(plugins): add zstd plugin

Reference: madder-codec-zstd-v1@zstd. Wraps github.com/DataDog/zstd.
zstd.NewReader's Close releases CGo-owned native zstd context, so
the returned io.ReadCloser is used directly (no NopCloser wrap).

:clown:
```

---

### Task 1.6: Slice 1 verification

**Step 1: Full test run**

Run: `just test-go`
Expected: PASS for the four plugin packages, all existing tests still green.

**Step 2: Build verification**

Run: `just build-go`
Expected: PASS.

**Step 3: Spot-check the facade**

Run: `git diff go/pkgs/plugins/main.go` (if it changed) — confirm only additive re-exports for `Factory`, `Default`, `MustRegister`, `Resolve`, `ErrAlreadyRegistered`, `ErrUnknownPlugin`. The plugin-specific subpackages (none, gzip, zlib, zstd) don't generate their own facades — they're consumed only via the registry.

---

## Slice 2 — Legacy translation table

Maps the existing on-disk `compression-type` strings (`""`, `"none"`, `"gzip"`, `"zlib"`, `"zstd"`) to plugin references. Pure addition — no consumer updates yet.

### Task 2.1: Implement and test the translator

**Files:**
- Create: `go/internal/bravo/plugins/legacy.go`
- Create: `go/internal/bravo/plugins/legacy_test.go`

**Step 1: Write the failing test**

Create `go/internal/bravo/plugins/legacy_test.go`. Note: this test
lives in the **external test package** `plugins_test` rather than
in-package. Why: it blank-imports `plugins/builtins` to populate the
default registry, and `builtins` transitively imports `plugins` — an
in-package test (`package plugins`) would create an import cycle. The
external test package compiles into a separate test binary so the
cycle doesn't form.

```go
package plugins_test

import (
	"errors"
	"testing"

	"code.linenisgreat.com/madder/go/internal/bravo/plugins"
	_ "code.linenisgreat.com/madder/go/internal/bravo/plugins/builtins"
)

func TestLegacyCompressionRef(t *testing.T) {
	cases := []struct {
		legacy string
		want   string
	}{
		{"", "madder-codec-none-v1@none"},
		{"none", "madder-codec-none-v1@none"},
		{"gzip", "madder-codec-gzip-v1@gzip"},
		{"zlib", "madder-codec-zlib-v1@zlib"},
		{"zstd", "madder-codec-zstd-v1@zstd"},
	}
	for _, tc := range cases {
		got, err := plugins.LegacyCompressionRef(tc.legacy)
		if err != nil {
			t.Errorf("legacy %q: unexpected error %v", tc.legacy, err)
			continue
		}
		if got != tc.want {
			t.Errorf("legacy %q: got %q, want %q", tc.legacy, got, tc.want)
		}
	}
}

func TestLegacyCompressionRef_Unknown(t *testing.T) {
	_, err := plugins.LegacyCompressionRef("brotli")
	if !errors.Is(err, plugins.ErrUnknownLegacyCompression) {
		t.Errorf("expected ErrUnknownLegacyCompression, got %v", err)
	}
}

func TestLegacyCompression_ResolvesViaDefault(t *testing.T) {
	for _, legacy := range []string{"", "none", "gzip", "zlib", "zstd"} {
		ref, err := plugins.LegacyCompressionRef(legacy)
		if err != nil {
			t.Fatalf("ref %q: %v", legacy, err)
		}
		if _, err := plugins.Resolve(ref); err != nil {
			t.Errorf("legacy %q -> %q failed Resolve: %v", legacy, ref, err)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `just test-go`
Expected: FAIL with `undefined: LegacyCompressionRef`, `undefined: ErrUnknownLegacyCompression`.

**Step 3: Write minimal implementation**

Create `go/internal/bravo/plugins/legacy.go`:

```go
package plugins

import (
	"code.linenisgreat.com/purse-first/libs/dewey/bravo/errors"
)

// ErrUnknownLegacyCompression is returned by LegacyCompressionRef when
// the input string is not one of the known on-disk values.
var ErrUnknownLegacyCompression = errors.New("unknown legacy compression-type")

// legacyCompressionTable maps on-disk compression-type strings to
// plugin references. The empty string is the v1/v2 default and means
// "no compression."
var legacyCompressionTable = map[string]string{
	"":     "madder-codec-none-v1@none",
	"none": "madder-codec-none-v1@none",
	"gzip": "madder-codec-gzip-v1@gzip",
	"zlib": "madder-codec-zlib-v1@zlib",
	"zstd": "madder-codec-zstd-v1@zstd",
}

// LegacyCompressionRef returns the plugin reference equivalent to a
// legacy on-disk compression-type string. Used when loading V1/V2/V3
// store configs to bridge their string-typed field into the plugin
// abstraction.
func LegacyCompressionRef(legacy string) (string, error) {
	ref, ok := legacyCompressionTable[legacy]
	if !ok {
		return "", errors.Errorf("%w: %q", ErrUnknownLegacyCompression, legacy)
	}
	return ref, nil
}
```

**Step 4: Run test to verify it passes**

Run: `just test-go`
Expected: PASS.

**Step 5: Regenerate facade and commit**

```bash
just generate-facades
git add go/internal/bravo/plugins/legacy.go \
    go/internal/bravo/plugins/legacy_test.go \
    go/pkgs/plugins/main.go
git commit -m "feat(plugins): legacy compression-type translator

LegacyCompressionRef maps on-disk strings (\"\", \"none\", \"gzip\",
\"zlib\", \"zstd\") to plugin references. Used by V1/V2/V3 store
config loaders to bridge their string field into the plugin
abstraction without changing the on-disk format.

:clown:"
```

---

### Task 2.2: Slice 2 verification

Run: `just test-go` and `just build-go`. Both PASS. The plugin registry can now resolve all four legacy compression types via the translator.

---

## Slice 3 — Migrate `inventory_archive` byte ↔ codec maps

The biggest single touch. Today `internal/alfa/inventory_archive/types.go:104-117` keys two maps by `compression_type.CompressionType`. We re-key them to plugin references (strings).

### Task 3.1: Refactor types.go maps

**Files:**
- Modify: `go/internal/alfa/inventory_archive/types.go`

**Step 1: Write the failing test (replace the implementation first; tests come from the existing fixture)**

This task is structural — no behavior change. The existing tests in `go/internal/alfa/inventory_archive/data_writer_test.go` and `data_reader_test.go` will fail to compile after the refactor until callers update. Run `just test-go` first to see compile errors as the equivalent of "failing tests."

Run: `just test-go` *(before changes)*
Expected: PASS (baseline).

**Step 2: Update types.go**

Replace the `compressionToByteMap` and `byteToCompressionMap` plus the helpers `CompressionToByte` / `ByteToCompression`. New signatures:

```go
// (in types.go — replace existing maps and helper signatures)
import (
	"code.linenisgreat.com/madder/go/internal/bravo/plugins"
	"code.linenisgreat.com/purse-first/libs/dewey/bravo/errors"
)

var compressionRefToByteMap = map[string]byte{
	"madder-codec-none-v1@none": CompressionByteNone,
	"madder-codec-gzip-v1@gzip": CompressionByteGzip,
	"madder-codec-zlib-v1@zlib": CompressionByteZlib,
	"madder-codec-zstd-v1@zstd": CompressionByteZstd,
}

var byteToCompressionRefMap = map[byte]string{
	CompressionByteNone: "madder-codec-none-v1@none",
	CompressionByteGzip: "madder-codec-gzip-v1@gzip",
	CompressionByteZlib: "madder-codec-zlib-v1@zlib",
	CompressionByteZstd: "madder-codec-zstd-v1@zstd",
}

// CompressionRefToByte maps a plugin reference to the on-disk
// compression byte used in inventory_archive entries. Returns an
// error for plugin references this archive format doesn't know
// about (e.g. parametric variants like zstd-with-dict).
func CompressionRefToByte(ref string) (byte, error) {
	b, ok := compressionRefToByteMap[ref]
	if !ok {
		return 0, errors.Errorf("unsupported compression for inventory_archive: %q", ref)
	}
	return b, nil
}

// ByteToCompressionRef maps an on-disk inventory_archive compression
// byte back to a plugin reference. Used by data_reader to instantiate
// the correct decoder for each entry.
func ByteToCompressionRef(b byte) (string, error) {
	ref, ok := byteToCompressionRefMap[b]
	if !ok {
		return "", errors.Errorf("unknown compression byte: 0x%02x", b)
	}
	return ref, nil
}
```

(The existing `CompressionByteNone`, `CompressionByteGzip`, etc. const declarations stay unchanged.)

The package's import of `compression_type` goes away in this file.

**Step 3: Update data_writer.go, data_writer_v1.go, data_reader.go, data_reader_v1.go**

Each currently has fields and parameters typed as `compression_type.CompressionType`. Change them to `string` (the plugin reference).

For `data_writer_v1.go`:
- Field `compressionType compression_type.CompressionType` → `compressionRef string`.
- Constructor parameter `ct compression_type.CompressionType` → `compressionRef string`.
- Wherever `ct.WrapWriter(...)` is called, replace with: resolve the plugin from `plugins.Resolve(compressionRef)` then call `.WrapWriter(...)` on the result.

Same shape for `data_writer.go`, `data_reader.go`, `data_reader_v1.go`.

The `CompressionType()` accessor on `DataReader` and `DataReaderV1` (lines 139, 143) returns the legacy enum today. Rename to `CompressionRef()` returning `string`. Update all callers in the same package.

**Step 4: Update the inventory_archive tests**

`data_writer_test.go`, `data_writer_v1_test.go`, and any others in the package: replace `ct := compression_type.CompressionTypeNone` with `ref := "madder-codec-none-v1@none"` and pass `ref` (the string) instead of `ct` (the enum). Same for Zstd → `"madder-codec-zstd-v1@zstd"`.

Add the import `_ "code.linenisgreat.com/madder/go/internal/bravo/plugins/builtins"` to the test files so the registry is populated when tests run.

**Step 5: Run tests**

Run: `just test-go`
Expected: PASS. All inventory_archive tests round-trip through the plugin registry.

**Step 6: Commit**

```bash
git add go/internal/alfa/inventory_archive/
git commit -m "refactor(inventory_archive): byte ↔ plugin-ref maps

Repurposes inventory_archive's per-entry compression byte to map
to/from plugin references instead of dewey's compression_type enum.
DataReader and DataWriter (both v0 and v1) now hold and accept
string plugin references; helpers CompressionRefToByte and
ByteToCompressionRef replace CompressionToByte / ByteToCompression.

The on-disk wire format is unchanged — same byte tags
(0x00..0x03), same per-entry layout. Only the in-memory typing
changes.

:clown:"
```

---

### Task 3.2: Slice 3 verification

Run: `just test-go` and `just build-go`. Both PASS. inventory_archive no longer imports `compression_type`.

Run: `rg --type go 'compression_type\.' go/internal/alfa/inventory_archive/`
Expected: zero matches.

---

## Slice 4 — Migrate `blob_store_configs` TOML fields

Each TOML config impl in `internal/charlie/blob_store_configs/` has a `CompressionType` field of type `compression_type.CompressionType`. Change to `string`. The TOML field name stays `compression-type`; the on-disk schema is unchanged.

### Task 4.1: Refactor toml_v1.go, toml_v2.go, toml_v3.go

**Files:**
- Modify: `go/internal/charlie/blob_store_configs/toml_v1.go`
- Modify: `go/internal/charlie/blob_store_configs/toml_v2.go`
- Modify: `go/internal/charlie/blob_store_configs/toml_v3.go`

**Per-file change:**

```go
// Before:
type TomlV3 struct {
    // ...
    CompressionType compression_type.CompressionType `toml:"compression-type"`
    // ...
}

func (blobStoreConfig TomlV3) GetBlobCompression() interfaces.IOWrapper {
    return &blobStoreConfig.CompressionType
}

// After:
type TomlV3 struct {
    // ...
    CompressionType string `toml:"compression-type"`
    // ...
}

func (blobStoreConfig TomlV3) GetBlobCompression() interfaces.IOWrapper {
    ref, err := plugins.LegacyCompressionRef(blobStoreConfig.CompressionType)
    if err != nil {
        // For load-time robustness: fall back to the none plugin and
        // log? Or panic? Choose: return the none plugin instance
        // (best-effort) and let downstream code surface the issue
        // when the wrong compression is applied. The legacy table
        // accepts "" / "none" / "gzip" / "zlib" / "zstd" which
        // covers every value the existing on-disk corpus carries.
        // Anything else means the user hand-edited the TOML to
        // something unsupported.
        ref = "madder-codec-none-v1@none"
    }
    plugin, err := plugins.Resolve(ref)
    if err != nil {
        panic(err) // Programming error: registry should always have these.
    }
    return plugin
}
```

**Same pattern for V1 and V2.** The TOML tag stays `compression-type`. The struct field type changes from `compression_type.CompressionType` to `string`.

**Note:** the `panic` on `Resolve` failure is correct — if the legacy table maps a value that the registry doesn't have, that's a build error, not a runtime config problem.

**Step 1: Run baseline tests**

Run: `just test-go`
Expected: PASS (baseline).

**Step 2: Apply the changes**

For each of `toml_v1.go`, `toml_v2.go`, `toml_v3.go`:
1. Drop the `compression_type` import.
2. Change the field type to `string`.
3. Rewrite `GetBlobCompression()` per the pattern above.
4. Add `import "code.linenisgreat.com/madder/go/internal/bravo/plugins"`.

**Step 3: Run tests**

Run: `just test-go`
Expected: PASS — TOML round-tripping should work because `string` and `compression_type.CompressionType` (which is `type CompressionType string`) marshal identically.

**Step 4: Commit**

```bash
git add go/internal/charlie/blob_store_configs/toml_v{1,2,3}.go
git commit -m "refactor(blob_store_configs): TomlV1/V2/V3 use plugin refs

The on-disk TOML format is unchanged: compression-type still
serializes as a string. The Go field type changes from
compression_type.CompressionType to string; GetBlobCompression
translates via plugins.LegacyCompressionRef and resolves the
plugin from plugins.Default.

:clown:"
```

---

### Task 4.2: Refactor toml_inventory_archive_v0.go, _v1.go, _v2.go and main.go

**Files:**
- Modify: `go/internal/charlie/blob_store_configs/toml_inventory_archive_v0.go`
- Modify: `go/internal/charlie/blob_store_configs/toml_inventory_archive_v1.go`
- Modify: `go/internal/charlie/blob_store_configs/toml_inventory_archive_v2.go`
- Modify: `go/internal/charlie/blob_store_configs/main.go`

The inventory-archive variants have the same shape plus a `GetCompressionType()` accessor that returns `compression_type.CompressionType`. Rename to `GetCompressionRef() string` returning the plugin reference (via `plugins.LegacyCompressionRef`). Update the interface declaration in `main.go:63`:

```go
// Before:
GetCompressionType() compression_type.CompressionType
// After:
GetCompressionRef() string
```

Update all callers of `GetCompressionType()` across the codebase to use `GetCompressionRef()`. Search-and-replace target:

```bash
rg 'GetCompressionType\(' go/
```

The inventory-archive store implementations in `foxtrot/blob_stores/store_inventory_archive*.go` are likely callers; update them to pass the ref through to the (now-string-keyed) `inventory_archive` package APIs from Slice 3.

**Step 1: Run baseline tests**

Run: `just test-go`
Expected: PASS.

**Step 2: Apply changes file-by-file**

For each of the three TOML inventory-archive files: same pattern as Task 4.1 (string field, translate-via-LegacyCompressionRef in GetBlobCompression). Plus rename `GetCompressionType` to `GetCompressionRef` returning `string`.

In `main.go`, update the interface declaration.

In every caller of the renamed method (search via `rg 'GetCompressionType\('`), update to the new name and string return type.

**Step 3: Run tests**

Run: `just test-go`
Expected: PASS.

**Step 4: Regenerate facades and commit**

```bash
just generate-facades
git add go/internal/charlie/blob_store_configs/ \
    go/internal/foxtrot/blob_stores/ \
    go/pkgs/blob_store_configs/main.go
git commit -m "refactor(blob_store_configs): inventory-archive variants use plugin refs

Same shape as TomlV1/V2/V3: compression-type stays a string on
disk; GetBlobCompression translates via plugins.LegacyCompressionRef.

GetCompressionType is renamed to GetCompressionRef returning
string. Callers in foxtrot/blob_stores update to use the new name
and pass the plugin ref to the now-string-keyed inventory_archive
APIs from Slice 3.

:clown:"
```

---

### Task 4.3: Slice 4 verification

Run: `just test-go` and `just build-go`. Both PASS. Run:

```bash
rg --type go 'compression_type\.' go/internal/charlie/blob_store_configs/
rg --type go 'compression_type\.' go/internal/foxtrot/blob_stores/
```

Expected: zero matches in `blob_store_configs/`. The `foxtrot/blob_stores/` results may still have a few from `discover.go` and tests — those are addressed in Slices 5 and 6.

---

## Slice 5 — Consumer cleanup

Updates the remaining production call sites: `env_dir/blob_config.go` (the awkward type assertion), `env_dir/blob_reader.go`, default-CompressionType call sites in `discover.go`, `delta/blob_store_configs/main.go`, `commands/init.go`, `commands_cache/init.go`.

### Task 5.1: Replace the env_dir type assertion

**Files:**
- Modify: `go/internal/echo/env_dir/blob_config.go`

The current `HasIdentityWrappers()` method (lines 74-97) asserts `config.GetBlobCompression().(*compression_type.CompressionType)` and checks for None/Empty. Replace with a marker check on the plugin instance.

The cleanest approach: expose a sentinel from the `none` plugin package and check for that.

**Step 1: Add a sentinel to the none plugin**

Modify `go/internal/bravo/plugins/none/none.go`: export a package-level `Wrapper` value that callers can compare against:

```go
// Wrapper is the singleton instance returned by the none plugin's
// factory. Callers checking for byte-identity behavior can compare
// against this value.
var Wrapper = ohio.NopeIOWrapper{}

func (factory) New() interfaces.IOWrapper { return Wrapper }
```

**Step 2: Update HasIdentityWrappers**

```go
// In go/internal/echo/env_dir/blob_config.go:
import (
    "code.linenisgreat.com/madder/go/internal/bravo/plugins/none"
    // (drop compression_type import)
)

func (config Config) HasIdentityWrappers() bool {
    if config.GetBlobCompression() != none.Wrapper {
        return false
    }
    // (existing encryption check unchanged)
    switch config.GetBlobEncryption().(type) {
    case *ohio.NopeIOWrapper, ohio.NopeIOWrapper:
        return true
    default:
        return false
    }
}
```

**Step 3: Update the default constant**

The current `defaultCompressionTypeValue = compression_type.CompressionTypeNone` (line 37) becomes a plugin-derived default:

```go
// before:
var defaultCompressionTypeValue = compression_type.CompressionTypeNone
// after:
var defaultCompressionWrapper interfaces.IOWrapper = none.Wrapper
```

Update any users of `defaultCompressionTypeValue` to use `defaultCompressionWrapper`.

**Step 4: Update blob_reader.go**

`go/internal/echo/env_dir/blob_reader.go:52` calls `compression_type.CompressionTypeNone.WrapReader(...)`. Replace with `none.Wrapper.WrapReader(...)`.

**Step 5: Run tests**

Run: `just test-go`
Expected: PASS.

**Step 6: Commit**

```bash
git add go/internal/echo/env_dir/blob_config.go \
    go/internal/echo/env_dir/blob_reader.go \
    go/internal/bravo/plugins/none/none.go
git commit -m "refactor(env_dir): replace compression_type type-assertion

HasIdentityWrappers compared the result of GetBlobCompression
against compression_type.CompressionTypeNone. Replace with a
singleton-pointer comparison against the none plugin's Wrapper
value. Same semantic; no dewey enum dependency.

:clown:"
```

---

### Task 5.2: Update default-compression call sites

**Files:**
- Modify: `go/internal/foxtrot/blob_stores/discover.go`
- Modify: `go/internal/delta/blob_store_configs/main.go`
- Modify: `go/internal/india/commands/init.go`
- Modify: `go/internal/india/commands_cache/init.go`

Each file currently has a line like:

```go
CompressionType: compression_type.CompressionTypeDefault,
```

`compression_type.CompressionTypeDefault` is a constant equal to `"zstd"` (per dewey/delta/compression_type/main.go:33). After Slice 4, the `CompressionType` field on these configs is a `string`. So the change is a literal:

```go
CompressionType: "zstd",
```

(Or define a local `const defaultCompressionRef = "zstd"` if you prefer a named constant — your choice; v0 just needs the dewey import gone.)

**Step 1: Apply the change in each file**

Use `Edit` per file. Drop the `compression_type` import line (`go vet` will complain otherwise).

**Step 2: Run tests**

Run: `just test-go`
Expected: PASS.

**Step 3: Commit**

```bash
git add go/internal/foxtrot/blob_stores/discover.go \
    go/internal/delta/blob_store_configs/main.go \
    go/internal/india/commands/init.go \
    go/internal/india/commands_cache/init.go
git commit -m "refactor(defaults): use plain \"zstd\" instead of compression_type

Each store-init call site set CompressionType to
compression_type.CompressionTypeDefault. Now that the field is a
plain string, use the literal \"zstd\" directly.

:clown:"
```

---

### Task 5.3: Slice 5 verification

Run: `just test-go` and `just build-go`. Both PASS. Spot-check:

```bash
rg --type go 'compression_type\.' go/internal/echo/
rg --type go 'compression_type\.' go/internal/foxtrot/blob_stores/
rg --type go 'compression_type\.' go/internal/delta/
rg --type go 'compression_type\.' go/internal/india/
```

Expected: zero matches in production files. Tests may still reference `compression_type` — Slice 6 cleans those up.

---

## Slice 6 — Migrate test fixtures

The remaining `compression_type.` matches all live in `_test.go` files. Migrate each.

### Task 6.1: Migrate inventory_archive tests

**Files:**
- Modify: `go/internal/alfa/inventory_archive/data_writer_test.go`
- Modify: `go/internal/alfa/inventory_archive/data_writer_v1_test.go`

Each currently has lines like `ct := compression_type.CompressionTypeNone` followed by `ct` being passed to a writer constructor that (post-Slice 3) takes a `string` plugin reference.

Replace with:

```go
ref := "madder-codec-none-v1@none"  // or the matching ref for Zstd
```

Pass `ref` instead of `ct`. Drop the `compression_type` import; add `_ "code.linenisgreat.com/madder/go/internal/bravo/plugins/builtins"` to ensure the registry is populated (some tests may already have this from Slice 3 — confirm and avoid duplicates).

Run: `just test-go`. Commit.

### Task 6.2: Migrate foxtrot/blob_stores tests

**Files:**
- Modify: `go/internal/foxtrot/blob_stores/concurrent_write_test.go`
- Modify: `go/internal/foxtrot/blob_stores/pack_v1_test.go`
- Modify: `go/internal/foxtrot/blob_stores/store_inventory_archive_test.go`

Same pattern. Replace `compression_type.CompressionTypeXxx` with the corresponding plugin reference string. Where these tests construct configs (e.g., `CompressionType: compression_type.CompressionTypeNone`), the field is now `string`; pass the legacy string `"none"` (or `"zstd"`, etc.) — the on-disk-string form, not the plugin reference, because these are config struct literals.

Run: `just test-go`. Commit.

### Task 6.3: Migrate env_dir tests

**Files:**
- Modify: `go/internal/echo/env_dir/blob_config_test.go`
- Modify: `go/internal/echo/env_dir/blob_reader_mmap_test.go`

`zstd := compression_type.CompressionTypeZstd` becomes `zstd, _ := plugins.Resolve("madder-codec-zstd-v1@zstd")` (or use `none.Wrapper` for the none case).

Run: `just test-go`. Commit.

### Task 6.4: Slice 6 verification

Run: `rg --type go 'compression_type\.' go/`

Expected: **zero matches anywhere in `go/`.**

Run: `just test-go`. Expected: PASS.

---

## Slice 7 — Drop the dewey/compression_type import

### Task 7.1: Confirm zero remaining references and tidy

**Step 1: Final search**

```bash
rg --type go 'compression_type\.' go/
rg --type go 'dewey/delta/compression_type' go/
```

Both must return zero matches. If anything remains, fix before continuing.

**Step 2: `go mod tidy`**

Run from `go/`:

```bash
cd go && go mod tidy
```

This may or may not remove the dewey dependency from `go.mod` — dewey is a single Go module, and other dewey subpaths (e.g. `dewey/0/interfaces`, `dewey/charlie/ohio`, `dewey/bravo/errors`) are still used. The expected outcome is **no changes** to `go.mod` because the module dep stays. If a `gomod2nix.toml` regen happens via the post-tidy hook, that's fine.

**Step 3: Regenerate facades**

```bash
just generate-facades
```

The `compression_type` re-export in `go/pkgs/...` (if any) goes away. The plugins package's facade is finalized.

**Step 4: Run tests + race + build**

```bash
just test-go
just test-go-race
just build-go
```

All three: PASS.

**Step 5: Commit (if there are any cleanup changes)**

If `go.mod`, `go.sum`, `gomod2nix.toml`, or the dagnabit facades changed, commit:

```bash
git add go/go.mod go/go.sum go/gomod2nix.toml go/pkgs/
git commit -m "chore: tidy after dropping compression_type import

go mod tidy + facade regeneration after the refactor. dewey is
still used (interfaces, ohio, errors); only the compression_type
subpath is gone from madder's import set.

:clown:"
```

If nothing changed, skip the commit.

---

### Task 7.2: Final verification

Run the full verification suite:

```bash
just test-go         # all green
just test-go-race    # all green
just build-go        # PASS
just build           # full nix build PASS, all binaries produced
just test-bats       # 97/97 PASS (no regressions)
git status --short   # clean except .claude/
rg --type go 'compression_type\.' go/  # zero matches
rg --type go 'dewey/delta/compression_type' go/  # zero matches
```

If all of these pass, FDR 0004 v0 is shipped. The only remaining work is FDR 0010 (zstd-with-dict), which builds on this foundation.

---

## Final state

A successful run produces:
- `go/internal/bravo/plugins/` — registry, legacy translator, sentinel errors.
- `go/internal/bravo/plugins/{none,gzip,zlib,zstd}/` — four built-in plugins.
- `go/internal/bravo/plugins/builtins/` — blank-import aggregator.
- `go/pkgs/plugins/main.go` — auto-generated dagnabit facade.
- All existing tests + bats green.
- Zero references to `compression_type.` across `go/`.
- `dewey/delta/compression_type` no longer imported (other dewey subpaths unaffected).

The user-facing surface (`madder write`, `madder cat`, `cg capture`, `madder init`, the four hyphence subcommands, every existing flag) is byte-identical in behavior. On-disk store configs are unchanged. inventory_archive's per-entry compression byte format is unchanged.

The plugin abstraction is now the single point of dispatch for compression encoding, ready to grow:
- FDR 0010 will add the `zstd-with-dict` plugin, the cg-capture flag, and the train-zstd-dict subcommand on top of this v0.
- A future V4 store config schema can surface plugin references directly in TOML.
- A future build-orchestration FDR (0005) can wire content-addressed builtin-plugin-ids without touching this layer.
