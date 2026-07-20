# Multi blob-store builder + read-through cache fill — implementation plan

> **For Claude:** REQUIRED SUB-SKILL: Use eng:subagent-driven-development to implement this plan task-by-task.

**Goal:** Consolidate `Multi` (broadcast-write fanout) with a new write-through-cache mode (1 write target + N read sources + tee-during-read cache-fill) into a single type with a builder, widen to full `BlobStore`, and reach it from the CLI via an opt-in `-multi` flag.

**Architecture:** Single `Multi` type with an internal mode enum (`modeMirror` / `modeWriteThrough`). Builder commits the caller to exactly one mode at chain time. Tee path uses `atomic.Bool` to serialize commit between caller's `Close` and `ctx.After` fallback. AllBlobs is an N-way merge via `iter.Pull` with same-byte-MarklId dedupe.

**Tech Stack:** Go 1.23+ (`iter.Pull`), `interfaces.ActiveContext.After` from dewey, bats-core for end-to-end tests.

**Rollback:** No existing callers of `Multi` (lowercase fields, no exported constructor — confirmed zero compile-time call sites). No wire-format changes; pure in-process composition. Downstream callers rollback by changing the construction line. CLI rollback: drop `-multi` from invocation.

**Design doc:** `docs/plans/2026-05-13-multi-blob-store-builder-design.md` (commit `45c6aac`).

---

## Conventions used throughout

**Build/test commands** (run from repo root unless noted):
- Go unit tests: `just test-go` (runs `cd go && go test -tags test ./...`). The `test` build tag is required — bare `go test ./...` produces spurious `undefined: ui.T` failures (per user-global memory).
- Go unit tests, single test: `cd go && go test -tags test -run '^TestX$' ./internal/foxtrot/blob_stores/`
- Race detector: `just test-go-race`
- Vet: `just vet-go`
- Build: `just build-go`
- bats suite: `just test-bats` (depends on `build`)
- bats single file: `just zz-tests_bats/test-targets multi.bats`
- Coverage (Go only, the slice we care about): `cd go && go test -tags test -coverprofile=/tmp/cov.out -coverpkg=./internal/foxtrot/blob_stores/ ./internal/foxtrot/blob_stores/ && go tool cover -func=/tmp/cov.out | tail -1`
- Coverage (combined Go + bats): `just cover-merged` then `just cover-summary`
- Regenerate `pkgs/` facade after public name changes: `just generate-facades` (uses dagnabit)

**Commit signoff:** Per Clown identity convention, every commit ends with:
```
Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>
```

**Files this plan touches:**
- Create: `go/internal/foxtrot/blob_stores/multi_builder.go`
- Create: `go/internal/foxtrot/blob_stores/multi_builder_test.go`
- Create: `go/internal/foxtrot/blob_stores/multi_test.go` (Mirror-mode behavior; expand existing if one exists)
- Create: `go/internal/foxtrot/blob_stores/multi_tee.go`
- Create: `go/internal/foxtrot/blob_stores/multi_tee_test.go`
- Create: `go/internal/foxtrot/blob_stores/multi_test_helpers.go` (controllable reader + spy writer; `//go:build test`)
- Modify: `go/internal/foxtrot/blob_stores/multi.go` (consolidated `Multi` type + mode-aware methods)
- Modify: `go/internal/0/domain_interfaces/blob_store.go` (doc-only — sort contract for `AllBlobs`)
- Modify: `go/internal/india/commands/cat.go` (`-multi`, `-no-read-fill` flags)
- Modify: `go/internal/india/commands/has.go` (`-multi` flag)
- Modify: `go/internal/india/commands/list.go` (`-multi` flag)
- Modify: `go/internal/india/commands/fsck.go` (`-multi` flag)
- Regenerate: `go/pkgs/blob_stores/main.go` (via `just generate-facades`)
- Create: `zz-tests_bats/multi.bats` (bats end-to-end)

---

## Task ordering and dependencies

```
Task 1 (doc-only AllBlobs sort) ─────► Task 7 (depends on documented contract)
Task 2 (test helpers) ─────────────► Tasks 3–12 (all Go unit tests consume)
Task 3 (builder Mirror happy path) ─► Tasks 4–8 (Mirror behavior)
Task 4 (builder error paths)
Task 5 (Mirror HasBlob/Reader/Writer)
Task 6 (Mirror BlobStore widening)
Task 7 (AllBlobs N-way merge, same-hash)
Task 8 (AllBlobs cross-hash pass-through)
Task 9 (WriteTo+Read, ReadFill=off)  ─► Task 10
Task 10 (Tee eager-close commit)     ─► Task 11
Task 11 (Tee ctx.After completion)   ─► Task 12
Task 12 (Cross-hash mapping in tee)
Task 13 (regenerate pkgs facade)     ─► Tasks 14–17
Task 14 (CLI cat -multi + bats)
Task 15 (CLI has -multi + bats)
Task 16 (CLI list -multi + bats)
Task 17 (CLI fsck -multi + bats)
Task 18 (coverage verification + gap fill)
```

