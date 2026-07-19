# FDR-0009 Multi-Store Config Type — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use eng:subagent-driven-development
> to implement this plan task-by-task.

**Goal:** Add `store_type = "multi"` as a first-class persistent
`blob_store-config`, reachable from the CLI via a new `madder
init-multi` command, so a configured multi store composes through
every command (`cat`/`has`/`list`/`fsck`) transparently — no
per-command flag.

**Architecture:** FDR-0009 adds a thin config-resolution layer on top
of the existing `Multi` primitive
(`go/internal/foxtrot/blob_stores/multi.go`, built by #182; do not
touch it). A new on-disk config type
(`!toml-blob_store_config-multi-v0`) carries a mode and an ordered set
of **digest-bearing** blob-store-id references (the
`name@blake2b256-…` form landed by FDR-0008 Phase 2, #198). At
store-map build time, a new fixpoint pass in `MakeBlobStores`
materializes multis in dependency order (leaves first, then multis
whose every reference is already built), asserting each reference's
digest against the resolved store's Phase-1 self-check digest via
`markl.AssertEqual`. Because every reference is a content digest, the
graph is a Merkle DAG and cycles are unrepresentable — no runtime
cycle detection is needed.

**Tech Stack:** Go, the hyphence typed-blob coder + `tommy`
codegen, `blob_store_id.Id` (Phase 2 digest suffix), `markl`
(`FormatHash`/`Id`/`AssertEqual`), the `Multi` builder
(`go/pkgs/blob_stores`), the futility CLI framework, bats integration
tests.

**Rollback:** Every task is one revert and is additive. The new type
id, config struct, Coder map entry, factory case, and the
`init-multi` command are all new surface; reverting them leaves
existing store types untouched. The only edit to shared control flow
is the new **third pass** in `MakeBlobStores` (Task 5), which is a
pure addition after the existing two passes — reverting it restores
prior behavior, and a repo with no multi configs is unaffected by it
regardless. No on-disk format owned by another tool changes.

**FDR:** [`docs/features/0009-multi-store-config-type.md`](../features/0009-multi-store-config-type.md)
**Umbrella:** [#217](https://github.com/amarbel-llc/madder/issues/217)
(Step 3). **Depends on:** FDR-0008 Phase 1 (#197, merged) + Phase 2
(#198, merged) — both at `experimental`, both landed on master.

---

## Orientation notes (read before starting)

These are facts established by reading the code on 2026-05-31. They
resolve ambiguities in the FDR prose and prevent dead-ends.

### A. The FDR's `store_type = "multi"` TOML line is illustrative, not literal

No madder config serializes a `store_type` key. The store type is
discriminated entirely by the hyphence type tag on the first line of
the config (e.g. `!toml-blob_store_config-v3`,
`!toml-blob_store_config-pointer-v1`) — see every struct in
`go/internal/charlie/blob_store_configs/toml_*.go` and the Coder map
in `go/internal/delta/blob_store_configs/coding.go`. `GetBlobStoreType()`
returns a secondary discriminator string ("local", "sftp",
"local-pointer-v1") that is **not** written to disk.

**Decision:** the multi type is the tag
`!toml-blob_store_config-multi-v0`. Do **not** add a `store_type`
TOML field — that would duplicate the tag and break the established
pattern. The "locked wire-format string" the FDR refers to is this
tag plus the `coding.go` map key, exactly as for every other type.
The on-disk body is:

    !toml-blob_store_config-multi-v0
    @ blake2b256-…

    mode = "write_through"
    write-store = "default@blake2b256-2k4p9r3m…"
    read-stores = ["archive@blake2b256-9ft3m74l…"]
    read-fill = true

### B. TOML key naming: hyphens, not underscores

`TomlPointerV1` uses `toml:"base-path"`
(`go/internal/charlie/blob_store_configs/toml_pointer_v1.go:13`). The
FDR shows underscores (`write_store`, `read_stores`). **Match the
repo: use hyphens** (`write-store`, `read-stores`, `mirror-stores`,
`read-fill`). Before writing the struct tags, grep neighboring
configs to confirm the convention is uniform:

```
rg -n 'toml:"' go/internal/charlie/blob_store_configs/
```

### C. References are bare-keyed in the map, digest-bearing on disk

Per FDR-0008 Phase 2 (`docs/plans/2026-05-27-config-digest-pins-phase2.md`,
"implementation notes #4"):

- `blob_store_id.Id.String()` returns the **bare** `[prefix]name`
  form — this is the `BlobStoreMap` key. Discovery never produces
  digest-bearing IDs, so every map key is bare.
- `blob_store_id.Id.Canonical()` returns the digest-bearing
  `name@blake2b256-…` wire form. This is what a multi config stores.
- `id.Set("default@blake2b256-…")` parses the suffix;
  `id.GetDigest()` returns the `markl.Id`; `id.HasDigest()` reports
  presence; `id.WithDigest(d)` sets it; `id.MarshalText()` /
  `UnmarshalText()` delegate to `Canonical()` / `Set()` and are the
  on-disk wire form (Phase 2 note #4).

**Reference parsing happens at hyphence decode time, not in the
factory** (design decision, 2026-05-31). The multi config's reference
fields are typed `blob_store_id.Id` / `[]blob_store_id.Id`, so the
hyphence/tommy coder parses each ref via `UnmarshalText` during
decode and a malformed digest fails the decode. A decode-time
`Validate()` step additionally rejects any reference that is **not**
digest-bearing (bare refs are globally legal but forbidden inside a
multi config). Consequently the `ConfigMulti` accessors return
already-parsed `blob_store_id.Id` values and **never return errors**.

At store-map build time the factory does only the two things that
require the populated map: it looks each reference up by its bare key
(`refId.String()`) and **asserts** `refId.GetDigest()` against the
resolved store's `Config.BlobDigest`. This mirrors the resolver seam
at `go/internal/foxtrot/blob_store_env/main.go` `GetBlobStore`.
Digest-mismatch, dangling-reference, and not-yet-built are runtime
construction conditions (they depend on the *other* configs), so
those errors stay in the factory; format and bare-ref errors live at
decode.

### D. `markl.AssertEqual` needs pointers

`markl.AssertEqual` takes `domain_interfaces.MarklId`; `markl.Id`
satisfies it only via pointer receiver. Call as
`markl.AssertEqual(&a, &b)`. Mirror the pattern in
`go/internal/delta/blob_store_configs/verify.go` and Phase 2's
resolver wiring. `markl.Equals` compares `(format, data)` only (not
purpose), so the config-side digest (purpose dropped on the wire,
notes #1) and an ID-side digest compare equal when bytes match.

### E. Build/test recipes

- Go tests carry the required `test` build tag via `just test-go`
  (a bare `go test ./...` produces spurious `undefined: ui.T`).
  Run a single package as `just test-go ./internal/<pkg>/...`,
  a single test as `just test-go -run TestName ./internal/<pkg>/...`.
- bats: `just test-bats-tags <tag>` runs one `# bats file_tags=`
  group. New bats files must be `git add`-ed before nix sees them
  (nix build over a dirty tree only includes git-tracked files).
- Cheap compile check of one package: `hamster.go-build` with
  `packages: ./internal/<pkg>/...`.
- `tommy` codegen: structs marked `//go:generate tommy generate`
  produce `Decode*`/`Encode*` helpers. Regenerate with
  `go generate ./internal/charlie/blob_store_configs/...` (confirm
  the exact invocation by reading an existing generated file's header
  and the package's `//go:generate` directives first).

### F. The `Multi` builder API (do not modify the primitive)

`go/internal/foxtrot/blob_stores/multi_builder.go`:

- `NewMulti(ctx interfaces.ActiveContext) *MultiBuilder` (line 38)
- `(*MultiBuilder).Mirror(stores ...BlobStoreInitialized) *MultiBuilder` (line 45)
- `(*MultiBuilder).WriteTo(store BlobStoreInitialized) *MultiBuilder` (line 61)
- `(*MultiBuilder).Read(stores ...BlobStoreInitialized) *MultiBuilder` (line 81)
- `(*MultiBuilder).ReadFill(enabled bool) *MultiBuilder` (line 91)
- `(*MultiBuilder).Build() (Multi, error)` (line 103)

`Multi` satisfies `domain_interfaces.BlobStore` by **value**
(`multi.go:34`). The variadic element type is `BlobStoreInitialized`
— exactly what `blobStores[key]` yields, so references pass straight
through.

### G. Promotion is two-stage; this plan targets `experimental` only

FDR-0009 `promotion-criteria`:
- → `experimental`: `init-multi` exists, a multi round-trips through
  the CLI, and the four canonical bats scenarios pass (mirror;
  write_through + read_fill; write_through no read_fill; nested
  multi-of-multi). **Tasks 1–8 deliver this.**
- → `accepted`: the ad-hoc `blobFromRemainingStores` fallback in
  `cat`/`has` is removed and a downstream consumer adopts a multi
  default. **Out of scope here** — see "Promotion to accepted" at the
  end. Do not remove `blobFromRemainingStores` in this plan.

---

## Task 1: Register the `!toml-blob_store_config-multi-v0` type id

**Promotion criteria:** N/A (additive, locked vocabulary).

**Files:**
- Modify: `go/internal/0/ids/types_builtin.go` (add const + append to
  the `init()` slice)
- Test: `go/internal/0/ids/types_builtin_test.go` (create if absent)

**Step 1: Write the failing test**

Create or append `go/internal/0/ids/types_builtin_test.go`:

```go
//go:build test

package ids

import "testing"

func TestMultiV0Registered(t *testing.T) {
	bt := GetOrPanic(TypeTomlBlobStoreConfigMultiV0)
	if bt.TypeStruct.String() != TypeTomlBlobStoreConfigMultiV0 {
		t.Fatalf("round-trip: got %q, want %q",
			bt.TypeStruct.String(), TypeTomlBlobStoreConfigMultiV0)
	}
}
```

**Step 2: Run test to verify it fails**

```
just test-go -run TestMultiV0Registered ./internal/0/ids/...
```

Expected: `undefined: TypeTomlBlobStoreConfigMultiV0` compile error.

**Step 3: Add the constant**

In `go/internal/0/ids/types_builtin.go`, add to the const block
(after line 19, the `*VCurrent` aliases):

```go
	TypeTomlBlobStoreConfigMultiV0 = "!toml-blob_store_config-multi-v0"
```

**Step 4: Register it in the init() slice**

In the same file, append to the slice in `init()` (after line 43,
`TypeTomlBlobStoreConfigS3V0`):

```go
		TypeTomlBlobStoreConfigMultiV0,
```

**Step 5: Run test to verify it passes**

```
just test-go -run TestMultiV0Registered ./internal/0/ids/...
```

Expected: PASS.

**Step 6: Commit**

```bash
git add go/internal/0/ids/types_builtin.go \
        go/internal/0/ids/types_builtin_test.go
git commit -m "ids: register !toml-blob_store_config-multi-v0 type

FDR-0009 Step 3. Locked wire-format tag for the multi store config
type. Refs: #217.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 2: `TomlMultiV0` config struct + `ConfigMulti` interface

This task authors the charlie-layer on-disk struct, its typed
reference accessors, the `read-fill` default, the `Validate()`
digest-bearing check (run at decode in Task 3), and the `ConfigMulti`
marker interface that the factory switches on. References are typed
`blob_store_id.Id` parsed by the coder at decode time, so the
accessors return parsed values and never error (Orientation note C);
digest assertion and store lookup live in the factory (Task 4).

**Promotion criteria:** N/A — additive type.

**Files:**
- Create: `go/internal/charlie/blob_store_configs/toml_multi_v0.go`
- Modify: `go/internal/charlie/blob_store_configs/main.go` (add the
  `ConfigMulti` interface to the type block, after `ConfigPointer`
  at line 104-107)
- Test: `go/internal/charlie/blob_store_configs/toml_multi_v0_test.go`

**Step 1: Write the failing test**

Create `go/internal/charlie/blob_store_configs/toml_multi_v0_test.go`:

```go
//go:build test

package blob_store_configs

import (
	"testing"

	"code.linenisgreat.com/madder/go/internal/alfa/blob_store_id"
	_ "code.linenisgreat.com/madder/go/internal/charlie/markl_registrations"
)

func mustId(t *testing.T, s string) blob_store_id.Id {
	t.Helper()
	var id blob_store_id.Id
	if err := id.Set(s); err != nil {
		t.Fatalf("Set(%q): %v", s, err)
	}
	return id
}

func TestTomlMultiV0_Accessors(t *testing.T) {
	readFill := true
	cfg := TomlMultiV0{
		Mode:       "write_through",
		WriteStore: mustId(t, "default@blake2b256-9ft3m74lwx9aq4nx7vrft96xeku0scrz8ymyljh9phpvfzv4kdgsmwxsws"),
		ReadStores: []blob_store_id.Id{mustId(t, "archive@blake2b256-2k4p9r3mwx9aq4nx7vrft96xeku0scrz8ymyljh9phpvfzv4kdgsm")},
		ReadFill:   &readFill,
	}

	if cfg.GetBlobStoreType() != "multi" {
		t.Errorf("GetBlobStoreType = %q, want multi", cfg.GetBlobStoreType())
	}
	if cfg.GetMode() != "write_through" {
		t.Errorf("GetMode = %q", cfg.GetMode())
	}
	if cfg.GetWriteStore().GetName() != "default" {
		t.Errorf("GetWriteStore name = %q", cfg.GetWriteStore().GetName())
	}
	if got := cfg.GetReadStores(); len(got) != 1 || !got[0].HasDigest() {
		t.Errorf("GetReadStores = %v", got)
	}
	if !cfg.GetReadFill() {
		t.Error("GetReadFill = false, want true")
	}
}

func TestTomlMultiV0_ReadFillDefaultsTrue(t *testing.T) {
	// Nil ReadFill (key absent) defaults to true per FDR-0009.
	cfg := TomlMultiV0{Mode: "write_through"}
	if !cfg.GetReadFill() {
		t.Error("GetReadFill with nil field = false, want true (default)")
	}
}

func TestTomlMultiV0_ValidateRejectsBareRef(t *testing.T) {
	// A reference with no @digest is forbidden inside a multi config.
	cfg := TomlMultiV0{
		Mode:         "mirror",
		MirrorStores: []blob_store_id.Id{mustId(t, "default")}, // bare
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted a bare reference, want error")
	}
}

func TestTomlMultiV0_ValidateAcceptsDigestBearing(t *testing.T) {
	cfg := TomlMultiV0{
		Mode:         "mirror",
		MirrorStores: []blob_store_id.Id{mustId(t, "default@blake2b256-9ft3m74lwx9aq4nx7vrft96xeku0scrz8ymyljh9phpvfzv4kdgsmwxsws")},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate rejected a digest-bearing ref: %v", err)
	}
}

func TestTomlMultiV0_SatisfiesConfigMulti(t *testing.T) {
	var _ ConfigMulti = TomlMultiV0{}
}
```

**Step 2: Run test to verify it fails**

```
just test-go -run TestTomlMultiV0 ./internal/charlie/blob_store_configs/...
```

Expected: `undefined: TomlMultiV0` / `undefined: ConfigMulti`.

**Step 3: Add the `ConfigMulti` interface**

In `go/internal/charlie/blob_store_configs/main.go`, add to the type
block right after the `ConfigPointer` interface (line 107):

```go
	// ConfigMulti is a blob_store-config that composes other stores
	// via the Multi primitive. References are typed blob_store_id.Id
	// values, parsed by the hyphence coder at decode time and
	// validated as digest-bearing by Validate() (also at decode), so
	// the accessors never return errors. The store-map factory does
	// only lookup + digest assertion. See FDR-0009.
	ConfigMulti interface {
		Config
		GetMode() string                    // "mirror" | "write_through"
		GetWriteStore() blob_store_id.Id     // write_through; zero otherwise
		GetReadStores() []blob_store_id.Id   // write_through; nil otherwise
		GetMirrorStores() []blob_store_id.Id // mirror; nil otherwise
		GetReadFill() bool                  // defaults true; mirror ignores
	}
```

**Step 4: Implement the struct**

Create `go/internal/charlie/blob_store_configs/toml_multi_v0.go`:

```go
package blob_store_configs

import (
	"code.linenisgreat.com/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

//go:generate tommy generate
type TomlMultiV0 struct {
	Mode         string             `toml:"mode"`
	WriteStore   blob_store_id.Id   `toml:"write-store,omitempty"`
	ReadStores   []blob_store_id.Id `toml:"read-stores,omitempty"`
	MirrorStores []blob_store_id.Id `toml:"mirror-stores,omitempty"`
	// ReadFill is a pointer so an absent key (nil) can default to
	// true (FDR-0009). A present `read-fill = false` disables tee.
	ReadFill *bool `toml:"read-fill,omitempty"`
}

func (TomlMultiV0) GetBlobStoreType() string {
	return "multi"
}

func (cfg TomlMultiV0) GetMode() string {
	return cfg.Mode
}

func (cfg TomlMultiV0) GetWriteStore() blob_store_id.Id {
	return cfg.WriteStore
}

func (cfg TomlMultiV0) GetReadStores() []blob_store_id.Id {
	return cfg.ReadStores
}

func (cfg TomlMultiV0) GetMirrorStores() []blob_store_id.Id {
	return cfg.MirrorStores
}

// GetReadFill defaults to true when the key is absent (FDR-0009).
func (cfg TomlMultiV0) GetReadFill() bool {
	return cfg.ReadFill == nil || *cfg.ReadFill
}

// Validate enforces the FDR-0009 invariant that every reference inside
// a multi config is digest-bearing. It is called by the hyphence Coder
// at decode time (Task 3), so a hand-edited config with a bare
// reference fails to read. Malformed digests are caught earlier by
// blob_store_id.Id.UnmarshalText during decode.
func (cfg TomlMultiV0) Validate() error {
	check := func(role string, id blob_store_id.Id) error {
		if !id.HasDigest() {
			return errors.BadRequestf(
				"multi %s reference %q must be digest-bearing "+
					"(name@blake2b256-…); bare references are forbidden "+
					"inside a multi config", role, id.String())
		}
		return nil
	}
	if !cfg.WriteStore.IsEmpty() {
		if err := check("write-store", cfg.WriteStore); err != nil {
			return err
		}
	}
	for _, id := range cfg.ReadStores {
		if err := check("read-store", id); err != nil {
			return err
		}
	}
	for _, id := range cfg.MirrorStores {
		if err := check("mirror-store", id); err != nil {
			return err
		}
	}
	return nil
}

// SetFlagDefinitions makes TomlMultiV0 a ConfigMutable so the generic
// init machinery accepts it. init-multi assembles the config from its
// own typed flags (Task 6) rather than these, so this is intentionally
// minimal; the fields are not user-settable via the generic flag path.
func (cfg *TomlMultiV0) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
}
```

> NOTE: confirm the `toml:` tag spelling against neighboring configs
> (Orientation note B). If the repo uses a different omitempty
> convention or a custom tommy tag form, match it — the bats
> round-trip in Task 6 will catch a mismatch, but matching up front
> saves a cycle.

> NOTE: `tommy generate` emits `DecodeTomlMultiV0` / `EncodeTomlMultiV0`
> from the struct. Run `go generate` for this package (Orientation
> note E) and `git add` the generated file. If tommy chokes on the
> `*bool` field, fall back to a plain `bool` with an explicit
> `ReadFillSet bool` companion — but try `*bool` first.

> NOTE: the typed `blob_store_id.Id` reference fields rely on tommy/
> toml honoring `encoding.TextMarshaler` / `TextUnmarshaler` (Phase 2
> gave `Id` a `MarshalText` that renders the digest-bearing
> `Canonical()` form and an `UnmarshalText` that parses it). **Verify
> this before relying on it:** encode a `TomlMultiV0` with a typed ref
> and confirm the on-disk form is `write-store = "name@blake2b256-…"`.
> If tommy does NOT invoke TextMarshaler for struct/slice fields, fall
> back to `[]string` reference fields and have `Validate()` (and a
> decode-time parse cached into unexported typed fields) own the
> conversion — keeping the accessors error-free either way. The
> typed-field approach is preferred; this is the documented fallback.

> NOTE: `blob_store_id.Id.IsEmpty()` is assumed by `Validate` for the
> write-store presence check (the inventory-archive config uses the
> same `GetLooseBlobStoreId().IsEmpty()` shape). Confirm `IsEmpty()`
> exists; if not, use `GetName() == ""`.

**Step 5: Run the generator, then the test**

```
go generate ./internal/charlie/blob_store_configs/...
just test-go -run TestTomlMultiV0 ./internal/charlie/blob_store_configs/...
```

Expected: PASS.

**Step 6: Commit**

```bash
git add go/internal/charlie/blob_store_configs/toml_multi_v0.go \
        go/internal/charlie/blob_store_configs/toml_multi_v0_test.go \
        go/internal/charlie/blob_store_configs/main.go \
        go/internal/charlie/blob_store_configs/*multi*gen*.go
git commit -m "blob_store_configs: add TomlMultiV0 + ConfigMulti interface

FDR-0009 Step 3. On-disk multi config struct with typed
blob_store_id.Id references parsed by the hyphence coder at decode
time; Validate() enforces the digest-bearing rule at decode;
ConfigMulti accessors are error-free. Refs: #217.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 3: Delta layer — reexport, Coder entry, `TypeStructForConfig`

Wire `TomlMultiV0`/`ConfigMulti` into the delta facade so the
hyphence Coder can encode/decode the new tag and `EncodeWithDigest`/
`DecodeAndVerify` round-trip it.

**Promotion criteria:** N/A — additive.

**Files:**
- Modify: `go/internal/delta/blob_store_configs/main.go` (reexports +
  `TypeStructForConfig` case + interface-satisfaction check)
- Modify: `go/internal/delta/blob_store_configs/coding.go` (Coder map
  entry)
- Test: `go/internal/delta/blob_store_configs/digest_test.go` (add a
  multi round-trip case alongside the existing Phase-1 tests)

**Step 1: Write the failing round-trip test**

Append to `go/internal/delta/blob_store_configs/digest_test.go`:

```go
func mustBlobStoreId(t *testing.T, s string) blob_store_id.Id {
	t.Helper()
	var id blob_store_id.Id
	if err := id.Set(s); err != nil {
		t.Fatalf("Set(%q): %v", s, err)
	}
	return id
}

func TestEncodeWithDigest_MultiRoundTrip(t *testing.T) {
	readFill := true
	typedConfig := &TypedConfig{
		Type: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigMultiV0).TypeStruct,
		Blob: &TomlMultiV0{
			Mode:       "write_through",
			WriteStore: mustBlobStoreId(t, "default@blake2b256-9ft3m74lwx9aq4nx7vrft96xeku0scrz8ymyljh9phpvfzv4kdgsmwxsws"),
			ReadStores: []blob_store_id.Id{mustBlobStoreId(t, "archive@blake2b256-2k4p9r3mwx9aq4nx7vrft96xeku0scrz8ymyljh9phpvfzv4kdgsm")},
			ReadFill:   &readFill,
		},
	}

	var buf bytes.Buffer
	if _, err := EncodeWithDigest(typedConfig, &buf); err != nil {
		t.Fatalf("EncodeWithDigest: %v", err)
	}

	decoded := &TypedConfig{}
	if _, err := DecodeAndVerify(decoded, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("DecodeAndVerify: %v", err)
	}

	multi, ok := decoded.Blob.(ConfigMulti)
	if !ok {
		t.Fatalf("decoded blob is %T, want ConfigMulti", decoded.Blob)
	}
	if multi.GetMode() != "write_through" {
		t.Errorf("GetMode = %q", multi.GetMode())
	}
	if !multi.GetReadFill() {
		t.Error("GetReadFill = false, want true")
	}
	if decoded.BlobDigest.IsNull() {
		t.Error("BlobDigest null after round-trip")
	}
}

// TestDecodeAndVerify_RejectsBareMultiRef: a multi config whose
// reference lacks a digest must fail at decode — Validate() runs in
// the Coder's Decode closure (Step 5). Encoding a bare typed Id
// renders the bare wire form, which decode then rejects.
func TestDecodeAndVerify_RejectsBareMultiRef(t *testing.T) {
	typedConfig := &TypedConfig{
		Type: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigMultiV0).TypeStruct,
		Blob: &TomlMultiV0{
			Mode:         "mirror",
			MirrorStores: []blob_store_id.Id{mustBlobStoreId(t, "default")}, // bare
		},
	}

	var buf bytes.Buffer
	if _, err := EncodeWithDigest(typedConfig, &buf); err != nil {
		t.Fatalf("EncodeWithDigest: %v", err)
	}

	decoded := &TypedConfig{}
	if _, err := DecodeAndVerify(decoded, bytes.NewReader(buf.Bytes())); err == nil {
		t.Fatal("expected decode to reject bare multi reference, got nil")
	}
}
```

Ensure `ids` and `blob_store_id` are imported in the test file
(`ids` is, for the Phase-1 tests; add `blob_store_id` if missing).

**Step 2: Run test to verify it fails**

```
just test-go -run TestEncodeWithDigest_MultiRoundTrip ./internal/delta/blob_store_configs/...
```

Expected: `undefined: TomlMultiV0` / `undefined: ConfigMulti`, or — once
reexports land but the Coder entry doesn't — a decode error "no
coders available for type !toml-blob_store_config-multi-v0".

**Step 3: Add the reexports + interface check**

In `go/internal/delta/blob_store_configs/main.go`:

- In the type block (after `ConfigPointer` at line 27):

```go
	ConfigMulti = charlie_bsc.ConfigMulti
```

- In the type block (after `TomlPointerV1` at line 47):

```go
	TomlMultiV0 = charlie_bsc.TomlMultiV0
```

- In the Decode reexports var block (after `DecodeTomlPointerV1` at
  line 83):

```go
	DecodeTomlMultiV0 = charlie_bsc.DecodeTomlMultiV0
```

- In the interface-satisfaction var block (after the `TomlPointerV1`
  checks at line 107):

```go
	_ ConfigMulti   = TomlMultiV0{}
	_ ConfigMutable = &TomlMultiV0{}
```

**Step 4: Add the `TypeStructForConfig` case**

In `TypeStructForConfig` (line 150-186), add before the `default`
(after the pointer cases at line 171):

```go
	case *TomlMultiV0, TomlMultiV0:
		typeId = ids.TypeTomlBlobStoreConfigMultiV0
```

**Step 5: Add the Coder map entry**

In `go/internal/delta/blob_store_configs/coding.go`, add an entry to
the `Blob` map mirroring the `TypeTomlBlobStoreConfigPointerV1` entry
(read that entry first for the exact `CoderTommy[...]` shape). Use
`charlie_bsc.DecodeTomlMultiV0` in the Decode closure and the
matching Encode helper:

```go
		ids.TypeTomlBlobStoreConfigMultiV0: hyphence.CoderTommy[Config, *Config]{
			Decode: func(b []byte) (Config, error) {
				doc, err := charlie_bsc.DecodeTomlMultiV0(b)
				if err != nil {
					return nil, err
				}
				cfg := doc.Data()
				// FDR-0009: enforce digest-bearing references at decode
				// time, so the accessors and the factory can assume
				// every reference is valid and digest-bearing.
				if v, ok := cfg.(interface{ Validate() error }); ok {
					if err := v.Validate(); err != nil {
						return nil, err
					}
				}
				return cfg, nil
			},
			Encode: /* mirror the pointer-v1 Encode closure exactly */,
		},
```

> NOTE: do not hand-write the Encode closure from memory — copy the
> `TypeTomlBlobStoreConfigPointerV1` entry verbatim and swap the type
> name. The `CoderTommy` generic params and the `doc.Data()` /
> encode-side shape must match the package's existing convention or
> the Coder won't compile.

> NOTE: the inline `interface{ Validate() error }` assertion keeps
> `Validate` off the exported `Config` interface — only multi configs
> implement it, and only the decode closure calls it. This is the
> decode-time seam that makes the bare-ref test in Step 1 pass.

**Step 6: Run the round-trip test + package suite**

```
just test-go -run TestEncodeWithDigest_MultiRoundTrip ./internal/delta/blob_store_configs/...
just test-go ./internal/delta/... ./internal/charlie/...
```

Expected: PASS.

**Step 7: Commit**

```bash
git add go/internal/delta/blob_store_configs/main.go \
        go/internal/delta/blob_store_configs/coding.go \
        go/internal/delta/blob_store_configs/digest_test.go
git commit -m "blob_store_configs: wire TomlMultiV0 into the delta Coder

FDR-0009 Step 3. Reexport ConfigMulti/TomlMultiV0/DecodeTomlMultiV0,
add the Coder map entry and TypeStructForConfig case, and prove the
EncodeWithDigest/DecodeAndVerify round-trip. Refs: #217.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 4: Factory — `makeMultiStore` + `case ConfigMulti`

The heart of the FDR: turn a resolved `ConfigMulti` plus a populated
`BlobStoreMap` into a built `Multi`. Reference resolution, digest
assertion, and the builder call all live here.

**Promotion criteria:** N/A — additive factory case. Reachable only
when a `multi` config exists on disk.

**Files:**
- Create: `go/internal/foxtrot/blob_stores/multi_factory.go`
- Modify: `go/internal/foxtrot/blob_stores/main.go` (add the
  `case blob_store_configs.ConfigMulti:` to `MakeBlobStore`'s switch,
  before `default` at line 434)
- Test: `go/internal/foxtrot/blob_stores/multi_factory_test.go`

**Step 1: Write the failing test**

Create `go/internal/foxtrot/blob_stores/multi_factory_test.go`. The
test builds two in-memory leaf stores, stamps each with a config
digest, places them in a `BlobStoreMap`, then resolves a
`ConfigMulti` referencing them and asserts the returned store is a
`Multi`:

```go
//go:build test

package blob_stores

import (
	"testing"

	"code.linenisgreat.com/madder/go/internal/alfa/blob_store_id"
	"code.linenisgreat.com/madder/go/internal/bravo/markl"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	_ "code.linenisgreat.com/madder/go/internal/charlie/markl_registrations"
)

// Helper: a BlobStoreInitialized whose Config carries a given digest
// and whose BlobStore field is already non-nil (i.e. "built"). Use the
// lightest available test double for the BlobStore — check
// multi_test.go for the in-memory store helper #182 introduced and
// reuse it rather than hand-rolling.
func builtLeafForTest(t *testing.T, name string, digestSeed byte) BlobStoreInitialized {
	t.Helper()
	// TODO(impl): construct via the #182 in-memory store helper.
	// Set bs.Config.BlobDigest to a blake2b256 markl.Id seeded by
	// digestSeed so the factory's AssertEqual has something to match.
	panic("replace with the multi_test.go in-memory store helper")
}

func TestMakeMultiStore_WriteThrough(t *testing.T) {
	write := builtLeafForTest(t, "default", 0x01)
	read := builtLeafForTest(t, "archive", 0x02)

	stores := MakeBlobStoreMap(write, read)

	readFill := true
	cfg := &blob_store_configs.TomlMultiV0{
		Mode:       "write_through",
		WriteStore: write.Path.GetId().WithDigest(write.Config.BlobDigest),
		ReadStores: []blob_store_id.Id{read.Path.GetId().WithDigest(read.Config.BlobDigest)},
		ReadFill:   &readFill,
	}

	store, err := makeMultiStore(testCtx(t), cfg, stores)
	if err != nil {
		t.Fatalf("makeMultiStore: %v", err)
	}
	if _, ok := store.(Multi); !ok {
		t.Fatalf("got %T, want Multi", store)
	}
}

func TestMakeMultiStore_DigestMismatchRefuses(t *testing.T) {
	write := builtLeafForTest(t, "default", 0x01)
	stores := MakeBlobStoreMap(write)

	// Reference the right name but the WRONG digest.
	var wrong markl.Id
	_ = wrong.SetMarklId(markl.FormatIdHashBlake2b256, bytesSeeded(0xFF))

	cfg := &blob_store_configs.TomlMultiV0{
		Mode:         "mirror",
		MirrorStores: []blob_store_id.Id{write.Path.GetId().WithDigest(wrong)},
	}

	if _, err := makeMultiStore(testCtx(t), cfg, stores); err == nil {
		t.Fatal("expected digest-mismatch error, got nil")
	}
}
```

> NOTE: the bare-reference case is a **decode-time** test now
> (`TestDecodeAndVerify_RejectsBareMultiRef` in Task 3), not a factory
> test — the factory receives already-parsed, already-validated
> `blob_store_id.Id` values, so it never sees a bare ref in practice.

> NOTE: the test scaffolding (`testCtx`, `bytesSeeded`, the in-memory
> store helper) must reuse what `multi_test.go` already established in
> #182 — read that file first and import/copy its helpers rather than
> inventing parallel ones. If a helper is unexported and in the same
> package, you can call it directly (this test is `package blob_stores`).

**Step 2: Run test to verify it fails**

```
just test-go -run TestMakeMultiStore ./internal/foxtrot/blob_stores/...
```

Expected: `undefined: makeMultiStore`.

**Step 3: Implement `makeMultiStore`**

Create `go/internal/foxtrot/blob_stores/multi_factory.go`:

```go
package blob_stores

import (
	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/alfa/blob_store_id"
	"code.linenisgreat.com/madder/go/internal/bravo/markl"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

// resolveMultiRef looks a reference up in the map by its bare key and
// asserts the reference's digest against the resolved store's Phase-1
// config digest. References arrive already parsed and validated as
// digest-bearing by decode-time Validate() (Task 2/3), so this does
// not re-parse or re-check the format.
//
// Returns ErrMultiRefNotReady when the named store exists but has not
// been built yet (BlobStore == nil) — the construction loop uses this
// to defer. Returns a hard error for a dangling ref (name not in the
// map), a legacy/undigested target, or a digest mismatch.
func resolveMultiRef(
	refId blob_store_id.Id,
	blobStores BlobStoreMap,
) (BlobStoreInitialized, error) {
	resolved, ok := blobStores[refId.String()]
	if !ok {
		return BlobStoreInitialized{}, errors.BadRequestf(
			"multi store references %q which is not present in any "+
				"configured XDG scope", refId.Canonical())
	}
	if resolved.BlobStore == nil {
		return BlobStoreInitialized{}, ErrMultiRefNotReady{Ref: refId.Canonical()}
	}

	configDigest := resolved.Config.BlobDigest
	if configDigest.IsNull() {
		return BlobStoreInitialized{}, errors.BadRequestf(
			"multi reference %q targets an unmigrated config (no "+
				"digest); run `madder config-pin_digest %s` first",
			refId.Canonical(), refId.String())
	}

	idDigest := refId.GetDigest()
	if err := markl.AssertEqual(&idDigest, &configDigest); err != nil {
		return BlobStoreInitialized{}, errors.Wrapf(err,
			"multi reference %q digest does not match resolved store",
			refId.Canonical())
	}

	return resolved, nil
}

// makeMultiStore builds a Multi from a ConfigMulti and a populated
// store map. Every reference must already be built; resolveMultiRef
// surfaces ErrMultiRefNotReady otherwise so the caller can defer.
func makeMultiStore(
	ctx interfaces.ActiveContext,
	config blob_store_configs.ConfigMulti,
	blobStores BlobStoreMap,
) (store domain_interfaces.BlobStore, err error) {
	builder := NewMulti(ctx)

	switch config.GetMode() {
	case "mirror":
		refs := config.GetMirrorStores()
		if len(refs) == 0 {
			return store, errors.BadRequestf(
				"multi mirror mode requires at least one mirror-store")
		}
		mirrors := make([]BlobStoreInitialized, 0, len(refs))
		for _, refId := range refs {
			resolved, e := resolveMultiRef(refId, blobStores)
			if e != nil {
				return store, e
			}
			mirrors = append(mirrors, resolved)
		}
		builder = builder.Mirror(mirrors...)

	case "write_through":
		writeId := config.GetWriteStore()
		if writeId.IsEmpty() {
			return store, errors.BadRequestf(
				"multi write_through mode requires a write-store")
		}
		writeStore, e := resolveMultiRef(writeId, blobStores)
		if e != nil {
			return store, e
		}
		builder = builder.WriteTo(writeStore)

		reads := make([]BlobStoreInitialized, 0, len(config.GetReadStores()))
		for _, refId := range config.GetReadStores() {
			resolved, e := resolveMultiRef(refId, blobStores)
			if e != nil {
				return store, e
			}
			reads = append(reads, resolved)
		}
		builder = builder.Read(reads...).ReadFill(config.GetReadFill())

	default:
		return store, errors.BadRequestf(
			"multi store has invalid mode %q (want mirror or "+
				"write_through)", config.GetMode())
	}

	built, err := builder.Build()
	if err != nil {
		return store, errors.Wrap(err)
	}

	return built, nil
}
```

Create the sentinel error in the same file (or a sibling
`multi_factory_errors.go`):

```go
// ErrMultiRefNotReady signals that a referenced store exists in the
// map but has not been built yet. The store-map construction loop
// treats it as "defer to the next iteration", not a hard failure.
type ErrMultiRefNotReady struct {
	Ref string
}

func (e ErrMultiRefNotReady) Error() string {
	return "multi reference not yet built: " + e.Ref
}

func (e ErrMultiRefNotReady) Is(target error) bool {
	_, ok := target.(ErrMultiRefNotReady)
	return ok
}
```

**Step 4: Add the factory switch case**

In `go/internal/foxtrot/blob_stores/main.go`, add before `default`
(line 434):

```go
	case blob_store_configs.ConfigMulti:
		return makeMultiStore(
			envDir.GetActiveContext(),
			config,
			blobStores,
		)
```

**Step 5: Run the tests**

```
just test-go -run TestMakeMultiStore ./internal/foxtrot/blob_stores/...
```

Expected: PASS (write-through builds; digest mismatch refuses). The
bare-reference case is a decode-time test in Task 3, not here.

**Step 6: Commit**

```bash
git add go/internal/foxtrot/blob_stores/multi_factory.go \
        go/internal/foxtrot/blob_stores/multi_factory_test.go \
        go/internal/foxtrot/blob_stores/main.go
git commit -m "blob_stores: resolve ConfigMulti into a built Multi

FDR-0009 Step 3. makeMultiStore looks up each (already-parsed,
already-validated) reference, asserts its digest against the resolved
store's Phase-1 config digest (markl.AssertEqual), and drives the
Multi builder. ErrMultiRefNotReady lets the construction loop defer
not-yet-built references. Refs: #217.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 5: Construction — fixpoint pass for nested multis

The existing `MakeBlobStores` is two passes: leaves, then
single-level cross-ref dependents (inventory archives). Multis can
reference other multis (unbounded depth), so they need a fixpoint
loop. This task: (a) skip multis in passes 1 and 2 so the archive
path is byte-unchanged, (b) add a **third pass** that builds multis
in dependency order, deferring on `ErrMultiRefNotReady` and erroring
on no-progress (dangling ref).

**Promotion criteria:** N/A — additive third pass; reverting restores
the two-pass behavior and a multi-free repo is unaffected.

**Files:**
- Modify: `go/internal/foxtrot/blob_stores/main.go` (passes 1-2 skip
  condition + new pass 3, in `MakeBlobStores` lines 167-216)
- Test: `go/internal/foxtrot/blob_stores/multi_construction_test.go`

**Step 1: Write the failing test**

Create `go/internal/foxtrot/blob_stores/multi_construction_test.go`.
Two cases: a nested `tiered → fast(mirror) → {ssd, nvme}` graph all
build; a multi with a dangling reference returns an error naming the
missing ref. Build the `BlobStoreMap` with leaf stores already built
and multi stores with `BlobStore == nil` and a `ConfigMulti` blob,
then call the new exported helper that runs only the multi pass (see
Step 3 — factor the multi pass into `buildMultiStores(ctx,
blobStores)` so it's unit-testable without a full env/disk).

```go
//go:build test

package blob_stores

import (
	"testing"

	_ "code.linenisgreat.com/madder/go/internal/charlie/markl_registrations"
)

func TestBuildMultiStores_Nested(t *testing.T) {
	ssd := builtLeafForTest(t, ".ssd", 0x01)
	nvme := builtLeafForTest(t, ".nvme", 0x02)

	fast := multiLeafForTest(t, "fast", &blob_store_configs.TomlMultiV0{
		Mode: "mirror",
		MirrorStores: []blob_store_id.Id{
			digestRef(ssd), digestRef(nvme),
		},
	})
	tiered := multiLeafForTest(t, "tiered", &blob_store_configs.TomlMultiV0{
		Mode:       "write_through",
		WriteStore: digestRef(fast), // reference the (as-yet unbuilt) multi
	})

	stores := MakeBlobStoreMap(ssd, nvme, fast, tiered)

	if err := buildMultiStores(testCtx(t), stores); err != nil {
		t.Fatalf("buildMultiStores: %v", err)
	}
	for _, name := range []string{"fast", "tiered"} {
		if stores[name].BlobStore == nil {
			t.Errorf("%q not built", name)
		}
	}
}

func TestBuildMultiStores_DanglingRef(t *testing.T) {
	orphan := multiLeafForTest(t, "orphan", &blob_store_configs.TomlMultiV0{
		Mode:         "mirror",
		MirrorStores: []blob_store_id.Id{mustId(t, "ghost@blake2b256-9ft3m74lwx9aq4nx7vrft96xeku0scrz8ymyljh9phpvfzv4kdgsmwxsws")},
	})
	stores := MakeBlobStoreMap(orphan)

	err := buildMultiStores(testCtx(t), stores)
	if err == nil {
		t.Fatal("expected dangling-ref error, got nil")
	}
}
```

> NOTE: `multiLeafForTest` constructs a `BlobStoreInitialized` with
> `BlobStore == nil`, a `ConfigNamed.Path` whose id is `name`, and a
> `Config` whose `.Blob` is the given `*TomlMultiV0` and whose
> `.BlobDigest` is a non-null seeded digest (so a parent multi can
> assert against it). `digestRef(bs)` returns the typed
> `bs.Path.GetId().WithDigest(bs.Config.BlobDigest)` (a
> `blob_store_id.Id`, not a string). Reuse the Task-4 helpers; this
> test file also needs the `blob_store_id` import and the `mustId`
> helper (copy it from Task 2's test or factor it into a shared
> test helper).

**Step 2: Run test to verify it fails**

```
just test-go -run TestBuildMultiStores ./internal/foxtrot/blob_stores/...
```

Expected: `undefined: buildMultiStores`.

**Step 3: Implement the fixpoint pass and skip multis in passes 1-2**

In `go/internal/foxtrot/blob_stores/main.go`:

First, make pass 1 (lines 173-192) skip multis too. Change the
existing skip guard at line 176:

```go
		if _, needsCrossRef := blobStore.Config.Blob.(blob_store_configs.ConfigInventoryArchive); needsCrossRef {
			continue
		}
		if _, isMulti := blobStore.Config.Blob.(blob_store_configs.ConfigMulti); isMulti {
			continue
		}
```

Next, make pass 2 (lines 194-213) skip multis. Add after line 197
(`if blobStore.BlobStore != nil { continue }`):

```go
		if _, isMulti := blobStore.Config.Blob.(blob_store_configs.ConfigMulti); isMulti {
			continue
		}
```

Then, after pass 2 (before `return blobStores` at line 215), call the
new pass:

```go
	if err := buildMultiStores(ctx, blobStores); err != nil {
		ctx.Cancel(err)
		return blobStores
	}
```

Finally, add the fixpoint function (new, e.g. in
`multi_factory.go`):

```go
// buildMultiStores materializes every ConfigMulti in blobStores in
// dependency order. Each iteration builds any multi whose references
// are all resolved; it loops until an iteration makes no progress.
// Because digest-bearing references form a Merkle DAG, a no-progress
// iteration with unbuilt multis remaining means a dangling reference
// (cycles are unrepresentable). The aggregated deferral errors name
// the offending references.
func buildMultiStores(
	ctx interfaces.ActiveContext,
	blobStores BlobStoreMap,
) error {
	for {
		progressed := false
		var deferred []error

		for key := range blobStores {
			blobStore := blobStores[key]
			if blobStore.BlobStore != nil {
				continue
			}
			config, isMulti := blobStore.Config.Blob.(blob_store_configs.ConfigMulti)
			if !isMulti {
				continue
			}

			built, err := makeMultiStore(ctx, config, blobStores)
			if err != nil {
				if errors.Is(err, ErrMultiRefNotReady{}) {
					deferred = append(deferred, errors.Wrapf(err,
						"multi %q", key))
					continue
				}
				return errors.Wrapf(err, "multi %q", key)
			}

			blobStore.BlobStore = built
			blobStores[key] = blobStore
			progressed = true
		}

		if len(deferred) == 0 {
			return nil // all multis built
		}
		if !progressed {
			// No multi advanced this pass and some remain unbuilt:
			// the only Merkle-DAG-consistent cause is a dangling
			// reference (a ref to a store that is itself an unbuilt
			// multi blocked on the same condition, transitively
			// bottoming out at a missing name).
			return errors.Join(deferred...)
		}
	}
}
```

> NOTE: confirm `errors.Join` (or the dewey errors package's
> equivalent) is available; if not, fold the deferred errors into one
> with `errors.Errorf` listing each. `errors.Is(err,
> ErrMultiRefNotReady{})` relies on the `Is` method added in Task 4 —
> verify the dewey `errors.Is` honors custom `Is` (it wraps stdlib;
> it does).

**Step 4: Run the tests + package suite**

```
just test-go -run TestBuildMultiStores ./internal/foxtrot/blob_stores/...
just test-go ./internal/foxtrot/...
```

Expected: PASS.

**Step 5: Commit**

```bash
git add go/internal/foxtrot/blob_stores/main.go \
        go/internal/foxtrot/blob_stores/multi_factory.go \
        go/internal/foxtrot/blob_stores/multi_construction_test.go
git commit -m "blob_stores: build multis via a dependency-order fixpoint pass

FDR-0009 Step 3. Passes 1-2 now skip ConfigMulti; a new third pass
loops, building any multi whose references are all resolved, until
no progress is made. Nested multi-of-multi resolves across
iterations; a dangling reference surfaces as an aggregated error.
The inventory-archive path is byte-unchanged. Refs: #217.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 6: `madder init-multi` command + canonical bats scenarios

Authoring surface. Mirrors the `init-*` family
(`go/internal/india/commands/init.go`,
`go/internal/golf/command_components/init.go`) but assembles a
`TomlMultiV0` from typed flags and resolves each bare reference to its
leaf's current digest before emitting the digest-bearing form.

**Promotion criteria:** This command IS the `experimental` gate. Once
it lands and the four bats scenarios pass, FDR-0009 promotes to
`experimental` (Task 8).

**Files:**
- Create: `go/internal/india/commands/init_multi.go`
- Create: `zz-tests_bats/init_multi.bats`
- Test: `zz-tests_bats/init_multi.bats` (the four canonical scenarios)

**Step 1: Write the failing bats scenarios**

Create `zz-tests_bats/init_multi.bats`. These four cases are the
FDR-0009 `experimental` promotion bar (Orientation note G). Use
`.`-prefixed store ids — `init_store` writes `.default` (CWD), notes
#6:

```bash
setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=init_multi

# Scenario 1: mirror across two local leaves.
function init_multi_mirror { # @test
  init_store
  run_madder init -encryption none .ssd
  assert_success
  run_madder init -encryption none .nvme
  assert_success

  run_madder init-multi .fanout --mode mirror \
    --mirror-store .ssd --mirror-store .nvme
  assert_success

  local config=".madder/local/share/blob_stores/fanout/blob_store-config"
  [[ -f $config ]] || fail "expected config at $config"
  run grep -E '^mode = "mirror"' "$config"
  assert_success
  # references are digest-bearing on disk
  run grep -E 'blake2b256-' "$config"
  assert_success

  # the multi composes transparently through list
  run_madder list
  assert_success
}

# Scenario 2: write_through WITH read_fill.
function init_multi_write_through_read_fill { # @test
  init_store
  run_madder init -encryption none .archive
  assert_success

  run_madder init-multi .cache --mode write_through \
    --write-store .default --read-store .archive --read-fill
  assert_success

  local config=".madder/local/share/blob_stores/cache/blob_store-config"
  run grep -E '^read-fill = true' "$config"
  assert_success
}

# Scenario 3: write_through WITHOUT read_fill.
function init_multi_write_through_no_read_fill { # @test
  init_store
  run_madder init -encryption none .archive
  assert_success

  run_madder init-multi .cache --mode write_through \
    --write-store .default --read-store .archive --no-read-fill
  assert_success

  local config=".madder/local/share/blob_stores/cache/blob_store-config"
  run grep -E '^read-fill = false' "$config"
  assert_success
}

# Scenario 4: nested multi-of-multi.
function init_multi_nested { # @test
  init_store
  run_madder init -encryption none .ssd
  assert_success
  run_madder init -encryption none .nvme
  assert_success
  run_madder init -encryption none .tape
  assert_success

  run_madder init-multi .fast --mode mirror \
    --mirror-store .ssd --mirror-store .nvme
  assert_success
  run_madder init-multi .tiered --mode write_through \
    --write-store .fast --read-store .tape --read-fill
  assert_success

  # the whole graph resolves at load time
  run_madder list
  assert_success
}
```

**Step 2: Run the bats tag to verify it fails**

```
git add zz-tests_bats/init_multi.bats
just test-bats-tags init_multi
```

Expected: FAIL — `init-multi` not registered.

**Step 3: Implement the command**

Create `go/internal/india/commands/init_multi.go`. Read
`go/internal/india/commands/init.go` (the `Init` command struct +
registration + `Run`) and `init_from.go` first and mirror their
shape. Key pieces:

```go
package commands

import (
	"fmt"
	"os"

	"code.linenisgreat.com/madder/go/internal/0/ids"
	"code.linenisgreat.com/madder/go/internal/alfa/blob_store_id"
	"code.linenisgreat.com/madder/go/internal/charlie/tap"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/madder/go/internal/futility"
	"code.linenisgreat.com/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

func init() {
	utility.AddCmd("init-multi", &InitMulti{})
}

type InitMulti struct {
	command_components.EnvBlobStore
	command_components.Init

	mode         string
	writeStore   string
	readStores   repeatedString
	mirrorStores repeatedString
	readFill     bool
	noReadFill   bool
}

func (cmd *InitMulti) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "blob-store-id",
			Description: "identifier for the new multi store",
			Required:    true,
		},
	}
}

func (cmd InitMulti) GetDescription() futility.Description {
	return futility.Description{
		Short: "compose existing stores into a multi blob store",
		Long: "Creates a multi blob_store-config that mirrors writes " +
			"across stores or writes through to one store with read " +
			"fallback (and optional cache fill). References are " +
			"recorded as digest-bearing blob-store-ids. See " +
			"docs/features/0009-multi-store-config-type.md.",
	}
}

func (cmd *InitMulti) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.StringVar(&cmd.mode, "mode", "",
		"mirror | write_through")
	flagSet.StringVar(&cmd.writeStore, "write-store", "",
		"write target (write_through mode)")
	flagSet.Var(&cmd.readStores, "read-store",
		"read source (write_through; repeatable)")
	flagSet.Var(&cmd.mirrorStores, "mirror-store",
		"mirror member (mirror mode; repeatable)")
	flagSet.BoolVar(&cmd.readFill, "read-fill", false,
		"tee read-source hits into the write store (write_through)")
	flagSet.BoolVar(&cmd.noReadFill, "no-read-fill", false,
		"disable read-fill")
}

func (cmd *InitMulti) Run(req futility.Request) {
	var blobStoreId blob_store_id.Id
	if err := blobStoreId.Set(req.PopArg("blob-store-id")); err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
	}
	req.AssertNoMoreArgs()

	envBlobStore := cmd.MakeEnvBlobStore(req)

	cfg := &blob_store_configs.TomlMultiV0{Mode: cmd.mode}

	resolve := func(ref string) blob_store_id.Id {
		// Resolve a bare name to its leaf's current digest; pass a
		// digest-bearing ref through unchanged. The typed Id renders
		// to the digest-bearing wire form via MarshalText -> Canonical
		// at encode time.
		var id blob_store_id.Id
		if err := id.Set(ref); err != nil {
			errors.ContextCancelWithBadRequestError(req, err)
		}
		if id.HasDigest() {
			return id
		}
		leaf := envBlobStore.GetBlobStore(id)
		digest := leaf.Config.BlobDigest
		if digest.IsNull() {
			req.Cancel(errors.BadRequestf(
				"reference %q targets an unmigrated config; run "+
					"`madder config-pin_digest %s` first", ref, ref))
			return blob_store_id.Id{}
		}
		return id.WithDigest(digest)
	}

	switch cmd.mode {
	case "mirror":
		for _, ref := range cmd.mirrorStores {
			cfg.MirrorStores = append(cfg.MirrorStores, resolve(ref))
		}
	case "write_through":
		cfg.WriteStore = resolve(cmd.writeStore)
		for _, ref := range cmd.readStores {
			cfg.ReadStores = append(cfg.ReadStores, resolve(ref))
		}
		readFill := !cmd.noReadFill // default true unless --no-read-fill
		cfg.ReadFill = &readFill
	default:
		req.Cancel(errors.BadRequestf(
			"--mode must be mirror or write_through, got %q", cmd.mode))
		return
	}

	tw := tap.NewWriter(os.Stdout)
	pathConfig := cmd.InitBlobStore(
		req,
		envBlobStore,
		blobStoreId,
		&blob_store_configs.TypedConfig{
			Type: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigMultiV0).TypeStruct,
			Blob: cfg,
		},
	)
	tw.Ok(fmt.Sprintf("init-multi %s", pathConfig.GetConfig()))
	tw.Plan()
}

// repeatedString is a flag.Value that appends on each occurrence,
// enabling --read-store a --read-store b.
type repeatedString []string

func (r *repeatedString) String() string  { return fmt.Sprint([]string(*r)) }
func (r *repeatedString) Set(v string) error {
	*r = append(*r, v)
	return nil
}
```

> NOTE: verify the repeated-flag mechanism. `flagSet.Var` expects the
> dewey `flags.Value` interface — confirm its method set
> (`String() string`, `Set(string) error`) matches `repeatedString`.
> If futility/dewey already ships a string-slice flag value (grep
> `pkgs/values` and `futility` for `Slice`/`Repeated`), use it instead
> of hand-rolling. The `values.String` import for the positional arg
> and the `tap` import must match what `init.go` actually uses — copy
> its import block.

> NOTE: `cmd.InitBlobStore` calls `EncodeWithDigest`
> (`go/internal/golf/command_components/init.go:60`), so the new
> multi config gets its own Phase-1 `@` digest line for free. No
> extra wiring needed for the config to be assertable by a parent
> multi.

**Step 4: Run the bats scenarios**

```
just test-bats-tags init_multi
```

Expected: all four PASS.

**Step 5: Run the full bats suite for regressions**

```
just test-bats-tags init
just test-bats-tags list
```

Expected: PASS (existing init/list behavior unchanged).

**Step 6: Commit**

```bash
git add go/internal/india/commands/init_multi.go \
        zz-tests_bats/init_multi.bats
git commit -m "commands: add 'madder init-multi'

FDR-0009 Step 3. Authors a multi blob_store-config from typed flags;
resolves bare references to the leaf's current Phase-1 digest and
emits the digest-bearing form. Four canonical bats scenarios (mirror,
write_through +/- read_fill, nested multi-of-multi) cover the
experimental promotion bar. Refs: #217.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 7: `madder list -tree` + multi fields in ndjson/json

Surface the multi graph. `list` already shows config digests
(FDR-0008 Phase 1). Add a `-tree` bool flag that walks the multi
reference graph, plus `mode`/`read_fill`/`refs` fields in structured
output.

**Promotion criteria:** N/A — display only.

**Files:**
- Modify: `go/internal/india/commands/list.go` (flag, `listRecord`
  struct lines 63-70, `makeListRecord` 163-177, text emitter 96-132)
- Test: `zz-tests_bats/list.bats` (extend with multi cases)

**Step 1: Write the failing bats test**

Append to `zz-tests_bats/list.bats`:

```bash
function list_tree_renders_multi_graph { # @test
  init_store
  run_madder init -encryption none .archive
  assert_success
  run_madder init-multi .cache --mode write_through \
    --write-store .default --read-store .archive --read-fill
  assert_success

  run_madder list -tree
  assert_success
  assert_output --partial 'multi'
  assert_output --partial 'write_through'
  # the tree shows the referenced leaves under the multi
  assert_output --partial '.archive'
}

function list_ndjson_multi_fields { # @test
  init_store
  run_madder init -encryption none .archive
  assert_success
  run_madder init-multi .cache --mode write_through \
    --write-store .default --read-store .archive --read-fill
  assert_success

  run_madder list -format=ndjson
  assert_success
  assert_output --partial '"mode":"write_through"'
  assert_output --partial '"read_fill":true'
}
```

**Step 2: Run the tests to verify they fail**

```
just test-bats-tags list
```

Expected: FAIL — no `-tree` flag, no `mode`/`read_fill` fields.

**Step 3: Add the `-tree` flag + structured fields**

In `go/internal/india/commands/list.go`:

- Add a `Tree bool` field to the `List` struct (lines 22-26).
- In `SetFlagDefinitions` (57-61):

```go
	flagSet.BoolVar(&cmd.Tree, "tree", false,
		"render the multi-store reference graph")
```

- Extend `listRecord` (63-70):

```go
	Mode     string          `json:"mode,omitempty"`
	ReadFill *bool           `json:"read_fill,omitempty"`
	Refs     []listRecordRef `json:"refs,omitempty"`
```

with:

```go
type listRecordRef struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
	Role   string `json:"role"` // "write" | "read" | "mirror"
}
```

- In `makeListRecord` (163-177), after the digest block, detect a
  multi config and populate the new fields:

```go
	if multi, ok := blobStore.Config.Blob.(blob_store_configs.ConfigMulti); ok {
		rec.Mode = multi.GetMode()
		switch multi.GetMode() {
		case "mirror":
			for _, id := range multi.GetMirrorStores() {
				rec.Refs = append(rec.Refs, makeRef(id, "mirror"))
			}
		case "write_through":
			rec.Refs = append(rec.Refs, makeRef(multi.GetWriteStore(), "write"))
			for _, id := range multi.GetReadStores() {
				rec.Refs = append(rec.Refs, makeRef(id, "read"))
			}
			rf := multi.GetReadFill()
			rec.ReadFill = &rf
		}
	}
```

where `makeRef(id blob_store_id.Id, role string) listRecordRef` splits
the already-parsed id into `Name` (`id.String()`) and `Digest`
(`id.GetDigest().String()`). No parsing or error handling — the ids
are typed and digest-bearing by construction (decode-time Validate).

- For `-tree`: when `cmd.Tree` is set, the text emitter recurses. Add
  a `emitListTree` function that, for each top-level multi, prints the
  store then walks `Refs`, looking each referenced name up in the
  same `BlobStoreMap` and indenting children (FDR example lines
  279-284 shows the target shape). Keep it simple — a recursive
  helper that prints `<indent>└── <name>  <description>  (<role>)`.
  Non-`-tree` output is unchanged.

> NOTE: the FDR's ASCII tree (lines 279-289) is the visual target,
> not a literal format contract. Match its spirit (indented children,
> role annotations); exact box-drawing characters are the
> implementer's call. The bats tests assert on substrings
> (`multi`, `write_through`, `.archive`), not the exact glyphs, so
> there's latitude.

**Step 4: Run the tests**

```
just test-bats-tags list
```

Expected: PASS.

**Step 5: Commit**

```bash
git add go/internal/india/commands/list.go zz-tests_bats/list.bats
git commit -m "list: -tree flag + multi mode/read_fill/refs in structured output

FDR-0009 Step 3. madder list -tree walks the multi reference graph;
ndjson/json gain mode, read_fill, and refs[{name,digest,role}] for
multi stores. Non-multi output is unchanged. Refs: #217.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Task 8: Docs + promote FDR-0009 to experimental

**Files:**
- Modify: `docs/man.7/blob-store-multi.md` (re-scope to cover the
  config type, not just the primitive)
- Modify: `docs/features/0009-multi-store-config-type.md` (status
  `proposed` → `experimental`)
- Modify: `docs/man.7/blob-store.md` (cross-reference the multi type
  in the store-type list, if it enumerates types)

**Step 1: Re-scope `blob-store-multi(7)`**

The current page (commit `adb88b2`, #195) covers the in-process
primitive only and explicitly defers the config type to FDR-0009
(see its DESCRIPTION, "This page covers the primitive only."). Add a
new section after the primitive coverage:

```markdown
# CONFIG TYPE

A multi blob store can be persisted as a `blob_store-config` with the
type tag **!toml-blob_store_config-multi-v0**. Author one with
**madder init-multi** (see its OPTIONS). The on-disk body carries a
`mode` (`mirror` or `write_through`), the referenced stores as
**digest-bearing** blob-store-ids
(*name*@*hash*-*digest*), and — for write_through — a `read-fill`
boolean.

References MUST be digest-bearing: the digest pins the referenced
config's content (FDR-0008 Phase 2), which makes the reference graph
a Merkle DAG — cycles are unrepresentable, so no runtime
cycle-detection exists or is needed. At load time each reference's
digest is asserted against the resolved store's config digest; a
mismatch (e.g. after editing a referenced leaf) is a hard error,
remedied by re-authoring the multi.

When a multi is the default store, every command composes through it
transparently; **madder list -tree** is the only surface that renders
the graph.
```

**Step 2: Promote the FDR**

In `docs/features/0009-multi-store-config-type.md` frontmatter,
change `status: proposed` to `status: experimental` once Tasks 1-7
are merged and the four canonical bats scenarios pass.

**Step 3: Commit**

```bash
git add docs/man.7/blob-store-multi.md \
        docs/man.7/blob-store.md \
        docs/features/0009-multi-store-config-type.md
git commit -m "docs: cover multi config type; promote FDR-0009 to experimental

FDR-0009 Step 3. Re-scopes blob-store-multi(7) to document the
config-type wrapper and init-multi authoring; promotes the FDR from
proposed to experimental now that init-multi exists and the four
canonical scenarios pass. Refs: #217.

:clown: with [Clown](https://github.com/amarbel-llc/clown)"
```

---

## Out of scope — promotion to `accepted` (separate future work)

Per FDR-0009 `promotion-criteria`, `accepted` requires two things
that this plan deliberately does **not** do:

1. **Remove `blobFromRemainingStores`** from `cat.go` (lines 240-272)
   and the `GetDefaultBlobStoreAndRemaining` fallback in `has.go`
   (line 147). These ad-hoc fallback walks stay in place during the
   `experimental` phase — removing them would force every fallback
   user onto a multi config before the config type has soaked. The
   removal is its own PR, gated on:
2. **A downstream consumer (dodder or cutting-garden) adopting a
   multi default.** Dodder's FDR-0015 is the natural first adopter
   (it already consumes the `Multi` primitive as a library; #195
   context).

File these as a follow-up issue under umbrella #217 when Tasks 1-8
land, and add a TaskCreate entry tracking it (per the user's
mid-sequence-followup convention). Do not pre-emptively remove the
fallback in this plan.

Also out of scope (FDR *Future Work* / *Limitations*): `madder
config-rebuild_multi` (auto re-mint dependent multis when a leaf
rotates), `madder default <id>` (atomic default switch), mirror-mode
quorum, and read-fill quotas. None block `experimental`.

---

## Execution order summary

| Task | Deliverable | Test gate |
|------|-------------|-----------|
| 1 | `!toml-blob_store_config-multi-v0` type id | go: id round-trip |
| 2 | `TomlMultiV0` struct + `ConfigMulti` interface | go: accessors + read-fill default |
| 3 | Delta Coder entry + `TypeStructForConfig` | go: round-trip + bare-ref rejected at decode |
| 4 | `makeMultiStore` + factory case | go: build, digest-mismatch |
| 5 | Fixpoint construction pass | go: nested, dangling-ref |
| 6 | `madder init-multi` | bats: 4 canonical scenarios |
| 7 | `list -tree` + structured fields | bats: tree + ndjson |
| 8 | Docs + FDR → experimental | (review) |

Tasks 1→5 are a strict dependency chain (each compiles on the prior).
Task 6 depends on 1-5. Task 7 depends on 2 (the `ConfigMulti`
accessors) and 6 (something to list). Task 8 is last.