The smallest first cycle that produces a green build + green tests is **Task 1** (doc-only). The smallest *code-producing* cycle is **Task 3** (builder Mirror happy path — wraps today's behavior under a new constructor).

---

## Task 1: Document `AllBlobs` sort contract

**Promotion criteria:** N/A (additive doc change).

**Files:**
- Modify: `go/internal/0/domain_interfaces/blob_store.go:83-91`

**Step 1: Read the existing interface block**

Run: `cat go/internal/0/domain_interfaces/blob_store.go | sed -n '83,91p'`
Expected: shows the `BlobStore` interface with `AllBlobs() interfaces.SeqError[MarklId]`.

**Step 2: Add the sort-contract doc comment**

Edit `go/internal/0/domain_interfaces/blob_store.go` at the `BlobStore` interface to document `AllBlobs`:

```go
BlobStore interface {
    BlobAccess
    BlobIOWrapperGetter

    GetBlobStoreDescription() string
    GetDefaultHashType() FormatHash
    GetBlobStoreConfig() BlobStoreConfig
    // AllBlobs yields every blob id this store holds. Implementations
    // MUST yield ids in ascending lexicographic byte order of the
    // MarklId representation (which embeds the hash-format tag).
    // The sort is load-bearing for callers that N-way merge across
    // stores (see blob_stores.Multi). Filesystem-backed stores
    // satisfy this naturally via filepath.WalkDir's lexical order.
    AllBlobs() interfaces.SeqError[MarklId]
}
```

**Step 3: Run vet + tests to confirm doc-only change**

Run: `just vet-go && just test-go`
Expected: PASS (no behavior change).

**Step 4: Commit**

```bash
git add go/internal/0/domain_interfaces/blob_store.go
git commit -m "docs(domain_interfaces): document AllBlobs sort contract

The N-way merge in Multi (see docs/plans/2026-05-13-multi-blob-store-builder-design.md)
relies on each child's AllBlobs yielding lex-ordered MarklIds.
Filesystem-backed stores satisfy this via filepath.WalkDir; the
contract is pinned here so future implementations can't drift.

Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>"
```

---

## Task 2: Add `multi_test_helpers.go`

**Promotion criteria:** N/A (test-only).

**Files:**
- Create: `go/internal/foxtrot/blob_stores/multi_test_helpers.go`

**Step 1: Write the helper file**

Create `go/internal/foxtrot/blob_stores/multi_test_helpers.go`:

```go
//go:build test

package blob_stores

import (
    "io"
    "sync"
    "sync/atomic"

    "code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
)

// controllableBlobReader yields bytes on demand. Tests call Feed(...)
// to make the next Read return those bytes; Close marks EOF.
type controllableBlobReader struct {
    mu      sync.Mutex
    queued  [][]byte
    closed  atomic.Bool
    id      domain_interfaces.MarklId
    onClose func()
}

func newControllableBlobReader(id domain_interfaces.MarklId) *controllableBlobReader {
    return &controllableBlobReader{id: id}
}

func (r *controllableBlobReader) Feed(b []byte) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.queued = append(r.queued, append([]byte(nil), b...))
}

func (r *controllableBlobReader) Read(p []byte) (int, error) {
    r.mu.Lock()
    if len(r.queued) == 0 {
        r.mu.Unlock()
        if r.closed.Load() {
            return 0, io.EOF
        }
        return 0, nil
    }
    chunk := r.queued[0]
    n := copy(p, chunk)
    if n < len(chunk) {
        r.queued[0] = chunk[n:]
    } else {
        r.queued = r.queued[1:]
    }
    r.mu.Unlock()
    return n, nil
}

func (r *controllableBlobReader) Close() error {
    r.closed.Store(true)
    if r.onClose != nil {
        r.onClose()
    }
    return nil
}

func (r *controllableBlobReader) GetMarklId() domain_interfaces.MarklId { return r.id }

// Stub the remaining BlobReader methods (ReadAt, Seek, WriteTo) with
// minimal no-ops — tests that need them will be added later.
func (r *controllableBlobReader) ReadAt(p []byte, off int64) (int, error) {
    return 0, io.EOF
}
func (r *controllableBlobReader) Seek(offset int64, whence int) (int64, error) {
    return 0, nil
}
func (r *controllableBlobReader) WriteTo(w io.Writer) (int64, error) {
    var total int64
    buf := make([]byte, 4096)
    for {
        n, err := r.Read(buf)
        if n > 0 {
            nw, _ := w.Write(buf[:n])
            total += int64(nw)
        }
        if err == io.EOF {
            return total, nil
        }
        if err != nil {
            return total, err
        }
    }
}

// spyBlobWriter records Write/Close calls. failAfterBytes > 0 causes
// Write to return an error once the cumulative byte count reaches it.
type spyBlobWriter struct {
    mu              sync.Mutex
    received        []byte
    closed          atomic.Bool
    closeErr        error
    failAfterBytes  int
    bytesWritten    int
    computedId      domain_interfaces.MarklId
}

func (w *spyBlobWriter) Write(p []byte) (int, error) {
    w.mu.Lock()
    defer w.mu.Unlock()
    if w.failAfterBytes > 0 && w.bytesWritten+len(p) > w.failAfterBytes {
        return 0, io.ErrShortWrite
    }
    w.received = append(w.received, p...)
    w.bytesWritten += len(p)
    return len(p), nil
}

func (w *spyBlobWriter) ReadFrom(r io.Reader) (int64, error) {
    return io.Copy(struct{ io.Writer }{w}, r)
}

func (w *spyBlobWriter) Close() error {
    w.closed.Store(true)
    return w.closeErr
}

func (w *spyBlobWriter) GetMarklId() domain_interfaces.MarklId { return w.computedId }
```

**Step 2: Confirm it compiles**

Run: `just test-go`
Expected: PASS (helpers compile under `-tags test`).

**Step 3: Commit**

```bash
git add go/internal/foxtrot/blob_stores/multi_test_helpers.go
git commit -m "test(blob_stores): add controllable reader + spy writer helpers for Multi tests

Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>"
```

---

## Task 3: Builder skeleton — Mirror happy path

**Promotion criteria:** N/A.

**Files:**
- Create: `go/internal/foxtrot/blob_stores/multi_builder.go`
- Create: `go/internal/foxtrot/blob_stores/multi_builder_test.go`
- Modify: `go/internal/foxtrot/blob_stores/multi.go` (add `mode` field + `modeMirror` enum value; existing methods become Mirror-mode branches but behavior is preserved)

**Step 1: Write the failing test**

Create `go/internal/foxtrot/blob_stores/multi_builder_test.go`:

```go
package blob_stores

import (
    "testing"
)

func TestBuilder_Mirror_HappyPath(t *testing.T) {
    ctx := &spyActiveContext{} // reuse from store_remote_sftp_test.go
    storeA := &stubBlobStore{} // reuse from store_inventory_archive_test.go
    storeB := &stubBlobStore{}

    m, err := NewMulti(ctx).Mirror(storeA, storeB).Build()
    if err != nil {
        t.Fatalf("Build: unexpected error: %v", err)
    }
    if m.mode != modeMirror {
        t.Fatalf("mode: got %v, want modeMirror", m.mode)
    }
}
```

**Step 2: Run test, verify FAIL**

Run: `cd go && go test -tags test -run TestBuilder_Mirror_HappyPath ./internal/foxtrot/blob_stores/`
Expected: FAIL with "undefined: NewMulti" (or similar).

**Step 3: Write minimal implementation**

Create `go/internal/foxtrot/blob_stores/multi_builder.go`:

```go
package blob_stores

import (
    "code.linenisgreat.com/purse-first/libs/dewey/0/interfaces"
)

type MultiBuilder struct {
    ctx         interfaces.ActiveContext
    mode        multiMode
    mirrorStores []BlobStoreInitialized
    writeStore  BlobStoreInitialized
    readStores  []BlobStoreInitialized
    readFill    bool
}

func NewMulti(ctx interfaces.ActiveContext) *MultiBuilder {
    return &MultiBuilder{ctx: ctx, readFill: true}
}

func (b *MultiBuilder) Mirror(stores ...BlobStoreInitialized) *MultiBuilder {
    b.mode = modeMirror
    b.mirrorStores = stores
    return b
}

func (b *MultiBuilder) Build() (Multi, error) {
    // Minimal first implementation: build a Mirror Multi.
    return Multi{
        ctx:         b.ctx,
        mode:        b.mode,
        childStores: b.mirrorStores,
    }, nil
}
```

Modify `go/internal/foxtrot/blob_stores/multi.go` — add the mode enum + field. The struct now looks like:

```go
type multiMode int

const (
    modeUnset multiMode = iota
    modeMirror
    modeWriteThrough
)

type Multi struct {
    ctx         interfaces.ActiveContext
    mode        multiMode
    childStores []BlobStoreInitialized  // mirror children (mode=mirror)
    // writeStore + readStores + readFill added later in Task 9
}
```

**Step 4: Run test, verify PASS**

Run: `cd go && go test -tags test -run TestBuilder_Mirror_HappyPath ./internal/foxtrot/blob_stores/`
Expected: PASS.

**Step 5: Run the full Go test suite**

Run: `just test-go`
Expected: PASS (we've not broken any existing tests — `Multi` still satisfies `BlobAccess` because its fields are populated in Mirror mode).

**Step 6: Commit**

```bash
git add go/internal/foxtrot/blob_stores/multi_builder.go \
        go/internal/foxtrot/blob_stores/multi_builder_test.go \
        go/internal/foxtrot/blob_stores/multi.go
git commit -m "feat(blob_stores): add NewMulti builder with Mirror happy path

Single-type, runtime-mode-flag design. Mirror mode preserves today's
broadcast-write semantics; WriteTo+Read mode lands in later tasks.

Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>"
```

---

## Task 4: Builder error paths

**Promotion criteria:** N/A.

**Files:**
- Modify: `go/internal/foxtrot/blob_stores/multi_builder.go`
- Modify: `go/internal/foxtrot/blob_stores/multi_builder_test.go`

**Step 1: Write failing tests for each error path**

Append to `multi_builder_test.go`:

```go
func TestBuilder_Build_EmptyStores(t *testing.T) {
    _, err := NewMulti(&spyActiveContext{}).Build()
    if err == nil {
        t.Fatal("expected error for empty stores; got nil")
    }
}

func TestBuilder_Build_ModeConfusion_MirrorThenRead(t *testing.T) {
    s := &stubBlobStore{}
    _, err := NewMulti(&spyActiveContext{}).Mirror(s).Read(s).Build()
    if err == nil {
        t.Fatal("expected mode-confusion error; got nil")
    }
}

func TestBuilder_Build_ModeConfusion_WriteToThenMirror(t *testing.T) {
    s := &stubBlobStore{}
    _, err := NewMulti(&spyActiveContext{}).WriteTo(s).Mirror(s).Build()
    if err == nil {
        t.Fatal("expected mode-confusion error; got nil")
    }
}

func TestBuilder_Build_WriteStoreInReadList(t *testing.T) {
    s := &stubBlobStore{}
    _, err := NewMulti(&spyActiveContext{}).WriteTo(s).Read(s).Build()
    if err == nil {
        t.Fatal("expected error for write-store-also-in-read-list; got nil")
    }
}

func TestBuilder_Build_ReadFillAfterMirror(t *testing.T) {
    s := &stubBlobStore{}
    _, err := NewMulti(&spyActiveContext{}).Mirror(s).ReadFill(false).Build()
    if err == nil {
        t.Fatal("expected error for ReadFill after Mirror; got nil")
    }
}
```

**Step 2: Run, verify FAIL**

Run: `cd go && go test -tags test -run TestBuilder_Build_ ./internal/foxtrot/blob_stores/`
Expected: FAIL — `WriteTo`, `Read`, `ReadFill` methods don't exist yet.

**Step 3: Implement the methods and error paths**

In `multi_builder.go`, add:

```go
func (b *MultiBuilder) WriteTo(store BlobStoreInitialized) *MultiBuilder {
    if b.mode == modeUnset || b.mode == modeWriteThrough {
        b.mode = modeWriteThrough
    } else {
        b.mode = -1 // mark as confused
    }
    b.writeStore = store
    return b
}

func (b *MultiBuilder) Read(stores ...BlobStoreInitialized) *MultiBuilder {
    if b.mode != modeWriteThrough {
        b.mode = -1
    }
    b.readStores = append(b.readStores, stores...)
    return b
}

func (b *MultiBuilder) ReadFill(enabled bool) *MultiBuilder {
    if b.mode != modeWriteThrough {
        b.mode = -1
    }
    b.readFill = enabled
    return b
}

func (b *MultiBuilder) Build() (Multi, error) {
    switch b.mode {
    case modeMirror:
        if len(b.mirrorStores) == 0 {
            return Multi{}, errors.Errorf("Mirror: no stores given")
        }
        return Multi{ctx: b.ctx, mode: modeMirror, childStores: b.mirrorStores}, nil
    case modeWriteThrough:
        if b.writeStore.BlobStore == nil { // adjust to actual BlobStoreInitialized shape
            return Multi{}, errors.Errorf("WriteTo: no write store given")
        }
        // write-store-in-read-list check
        for _, r := range b.readStores {
            if storeIdsEqual(r, b.writeStore) {
                return Multi{}, errors.Errorf("write store also appears in read list")
            }
        }
        return Multi{
            ctx:        b.ctx,
            mode:       modeWriteThrough,
            writeStore: b.writeStore,
            readStores: b.readStores,
            readFill:   b.readFill,
        }, nil
    case modeUnset:
        return Multi{}, errors.Errorf("no mode selected; call .Mirror or .WriteTo")
    default:
        return Multi{}, errors.Errorf("mode confusion: only one of .Mirror or .WriteTo is allowed")
    }
}
```

Also add `writeStore` and `readStores`/`readFill` fields to `Multi` in `multi.go` (these will be used in later tasks):

```go
type Multi struct {
    ctx         interfaces.ActiveContext
    mode        multiMode
    childStores []BlobStoreInitialized  // mirror mode
    writeStore  BlobStoreInitialized    // write-through mode
    readStores  []BlobStoreInitialized  // write-through mode
    readFill    bool                    // write-through mode
}
```

Implementer note: `storeIdsEqual` needs to compare `BlobStoreInitialized` by id. Use the same key as `BlobStoreMap` — likely `.Path.GetId().String()` based on `has.go:158`.

**Step 4: Run, verify PASS**

Run: `cd go && go test -tags test -run TestBuilder_Build_ ./internal/foxtrot/blob_stores/`
Expected: PASS.

**Step 5: Run the full Go suite**

Run: `just test-go`
Expected: PASS.

**Step 6: Commit**

```bash
git add go/internal/foxtrot/blob_stores/
git commit -m "feat(blob_stores): builder error paths (empty, mode confusion, write-in-read)

Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>"
```

---

## Task 5: Mirror mode — HasBlob, MakeBlobReader, MakeBlobWriter

**Promotion criteria:** N/A.

**Files:**
- Modify: `go/internal/foxtrot/blob_stores/multi.go`
- Create: `go/internal/foxtrot/blob_stores/multi_test.go`

**Step 1: Write failing tests**

Create `go/internal/foxtrot/blob_stores/multi_test.go`:

```go
package blob_stores

import (
    "testing"
)

func TestMulti_Mirror_HasBlob_AnyChild(t *testing.T) {
    // storeA has the blob; storeB does not. HasBlob must return true.
    // (Use the existing stubBlobStore from store_inventory_archive_test.go.)
}

func TestMulti_Mirror_MakeBlobReader_FirstHit(t *testing.T) {
    // storeA has blob; storeB has blob. Reader comes from storeA.
}

func TestMulti_Mirror_MakeBlobWriter_WritesToAll(t *testing.T) {
    // Write through Multi; assert both children received the bytes.
}
```

Implementer fills in the actual asserts using stubs/spies. Use `controllableBlobReader` from Task 2 for the read tests; use a spy `BlobStoreInitialized` wrapper to capture writes.

**Step 2: Run, verify FAIL**

Run: `cd go && go test -tags test -run TestMulti_Mirror_ ./internal/foxtrot/blob_stores/`
Expected: FAIL (Mirror methods don't branch on mode yet).

**Step 3: Refactor existing `Multi` methods to branch on mode**

In `multi.go`, the existing `HasBlob`, `MakeBlobReader`, `MakeBlobWriter` already implement Mirror semantics — wrap each with a mode switch so they only fire when `mode == modeMirror`:

```go
func (m Multi) HasBlob(id domain_interfaces.MarklId) bool {
    switch m.mode {
    case modeMirror:
        for _, c := range m.childStores {
            if c.HasBlob(id) { return true }
        }
        return false
    case modeWriteThrough:
        return false // implemented in Task 9
    }
    return false
}

// similarly for MakeBlobReader, MakeBlobWriter
```

**Step 4: Run, verify PASS**

Run: `cd go && go test -tags test -run TestMulti_Mirror_ ./internal/foxtrot/blob_stores/`
Expected: PASS.

**Step 5: Commit**

```bash
git add go/internal/foxtrot/blob_stores/multi.go go/internal/foxtrot/blob_stores/multi_test.go
git commit -m "feat(blob_stores): mode-aware Mirror dispatch for HasBlob/Reader/Writer

Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>"
```

---

## Task 6: Mirror widening — `BlobStore` interface (description, config, hash, IO wrapper)

**Promotion criteria:** N/A.

**Files:**
- Modify: `go/internal/foxtrot/blob_stores/multi.go`
- Modify: `go/internal/foxtrot/blob_stores/multi_test.go`

**Step 1: Write failing tests for each delegation**

Append to `multi_test.go`:

```go
func TestMulti_Mirror_GetBlobStoreDescription(t *testing.T) {
    // Mirror(A,B,C).GetBlobStoreDescription() == "multi/mirror(idA,idB,idC)"
}

func TestMulti_Mirror_GetDefaultHashType_FirstChild(t *testing.T) {
    // Delegates to first child.
}

func TestMulti_Mirror_GetBlobStoreConfig_FirstChild(t *testing.T) { /* ... */ }
func TestMulti_Mirror_GetBlobIOWrapper_FirstChild(t *testing.T) { /* ... */ }
```

**Step 2: Run, verify FAIL**

Run: `cd go && go test -tags test -run TestMulti_Mirror_Get ./internal/foxtrot/blob_stores/`
Expected: FAIL (methods don't exist).

**Step 3: Implement the methods**

Add to `multi.go`:

```go
func (m Multi) GetBlobStoreDescription() string { /* mirror vs write-through */ }
func (m Multi) GetDefaultHashType() domain_interfaces.FormatHash { /* first child or write store */ }
func (m Multi) GetBlobStoreConfig() domain_interfaces.BlobStoreConfig { /* ... */ }
func (m Multi) GetBlobIOWrapper() domain_interfaces.BlobIOWrapper { /* ... */ }
```

Update the interface guard from `var _ domain_interfaces.BlobAccess = Multi{}` to `var _ domain_interfaces.BlobStore = Multi{}` once `AllBlobs` lands in Task 7 (write a TODO comment for now: `// var _ domain_interfaces.BlobStore = Multi{}` once AllBlobs lands).

**Step 4: Run, verify PASS**

Run: `cd go && go test -tags test -run TestMulti_Mirror_Get ./internal/foxtrot/blob_stores/`
Expected: PASS.

**Step 5: Commit**

```bash
git add go/internal/foxtrot/blob_stores/multi.go go/internal/foxtrot/blob_stores/multi_test.go
git commit -m "feat(blob_stores): Multi delegates description/config/hash/wrapper

Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>"
```

---

## Task 7: AllBlobs N-way merge with same-hash dedupe

**Promotion criteria:** N/A.

**Files:**
- Modify: `go/internal/foxtrot/blob_stores/multi.go`
- Modify: `go/internal/foxtrot/blob_stores/multi_test.go`

**Step 1: Write failing test**

```go
func TestMulti_AllBlobs_SameHashDedupes(t *testing.T) {
    // storeA yields [d1, d2, d3] (sorted)
    // storeB yields [d2, d3, d4] (sorted)
    // Expected: [d1, d2, d3, d4] — d2 and d3 dedupe.
}

func TestMulti_AllBlobs_BothStoresEmpty(t *testing.T) { /* yields nothing */ }
func TestMulti_AllBlobs_OneStoreEmpty(t *testing.T) { /* yields the non-empty's blobs */ }
```

**Step 2: Run, verify FAIL**

Expected: FAIL (`AllBlobs` not implemented on `Multi`).

**Step 3: Implement N-way merge via `iter.Pull`**

Add to `multi.go`:

```go
import "iter"

func (m Multi) AllBlobs() interfaces.SeqError[domain_interfaces.MarklId] {
    sources := m.allBlobSources() // mirror children or write+read stores
    return func(yield func(domain_interfaces.MarklId, error) bool) {
        type head struct {
            id  domain_interfaces.MarklId
            err error
            ok  bool
            next func() (domain_interfaces.MarklId, error, bool)
            stop func()
        }
        heads := make([]head, len(sources))
        for i, s := range sources {
            next, stop := iter.Pull2(iter.Seq2[domain_interfaces.MarklId, error](s.AllBlobs()))
            id, err, ok := next()
            heads[i] = head{id, err, ok, next, stop}
        }
        defer func() {
            for _, h := range heads {
                h.stop()
            }
        }()
        for {
            // find the lexically-minimum live head
            minIdx := -1
            for i, h := range heads {
                if !h.ok { continue }
                if h.err != nil {
                    if !yield(nil, h.err) { return }
                    heads[i].id, heads[i].err, heads[i].ok = h.next()
                    continue
                }
                if minIdx == -1 || compareMarklIds(h.id, heads[minIdx].id) < 0 {
                    minIdx = i
                }
            }
            if minIdx == -1 { return } // all exhausted
            minId := heads[minIdx].id
            if !yield(minId, nil) { return }
            // advance every head matching the min (dedupe by byte equality)
            for i, h := range heads {
                if h.ok && h.err == nil && compareMarklIds(h.id, minId) == 0 {
                    heads[i].id, heads[i].err, heads[i].ok = h.next()
                }
            }
        }
    }
}

// compareMarklIds returns -1/0/1 by byte-lex comparison of the
// MarklId's wire representation. Cross-hash digests compare by
// their formatted bytes (which embed the hash-format tag), so they
// never compare as equal.
func compareMarklIds(a, b domain_interfaces.MarklId) int {
    // implement: bytes.Compare(a.bytes, b.bytes) where bytes is the
    // canonical encoding. Use markl.* helpers if they exist; otherwise
    // compare via String() as a fallback.
}

func (m Multi) allBlobSources() []BlobStoreInitialized {
    switch m.mode {
    case modeMirror:
        return m.childStores
    case modeWriteThrough:
        out := make([]BlobStoreInitialized, 0, 1+len(m.readStores))
        out = append(out, m.writeStore)
        out = append(out, m.readStores...)
        return out
    }
    return nil
}
```

Add the interface guard now: `var _ domain_interfaces.BlobStore = Multi{}`.

**Step 4: Run, verify PASS**

Run: `cd go && go test -tags test -run TestMulti_AllBlobs ./internal/foxtrot/blob_stores/`
Expected: PASS.

**Step 5: Commit**

```bash
git add go/internal/foxtrot/blob_stores/
git commit -m "feat(blob_stores): N-way merge AllBlobs with same-hash dedupe

Implements BlobStore interface fully. Uses iter.Pull for pull-mode
iteration over each child's SeqError[MarklId]. Same-byte-MarklId
heads collapse to one yield; the contract documented in Task 1
makes this load-bearing.

Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>"
```

---

## Task 8: AllBlobs cross-hash pass-through

**Promotion criteria:** N/A.

**Files:**
- Modify: `go/internal/foxtrot/blob_stores/multi_test.go`

**Step 1: Write failing test**

```go
func TestMulti_AllBlobs_CrossHashPassesThrough(t *testing.T) {
    // storeA: [blake2b256:abc, blake2b256:def] (sorted)
    // storeB: [sha256:xyz] (sorted)
    // Expected: all three digests appear separately; sort order is
    // lex byte-order of the formatted MarklId (which embeds the
    // hash-format tag), so the order depends on whether "blake2b256"
    // sorts before "sha256". Assert the SET of digests, not order,
    // OR assert the byte-lex order explicitly with chosen test data.
}
```

**Step 2: Run, verify**

Run: `cd go && go test -tags test -run TestMulti_AllBlobs_CrossHash ./internal/foxtrot/blob_stores/`
Expected: PASS (this is already supported by the Task 7 implementation; the test is here to pin the behavior).

If FAIL: refine `compareMarklIds` to use the canonical byte-encoding of the MarklId. The test is the contract pin.

**Step 3: Commit**

```bash
git add go/internal/foxtrot/blob_stores/multi_test.go
git commit -m "test(blob_stores): pin cross-hash pass-through in AllBlobs merge

Different hash types are not byte-equal and emit as separate entries.
Behavior was already covered by Task 7's implementation; this test
pins the contract.

Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>"
```

---

## Task 9: WriteTo+Read mode skeleton (ReadFill=off)

**Promotion criteria:** N/A.

**Files:**
- Modify: `go/internal/foxtrot/blob_stores/multi.go`
- Modify: `go/internal/foxtrot/blob_stores/multi_test.go`

**Step 1: Write failing tests**

```go
func TestMulti_WriteThrough_HasBlob_WriteOrAnyRead(t *testing.T) { /* ... */ }

func TestMulti_WriteThrough_MakeBlobReader_WriteStoreFirst(t *testing.T) { /* ... */ }

func TestMulti_WriteThrough_MakeBlobReader_FallsBackToReadSource_NoFill(t *testing.T) {
    // Build with ReadFill(false). Reader comes directly from read source;
    // write store unchanged after the read.
}

func TestMulti_WriteThrough_MakeBlobWriter_WriteStoreOnly(t *testing.T) { /* ... */ }

func TestMulti_WriteThrough_Description_NamesWriteAndRead(t *testing.T) {
    // GetBlobStoreDescription() includes "W=" and "R=" segments.
}

func TestMulti_WriteThrough_DefaultHashType_FromWriteStore(t *testing.T) { /* ... */ }
```

**Step 2: Run, verify FAIL**

Expected: FAIL (write-through branches return false/empty today).

**Step 3: Implement the write-through branches**

In `multi.go`, fill in the `case modeWriteThrough:` arms of `HasBlob`, `MakeBlobReader`, `MakeBlobWriter`, and the delegation getters. For `MakeBlobReader` when `readFill=false`, return the source reader directly. The tee path is Task 10.

**Step 4: Run, verify PASS**

Expected: PASS.

**Step 5: Commit**

```bash
git add go/internal/foxtrot/blob_stores/
git commit -m "feat(blob_stores): WriteTo+Read mode without ReadFill (fanout read, single-target write)

ReadFill=true (tee path) lands in next task.

Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>"
```

---

## Task 10: Tee-during-read — eager-close commit

**Promotion criteria:** N/A.

**Files:**
- Create: `go/internal/foxtrot/blob_stores/multi_tee.go`
- Create: `go/internal/foxtrot/blob_stores/multi_tee_test.go`
- Modify: `go/internal/foxtrot/blob_stores/multi.go` (MakeBlobReader write-through branch wires the tee when readFill=true)

**Step 1: Write failing tests**

Create `multi_tee_test.go`:

```go
package blob_stores

import "testing"

func TestTee_ReadCopiesToSink(t *testing.T) {
    src := newControllableBlobReader(/* id */)
    sink := &spyBlobWriter{}
    src.Feed([]byte("hello world"))
    src.Close() // mark EOF
    tee := newTeeBlobReader(/* ctx */, src, sink)
    buf := make([]byte, 11)
    n, _ := tee.Read(buf)
    if string(buf[:n]) != "hello" || /* etc — assert sink.received contains the same prefix */ { t.Fatal(...) }
}

func TestTee_CallerClose_CommitsSink(t *testing.T) {
    // Drain fully, call tee.Close. Assert sink.closed.Load() == true.
}

func TestTee_SinkWriteError_PoisonsButReadContinues(t *testing.T) {
    sink := &spyBlobWriter{failAfterBytes: 3}
    // Feed 11 bytes; sink fails after 3; caller still reads all 11
    // from the source.
}
```

**Step 2: Run, verify FAIL**

Expected: FAIL (`newTeeBlobReader` doesn't exist).

**Step 3: Implement the tee**

Create `multi_tee.go`:

```go
package blob_stores

import (
    "io"
    "sync/atomic"

    "code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
    "code.linenisgreat.com/purse-first/libs/dewey/0/interfaces"
    "code.linenisgreat.com/purse-first/libs/dewey/bravo/errors"
)

type teeBlobReader struct {
    ctx       interfaces.ActiveContext
    src       domain_interfaces.BlobReader
    sink      domain_interfaces.BlobWriter
    expected  domain_interfaces.MarklId
    sinkDead  atomic.Bool
    done      atomic.Bool
}

func newTeeBlobReader(
    ctx interfaces.ActiveContext,
    src domain_interfaces.BlobReader,
    sink domain_interfaces.BlobWriter,
    expected domain_interfaces.MarklId,
) *teeBlobReader {
    t := &teeBlobReader{ctx: ctx, src: src, sink: sink, expected: expected}
    ctx.After(errors.MakeFuncContextFromFuncErr(func() error {
        t.flushAndCommit()
        return nil
    }))
    return t
}

func (t *teeBlobReader) Read(p []byte) (int, error) {
    n, err := t.src.Read(p)
    if n > 0 && !t.sinkDead.Load() {
        if _, werr := t.sink.Write(p[:n]); werr != nil {
            t.sinkDead.Store(true)
        }
    }
    return n, err
}

func (t *teeBlobReader) Close() error {
    if t.done.Swap(true) {
        return nil
    }
    err1 := t.src.Close()
    err2 := t.sink.Close()
    return errors.Join(err1, err2)
}

func (t *teeBlobReader) GetMarklId() domain_interfaces.MarklId {
    return t.src.GetMarklId()
}

// ReadAt/Seek/WriteTo: delegate to src; WriteTo also tees. Implementer
// fills these in based on the BlobReader contract.

func (t *teeBlobReader) flushAndCommit() {
    if t.done.Swap(true) {
        return
    }
    _, _ = io.Copy(io.Discard, t) // through the tee
    _ = t.src.Close()
    _ = t.sink.Close()
    // After commit, optionally compare sink.GetMarklId() to expected and
    // log via observer if they differ — pure detect-and-report.
}
```

Wire the tee into `multi.go`'s WriteTo+Read MakeBlobReader path:

```go
case modeWriteThrough:
    if m.writeStore.HasBlob(id) {
        return m.writeStore.MakeBlobReader(id)
    }
    for _, src := range m.readStores {
        if !src.HasBlob(id) { continue }
        reader, err := src.MakeBlobReader(id)
        if err != nil { return nil, err }
        if !m.readFill {
            return reader, nil
        }
        writer, err := m.writeStore.MakeBlobWriter(m.writeStore.GetDefaultHashType())
        if err != nil {
            // can't tee; degrade to plain read
            return reader, nil
        }
        return newTeeBlobReader(m.ctx, reader, writer, id), nil
    }
    return nil, blob_io.ErrBlobMissing{BlobId: clonedId}
```

**Step 4: Run, verify PASS**

Run: `cd go && go test -tags test -run 'TestTee_|TestMulti_WriteThrough_' ./internal/foxtrot/blob_stores/`
Expected: PASS.

**Step 5: Commit**

```bash
git add go/internal/foxtrot/blob_stores/
git commit -m "feat(blob_stores): tee-during-read cache fill with eager-close commit

Wraps the source reader; bytes tee to the write-store writer. Caller's
Close commits the sink. ctx.After fallback added in next task.

Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>"
```

---

## Task 11: Tee — ctx.After completion fallback

**Promotion criteria:** N/A.

**Files:**
- Modify: `go/internal/foxtrot/blob_stores/multi_tee_test.go`
- Modify: `go/internal/foxtrot/blob_stores/multi_tee.go` (only if missing logic)

**Step 1: Write failing tests**

```go
func TestTee_PartialDrain_CtxAfterDrainsRest(t *testing.T) {
    // Feed 11 bytes; caller reads 3 then never reads more; never calls Close.
    // Trigger ctx.After. Assert sink.received has all 11 bytes; sink.closed true.
}

func TestTee_CallerCloseFirst_CtxAfterIsNoop(t *testing.T) {
    // Drain + Close. Then fire ctx.After. Assert no double-close / no panic
    // and that the spy writer's Close was called exactly once.
}

func TestTee_CallerNeverCloses_CtxAfterDrainsAndCloses(t *testing.T) {
    // Don't drain, don't close. ctx.After must drain through the tee and close.
}
```

The spy `ActiveContext` needs to actually invoke registered After-funcs when "fired" — the current `spyActiveContext.After` at `store_remote_sftp_test.go:229` is a no-op. Add a richer spy in `multi_test_helpers.go`:

```go
type firingActiveContext struct {
    spyActiveContext
    afterFuncs []interfaces.FuncActiveContext
}

func (c *firingActiveContext) After(f interfaces.FuncActiveContext) {
    c.afterFuncs = append(c.afterFuncs, f)
}

func (c *firingActiveContext) FireAfter() {
    for _, f := range c.afterFuncs {
        _ = f(c)
    }
}
```

**Step 2: Run, verify FAIL or PASS**

Run: `cd go && go test -tags test -run 'TestTee_PartialDrain|TestTee_CallerClose|TestTee_CallerNeverCloses' ./internal/foxtrot/blob_stores/`
Expected: PASS — `flushAndCommit` was implemented in Task 10. If a test reveals a missing edge case (e.g. double-close panic), fix in `multi_tee.go`.

**Step 3: Commit**

```bash
git add go/internal/foxtrot/blob_stores/
git commit -m "test(blob_stores): pin tee ctx.After completion paths

Covers partial-drain + ctx.After, eager-close-then-ctx.After-noop,
and never-close + ctx.After. atomic.Bool serializes commits.

Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>"
```

---

## Task 12: Cross-hash digest mapping in tee path

**Promotion criteria:** N/A.

**Files:**
- Modify: `go/internal/foxtrot/blob_stores/multi_tee.go`
- Modify: `go/internal/foxtrot/blob_stores/multi_tee_test.go`

**Step 1: Write failing test**

```go
func TestTee_CrossHash_RegistersForeignDigestMapping(t *testing.T) {
    // Source produces blob under hash H1 with id1.
    // Write store uses hash H2; after commit its writer.GetMarklId() == id2.
    // If write store also implements BlobForeignDigestAdder, the tee must
    // call AddForeignBlobDigestForNativeDigest(id1, id2) after commit.
    //
    // Use a spy write-store that records adder calls.
}
```

**Step 2: Run, verify FAIL**

Expected: FAIL (cross-hash registration not in the current tee code).

**Step 3: Implement in `multi_tee.go`'s `flushAndCommit` (and the caller-Close path)**

After both Close calls succeed:

```go
// Pulled inline from CopyBlobIfNecessary lines 119-132
writerDigest := t.sink.GetMarklId()
if t.expected != nil && !markl.Equals(t.expected, writerDigest) {
    // Different hash format → register the mapping.
    if t.expected.GetMarklFormat().GetMarklFormatId() !=
        writerDigest.GetMarklFormat().GetMarklFormatId() {
        if adder, ok := t.writeStore.(domain_interfaces.BlobForeignDigestAdder); ok {
            _ = adder.AddForeignBlobDigestForNativeDigest(t.expected, writerDigest)
        }
    }
    // Same-hash mismatch: detect-and-report (see design §Error handling).
    // Wrong-bytes blob lands under its actual digest; requested id remains missing.
}
```

This requires storing a `writeStore` reference on `teeBlobReader` — add it to the struct and constructor.

**Step 4: Run, verify PASS**

Expected: PASS.

**Step 5: Run race-detector to catch any flake**

Run: `just test-go-race`
Expected: PASS.

**Step 6: Commit**

```bash
git add go/internal/foxtrot/blob_stores/
git commit -m "feat(blob_stores): cross-hash foreign-digest mapping in tee commit

Mirrors CopyBlobIfNecessary lines 119-132. Same-hash mismatch is
detect-and-report only; content addressing makes wrong-bytes commits
non-corrupting.

Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>"
```

---

## Task 13: Regenerate `pkgs/blob_stores` facade

**Promotion criteria:** N/A.

**Files:**
- Regenerate: `go/pkgs/blob_stores/main.go`

**Step 1: Regenerate**

Run: `just generate-facades`
Expected: `git diff go/pkgs/blob_stores/main.go` shows new exports for `NewMulti`, `MultiBuilder` (and Multi has expanded methods). If dagnabit lifts these automatically, the diff is small. If not, the public surface may need an explicit re-export.

**Step 2: Run build + tests**

Run: `just build-go && just test-go`
Expected: PASS.

**Step 3: Commit**

```bash
git add go/pkgs/blob_stores/main.go
git commit -m "chore(pkgs): regenerate blob_stores facade for NewMulti + MultiBuilder

Generated via just generate-facades (dagnabit). Exposes the new
builder shape to external consumers (cutting-garden, future wrappers).

Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>"
```

---

## Task 14: CLI `cat -multi` + bats coverage

**Promotion criteria:** N/A.

**Files:**
- Modify: `go/internal/india/commands/cat.go`
- Create: `zz-tests_bats/multi.bats`

**Step 1: Write the failing bats test**

Create `zz-tests_bats/multi.bats`:

```bash
setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=multi

function cat_multi_reads_from_default_store { # @test
  init_store
  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "default content" >"$blob"
  local id
  id="$(write_blob_id "$blob")"

  run_madder cat -multi "$id"
  assert_success
  assert_output --partial "default content"
}

function cat_multi_reads_from_secondary_store { # @test
  init_store
  run_madder init -encryption none .other
  assert_success

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "other content" >"$blob"
  local id
  id="$(write_blob_id ".other" "$blob")"

  run_madder cat -multi "$id"
  assert_success
  assert_output --partial "other content"
}

function cat_multi_fills_default_store_on_miss { # @test
  init_store
  run_madder init -encryption none .other
  assert_success

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "fill me" >"$blob"
  local id
  id="$(write_blob_id ".other" "$blob")"

  # Confirm default does NOT have it yet.
  run_madder has ".default" "$id"
  assert_output --partial "not found"

  run_madder cat -multi "$id"
  assert_success

  # After the multi-cat, the default store now has the blob.
  run_madder has ".default" "$id"
  assert_output --partial "found"
}

function cat_multi_no_read_fill_skips_copy { # @test
  init_store
  run_madder init -encryption none .other
  assert_success

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "do not fill" >"$blob"
  local id
  id="$(write_blob_id ".other" "$blob")"

  run_madder cat -multi -no-read-fill "$id"
  assert_success

  run_madder has ".default" "$id"
  assert_output --partial "not found"
}
```

**Step 2: Run, verify FAIL**

Run: `just zz-tests_bats/test-targets multi.bats`
Expected: FAIL — `-multi` flag is unknown.

**Step 3: Implement the flags + Multi wiring in `cat.go`**

Add to `Cat` struct:

```go
type Cat struct {
    command_components.EnvBlobStore
    Utility    script_value.Utility
    PrefixSha  bool
    Multi      bool
    NoReadFill bool
}

func (cmd *Cat) SetFlagDefinitions(flagSet interfaces.CLIFlagDefinitions) {
    flagSet.Var(&cmd.Utility, "utility", "")
    flagSet.BoolVar(&cmd.PrefixSha, "prefix-sha", false, "")
    flagSet.BoolVar(&cmd.Multi, "multi", false,
        "read via Multi over all configured stores; cache-fills the default store on miss")
    flagSet.BoolVar(&cmd.NoReadFill, "no-read-fill", false,
        "with -multi: skip the cache-fill into the default store")
}
```

In `Cat.Run`, when `cmd.Multi` is true, construct a `Multi` instead of `GetDefaultBlobStore`:

```go
if cmd.Multi {
    defaultStore, remaining := envBlobStore.GetDefaultBlobStoreAndRemaining()
    readStores := make([]blob_stores.BlobStoreInitialized, 0, len(remaining))
    for _, s := range remaining {
        readStores = append(readStores, s)
    }
    builder := blob_stores.NewMulti(req.Context).
        WriteTo(defaultStore).
        Read(readStores...)
    if cmd.NoReadFill {
        builder = builder.ReadFill(false)
    }
    m, err := builder.Build()
    if err != nil {
        errors.ContextCancelWithError(req, err)
        return
    }
    // Use `m` as the single blob store for the read loop. (No more
    // blobFromRemainingStores fallback — Multi handles it internally.)
}
```

Implementer note: The Multi's runtime type isn't `BlobStoreInitialized` — it's `Multi`. Adapt the read loop accordingly. The cleanest path is to call `m.MakeBlobReader(id)` directly and route the bytes through the existing `cmd.blob` helper (which already accepts something narrower than `BlobStoreInitialized` — verify by reading `cmd.blob` at `cat.go:264`).

**Step 4: Run bats, verify PASS**

Run: `just zz-tests_bats/test-targets multi.bats`
Expected: PASS.

**Step 5: Run full bats + Go suites**

Run: `just test`
Expected: PASS.

**Step 6: Commit**

```bash
git add go/internal/india/commands/cat.go zz-tests_bats/multi.bats
git commit -m "feat(cat): add -multi and -no-read-fill flags

When -multi is set, cat constructs a Multi blob-store over the default
store (write target) and remaining stores (read sources). Cache-fills
the default store on miss; -no-read-fill opts out.

Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>"
```

---

## Task 15: CLI `has -multi` + bats coverage

**Promotion criteria:** N/A.

**Files:**
- Modify: `go/internal/india/commands/has.go`
- Modify: `zz-tests_bats/multi.bats`

**Step 1: Write failing bats test**

Append to `multi.bats`:

```bash
function has_multi_finds_blob_in_secondary { # @test
  init_store
  run_madder init -encryption none .other
  assert_success

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "where am i" >"$blob"
  local id
  id="$(write_blob_id ".other" "$blob")"

  run_madder has "$id"
  # without -multi: should find via "all-stores search" (existing behavior)
  # may already pass — adjust the assertion to confirm Multi is what answers
  # if needed.

  run_madder has -multi "$id"
  assert_success
  assert_output --partial "found"
}
```

**Step 2: Run, verify FAIL**

Expected: FAIL (`-multi` flag unknown on `has`).

**Step 3: Implement**

Add `Multi bool` to `Has`; add flag def; in `Has.Run` when `cmd.Multi`, construct a Multi (similar to cat — same shape) and replace `findStores` with a single `m.HasBlob(id)` call.

Implementer note: `has -multi` and `has -all` have related but different semantics; the existing `findStores` loop iterates manually. With `-multi`, the question reduces to a single `HasBlob` on the Multi — but the output should still indicate *which* store had it (so the test can assert "found" with a store id). Either pick a Multi-aware print format or fall back to the manual loop just for the print step. Document the choice in a comment.

**Step 4: Run bats, verify PASS**

Expected: PASS.

**Step 5: Commit**

```bash
git add go/internal/india/commands/has.go zz-tests_bats/multi.bats
git commit -m "feat(has): add -multi flag

Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>"
```

---

## Task 16: CLI `list -multi` + bats coverage

**Promotion criteria:** N/A.

**Files:**
- Modify: `go/internal/india/commands/list.go`
- Modify: `zz-tests_bats/multi.bats`

**Step 1: Write failing bats test**

Append to `multi.bats`:

```bash
function list_multi_unions_stores { # @test
  init_store
  run_madder init -encryption none .other
  assert_success

  local b1="$BATS_TEST_TMPDIR/b1.txt"
  echo "in default" >"$b1"
  local id1
  id1="$(write_blob_id "$b1")"

  local b2="$BATS_TEST_TMPDIR/b2.txt"
  echo "in other" >"$b2"
  local id2
  id2="$(write_blob_id ".other" "$b2")"

  run_madder list -multi -format tap
  assert_success
  assert_output --partial "$id1"
  assert_output --partial "$id2"
}

function list_multi_dedupes_same_hash_overlap { # @test
  init_store
  run_madder init -encryption none .other
  assert_success

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "in both stores" >"$blob"
  local id1 id2
  id1="$(write_blob_id "$blob")"
  id2="$(write_blob_id ".other" "$blob")"
  # Same content → same hash → same id
  [[ "$id1" == "$id2" ]] || { echo "expected same id; got $id1 vs $id2" >&2; return 1; }

  run_madder list -multi -format tap
  assert_success
  # The id appears exactly once
  local count
  count=$(echo "$output" | grep -c "$id1")
  [[ "$count" -eq 1 ]] || { echo "expected 1 occurrence, got $count" >&2; return 1; }
}
```

**Step 2: Run, verify FAIL**

Expected: FAIL.

**Step 3: Implement**

Add `Multi bool` to `List`; flag def; in `List.Run` when `cmd.Multi`, construct Multi over all stores (Mirror mode — every store contributes to AllBlobs equally) and iterate `m.AllBlobs()`.

Implementer note: For `list -multi`, the relevant Multi mode is *Mirror* (so AllBlobs unions everything). WriteTo+Read mode also gives a union via AllBlobs but biases description/config toward the write store, which `list` doesn't need.

**Step 4: Run bats, verify PASS**

Expected: PASS.

**Step 5: Commit**

```bash
git add go/internal/india/commands/list.go zz-tests_bats/multi.bats
git commit -m "feat(list): add -multi flag (unions AllBlobs across stores)

Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>"
```

---

## Task 17: CLI `fsck -multi` + bats coverage

**Promotion criteria:** N/A.

**Files:**
- Modify: `go/internal/india/commands/fsck.go`
- Modify: `zz-tests_bats/multi.bats`

**Step 1: Write failing bats test**

Append to `multi.bats`:

```bash
function fsck_multi_walks_all_stores { # @test
  init_store
  run_madder init -encryption none .other
  assert_success

  local b1="$BATS_TEST_TMPDIR/b1.txt"
  echo "in default" >"$b1"
  write_blob_id "$b1" >/dev/null

  local b2="$BATS_TEST_TMPDIR/b2.txt"
  echo "in other" >"$b2"
  write_blob_id ".other" "$b2" >/dev/null

  run_madder fsck -multi -format tap
  assert_success
}
```

**Step 2: Run, verify FAIL**

Expected: FAIL.

**Step 3: Implement**

Same pattern: `Multi bool` field, flag def, build Multi over all stores when set, walk `AllBlobs`, verify each blob is readable.

**Step 4: Run bats, verify PASS**

Expected: PASS.

**Step 5: Commit**

```bash
git add go/internal/india/commands/fsck.go zz-tests_bats/multi.bats
git commit -m "feat(fsck): add -multi flag (verify blobs across all stores)

Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>"
```

---

## Task 18: Coverage verification + gap fill

**Promotion criteria:** N/A.

**Files:** any test files needed to close coverage gaps.

**Step 1: Run merged coverage**

Run: `just cover-merged && just cover-summary`
Expected: report shows coverage for `internal/foxtrot/blob_stores/multi.go`, `multi_builder.go`, `multi_tee.go`.

Alternative tighter command:
```
cd go && go test -tags test -coverprofile=/tmp/cov.out \
  -coverpkg=./internal/foxtrot/blob_stores \
  ./internal/foxtrot/blob_stores
go tool cover -func=/tmp/cov.out | grep -E 'multi(_builder|_tee)?\.go'
```

**Step 2: Identify gaps**

For each uncovered line in `multi*.go`, decide:
- Is it covered by bats? (Re-run with `just test-bats-cover` to combine.)
- If neither: add a Go unit test in `multi_test.go`, `multi_builder_test.go`, or `multi_tee_test.go`.

Common likely gaps:
- Error-wrap branches in `MakeBlobWriter` (write store's writer creation fails)
- `flushAndCommit` when src.Close errors
- `Multi.GetBlobIOWrapper` in Mirror mode with N>1 children (delegation correctness)

**Step 3: Run race detector one final time**

Run: `just test-go-race`
Expected: PASS, no data races.

**Step 4: Commit (squash if multiple gap-fill tests)**

```bash
git add go/internal/foxtrot/blob_stores/
git commit -m "test(blob_stores): fill coverage gaps to 100% for Multi/builder/tee

Signed-off-by: Clown :clown: <https://github.com/amarbel-llc/clown>"
```

**Step 5: Final verification — entire test suite**

Run: `just test`
Expected: PASS (Go unit tests + bats + bats-net-cap).

---

## Closing checklist

After Task 18:

- [ ] `just test` passes
- [ ] `just test-go-race` passes
- [ ] `just vet-go` passes
- [ ] Coverage for `multi.go`, `multi_builder.go`, `multi_tee.go` is 100%
- [ ] `go/pkgs/blob_stores/main.go` regenerated and exposes `NewMulti`, `MultiBuilder`
- [ ] All commits signed off as Clown
- [ ] Design doc `docs/plans/2026-05-13-multi-blob-store-builder-design.md` cross-referenced from this plan
- [ ] Issue (if any) for the deferred items: long-lived ctx pressure profiling, optional `BlobWriter` expected-digest extension, optional `BlobDeleter`/`BlobForeignDigestAdder` on `Multi`, "pointer" Multi config-store type
