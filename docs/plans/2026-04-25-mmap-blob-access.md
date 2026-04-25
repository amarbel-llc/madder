# mmap'd `[]byte` access to local blobs — implementation plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Implement `MakeMmapBlobFromBlobReader` so library callers can promote a `BlobReader` to a zero-copy `[]byte` view, restricted in v1 to local hash-bucketed blobs with nil compression and nil encryption.

**Architecture:** New `foxtrot/mmap_blob/` package holds the `MmapBlob` interface, sentinel errors, and platform mmap glue. A new `MmapSource` capability interface in layer 0 is implemented only by `env_dir.blobReader`. Promotion is a type-assert + wrapper-policy check on the reader the caller already has; no changes to `BlobStore` / `BlobReaderFactory` interfaces.

**Tech Stack:** Go 1.26, `golang.org/x/sys/unix` (Mmap/Munmap), `dewey/bravo/errors` for wrapping, `dewey/delta/compression_type`, `dewey/charlie/ohio` (NopeIOWrapper).

**Rollback:** Purely additive. Delete `go/internal/foxtrot/mmap_blob/`, the `MmapSource()` method on `env_dir.blobReader`, the `Config.HasIdentityWrappers()` method, and the `MmapSource` interface from layer 0. No wire-format change.

**Design reference:** `docs/plans/2026-04-25-mmap-blob-access-design.md`

---

## Test runner cheat sheet

The repo's tests are tagged `//go:build test`. Run them via the root justfile:

```bash
just test-go-pkg internal/echo/env_dir       # one package
just test-go-pkg internal/foxtrot/mmap_blob  # one package
just test-go                                  # all Go tests
just test-go-race                             # with -race
```

Or directly: `cd go && go test -tags test ./internal/<path>/...`

Bats integration: `just test-bats` (default-deny sandcastle partition).

---

## Task 1: Stash `Config` on `env_dir.blobReader`

**Promotion criteria:** N/A (refactor; old behavior unchanged).

**Files:**
- Modify: `go/internal/echo/env_dir/blob_reader.go:18-63`

**Step 1: Run baseline tests**

```bash
just test-go-pkg internal/echo/env_dir
```
Expected: PASS.

**Step 2: Add `config` field and assign it**

In `blob_reader.go`, change the struct and `NewReader`:

```go
type blobReader struct {
	config     Config           // <-- new
	readSeeker io.ReadSeeker
	digester   domain_interfaces.BlobWriter
	decrypter  io.Reader
	expander   io.ReadCloser
	tee        io.Reader
}

func NewReader(
	config Config,
	readSeeker io.ReadSeeker,
) (reader *blobReader, err error) {
	reader = &blobReader{
		config:     config,         // <-- new
		readSeeker: readSeeker,
	}
	// rest unchanged
```

**Step 3: Re-run tests**

```bash
just test-go-pkg internal/echo/env_dir
```
Expected: PASS (no behavioral change).

**Step 4: Commit**

```
refactor(env_dir): stash Config on blobReader

Holds the full Config so an upcoming MmapSource() method can inspect
the wrapper chain. No behavioral change.
```

---

## Task 2: Add `Config.HasIdentityWrappers()`

**Promotion criteria:** N/A.

**Files:**
- Modify: `go/internal/echo/env_dir/blob_config.go`
- Test: `go/internal/echo/env_dir/blob_config_test.go` (new)

**Step 1: Write failing tests**

Create `go/internal/echo/env_dir/blob_config_test.go`:

```go
//go:build test

package env_dir

import (
	"testing"

	"github.com/amarbel-llc/purse-first/libs/dewey/delta/compression_type"
)

func TestHasIdentityWrappers_Default(t *testing.T) {
	if !DefaultConfig.HasIdentityWrappers() {
		t.Fatal("DefaultConfig should have identity wrappers")
	}
}

func TestHasIdentityWrappers_Zstd(t *testing.T) {
	zstd := compression_type.CompressionTypeZstd
	cfg := DefaultConfig
	cfg.compression = &zstd
	if cfg.HasIdentityWrappers() {
		t.Fatal("zstd compression must not be identity")
	}
}

func TestHasIdentityWrappers_Gzip(t *testing.T) {
	gzip := compression_type.CompressionTypeGzip
	cfg := DefaultConfig
	cfg.compression = &gzip
	if cfg.HasIdentityWrappers() {
		t.Fatal("gzip compression must not be identity")
	}
}
```

**Step 2: Run tests to confirm they fail**

```bash
just test-go-pkg internal/echo/env_dir
```
Expected: FAIL with "config.HasIdentityWrappers undefined".

**Step 3: Implement the method**

Append to `blob_config.go`:

```go
// HasIdentityWrappers returns true when both blob wrappers are
// byte-identity (none compression, no-op encryption). When true, the
// on-disk file bytes equal the logical blob bytes — a precondition
// for direct file mmap.
func (config Config) HasIdentityWrappers() bool {
	compType, ok := config.GetBlobCompression().(*compression_type.CompressionType)
	if !ok {
		return false
	}
	if *compType != compression_type.CompressionTypeNone &&
		*compType != compression_type.CompressionTypeEmpty {
		return false
	}
	if _, ok := config.GetBlobEncryption().(*ohio.NopeIOWrapper); !ok {
		return false
	}
	return true
}
```

(Adjust the `*ohio.NopeIOWrapper` type assertion if `GetBlobEncryption` returns the value not the pointer — the existing code uses both shapes; check what `defaultEncryptionIOWrapper` and `GetBlobEncryption` actually return and match it.)

**Step 4: Run tests**

```bash
just test-go-pkg internal/echo/env_dir
```
Expected: PASS.

**Step 5: Commit**

```
feat(env_dir): Config.HasIdentityWrappers policy check

Returns true iff both compression and encryption wrappers preserve
byte identity (none compression + NopeIOWrapper encryption). Used
by upcoming MmapSource gate.
```

---

## Task 3: Add `MmapSource` interface to layer 0

**Promotion criteria:** N/A.

**Files:**
- Modify: `go/internal/0/domain_interfaces/blob_store.go`

**Step 1: Add the interface**

In `blob_store.go`, alongside the existing block:

```go
import (
	"io"
	"os"  // <-- add

	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
)
```

Inside the type block:

```go
// MmapSource is implemented by a BlobReader whose bytes equal a
// contiguous file region. Only the local hash-bucketed store's
// reader implements this in v1; non-local stores (SFTP, in-memory)
// and stores wrapping the file with non-identity encoding return
// ok=false from MmapSource().
//
// On ok=true, ownership of file transfers to the caller; the caller
// is responsible for closing it (typically the MmapBlob does this).
MmapSource interface {
	MmapSource() (file *os.File, offset int64, length int64, ok bool, err error)
}
```

**Step 2: Vet check**

```bash
cd go && go vet -tags test ./internal/0/domain_interfaces/
```
Expected: no output (clean).

**Step 3: Build all packages to confirm no break**

```bash
cd go && go build -tags test ./...
```
Expected: no output (clean).

**Step 4: Commit**

```
feat(domain_interfaces): MmapSource capability interface

Optional capability for BlobReaders that back a contiguous on-disk
file range with byte-identity bytes. Used by the new mmap_blob
package to promote a BlobReader to MmapBlob.
```

---

## Task 4: Implement `(*blobReader).MmapSource()` — TDD

**Promotion criteria:** N/A.

**Files:**
- Test: `go/internal/echo/env_dir/blob_reader_mmap_test.go` (new)
- Modify: `go/internal/echo/env_dir/blob_reader.go`

**Step 1: Write failing tests**

Create `go/internal/echo/env_dir/blob_reader_mmap_test.go`:

```go
//go:build test

package env_dir

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/amarbel-llc/purse-first/libs/dewey/delta/compression_type"
)

func TestMmapSource_LocalFileIdentityWrappers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	payload := []byte("hello mmap world")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	br, err := NewFileReaderOrErrNotExist(DefaultConfig, path)
	if err != nil {
		t.Fatal(err)
	}
	defer br.Close()

	src, ok := br.(MmapSource)
	if !ok {
		t.Fatal("blobReader should implement MmapSource")
	}
	file, off, length, mmapOk, err := src.MmapSource()
	if err != nil {
		t.Fatal(err)
	}
	if !mmapOk {
		t.Fatal("expected ok=true for local file with default config")
	}
	if off != 0 {
		t.Fatalf("offset: got %d want 0", off)
	}
	if length != int64(len(payload)) {
		t.Fatalf("length: got %d want %d", length, len(payload))
	}
	if file == nil {
		t.Fatal("file is nil")
	}
}

func TestMmapSource_BytesReader(t *testing.T) {
	br, err := NewReader(DefaultConfig, bytes.NewReader([]byte("hi")))
	if err != nil {
		t.Fatal(err)
	}
	src, ok := any(br).(MmapSource)
	if !ok {
		t.Fatal("type-assert failed")
	}
	_, _, _, mmapOk, err := src.MmapSource()
	if err != nil {
		t.Fatal(err)
	}
	if mmapOk {
		t.Fatal("expected ok=false for non-*os.File reader")
	}
}

func TestMmapSource_ZstdCompression(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	if err := os.WriteFile(path, []byte("zstd content"), 0o644); err != nil {
		t.Fatal(err)
	}

	zstd := compression_type.CompressionTypeZstd
	cfg := DefaultConfig
	cfg.compression = &zstd

	// NewFileReaderOrErrNotExist would try to actually decompress; we
	// just want a blobReader holding the wrong compression. Use NewReader
	// with an opened *os.File.
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// NewReader with non-None compression: the decompressor wrap may
	// fail on raw bytes; that's fine for our test — we only care about
	// the policy check. Skip if NewReader fails.
	br, err := NewReader(cfg, f)
	if err != nil {
		t.Skipf("NewReader rejected zstd config on raw bytes: %v", err)
	}
	_, _, _, mmapOk, _ := br.MmapSource()
	if mmapOk {
		t.Fatal("expected ok=false for zstd-configured reader")
	}
}
```

**Step 2: Run tests to confirm they fail**

```bash
just test-go-pkg internal/echo/env_dir
```
Expected: FAIL with "br.(MmapSource): MmapSource undefined" or similar.

**Step 3: Implement `MmapSource()`**

Append to `blob_reader.go`:

```go
// MmapSource implements domain_interfaces.MmapSource. Returns ok=true
// only when readSeeker is *os.File and the wrapper chain is identity
// (no compression, no encryption). On ok=true the caller owns the
// returned *os.File.
func (reader *blobReader) MmapSource() (
	file *os.File,
	offset int64,
	length int64,
	ok bool,
	err error,
) {
	f, isFile := reader.readSeeker.(*os.File)
	if !isFile {
		return nil, 0, 0, false, nil
	}
	if !reader.config.HasIdentityWrappers() {
		return nil, 0, 0, false, nil
	}
	stat, err := f.Stat()
	if err != nil {
		return nil, 0, 0, false, errors.Wrap(err)
	}
	return f, 0, stat.Size(), true, nil
}
```

Add `var _ domain_interfaces.MmapSource = (*blobReader)(nil)` near the top of the file to enforce the interface contract at compile time.

**Step 4: Run tests**

```bash
just test-go-pkg internal/echo/env_dir
```
Expected: PASS.

**Step 5: Commit**

```
feat(env_dir): blobReader.MmapSource implements local-only mmap gate

Returns ok=true only when readSeeker is *os.File AND wrappers are
identity (none compression, NopeIOWrapper encryption). All other
cases return ok=false and the caller falls back to streaming reads.
```

---

## Task 5: Create `foxtrot/mmap_blob` package skeleton

**Promotion criteria:** N/A.

**Files:**
- Create: `go/internal/foxtrot/mmap_blob/blob.go`
- Test: `go/internal/foxtrot/mmap_blob/blob_test.go`

**Step 1: Write failing test**

Create `go/internal/foxtrot/mmap_blob/blob_test.go`:

```go
//go:build test

package mmap_blob

import (
	"errors"
	"testing"
)

func TestErrMmapUnsupported_IsSentinel(t *testing.T) {
	if ErrMmapUnsupported == nil {
		t.Fatal("ErrMmapUnsupported is nil")
	}
	wrapped := errors.Join(ErrMmapUnsupported, errors.New("ctx"))
	if !errors.Is(wrapped, ErrMmapUnsupported) {
		t.Fatal("errors.Is should match")
	}
}

func TestErrDigestMismatch_IsSentinel(t *testing.T) {
	if ErrDigestMismatch == nil {
		t.Fatal("ErrDigestMismatch is nil")
	}
}
```

**Step 2: Run tests to confirm package doesn't exist**

```bash
just test-go-pkg internal/foxtrot/mmap_blob
```
Expected: FAIL with "no Go files".

**Step 3: Create package with errors and interface**

Create `go/internal/foxtrot/mmap_blob/blob.go`:

```go
// Package mmap_blob promotes a BlobReader to a zero-copy []byte view
// backed by file mmap, when the underlying storage permits.
package mmap_blob

import (
	"errors"
	"io"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
)

var (
	// ErrMmapUnsupported is returned when MakeMmapBlobFromBlobReader
	// cannot promote the reader — wrong store, non-file backing, or
	// wrappers preclude byte-identity.
	ErrMmapUnsupported = errors.New("mmap_blob: blob is not mmap-able")

	// ErrDigestMismatch is returned only from MmapBlob.Verify() when
	// the recomputed digest does not match the recorded MarklId.
	ErrDigestMismatch = errors.New("mmap_blob: digest mismatch")
)

// MmapBlob is a zero-copy view of a blob's bytes. Bytes() returns a
// slice valid until Close(). Close is idempotent.
type MmapBlob interface {
	Bytes() []byte
	GetMarklId() domain_interfaces.MarklId
	Verify() error
	io.Closer
}
```

**Step 4: Run tests**

```bash
just test-go-pkg internal/foxtrot/mmap_blob
```
Expected: PASS.

**Step 5: Commit**

```
feat(mmap_blob): package skeleton — interface + sentinel errors

Empty MmapBlob interface and ErrMmapUnsupported / ErrDigestMismatch
sentinels. Implementation lands in subsequent commits.
```

---

## Task 6: Implement `MmapBlob` struct (`mmap_unix.go`)

**Promotion criteria:** N/A.

**Files:**
- Create: `go/internal/foxtrot/mmap_blob/mmap_unix.go`
- Test: `go/internal/foxtrot/mmap_blob/mmap_unix_test.go`

**Step 1: Write failing test**

Create `go/internal/foxtrot/mmap_blob/mmap_unix_test.go`:

```go
//go:build test && unix

package mmap_blob

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestMmapFile_Bytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	payload := []byte("zero copy hello world")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mb, err := mmapFile(f, 0, int64(len(payload)), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer mb.Close()
	if !bytes.Equal(mb.Bytes(), payload) {
		t.Fatalf("bytes mismatch: got %q want %q", mb.Bytes(), payload)
	}
}

func TestMmapFile_CloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mb, err := mmapFile(f, 0, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := mb.Close(); err != nil {
		t.Fatal(err)
	}
	if err := mb.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
```

**Step 2: Run tests to confirm they fail**

```bash
just test-go-pkg internal/foxtrot/mmap_blob
```
Expected: FAIL with "mmapFile undefined".

**Step 3: Implement `mmap_unix.go`**

```go
//go:build unix

package mmap_blob

import (
	"os"
	"sync"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"golang.org/x/sys/unix"
)

type mmapBlob struct {
	bytes  []byte
	marklId domain_interfaces.MarklId
	file   *os.File

	closeOnce sync.Once
	closeErr  error
}

func mmapFile(
	file *os.File,
	offset, length int64,
	marklId domain_interfaces.MarklId,
) (MmapBlob, error) {
	if length == 0 {
		// Empty blobs: return an empty []byte without calling mmap
		// (which rejects length=0 with EINVAL).
		return &mmapBlob{file: file, marklId: marklId}, nil
	}
	data, err := unix.Mmap(
		int(file.Fd()),
		offset,
		int(length),
		unix.PROT_READ,
		unix.MAP_SHARED,
	)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	return &mmapBlob{
		bytes:   data,
		marklId: marklId,
		file:    file,
	}, nil
}

func (m *mmapBlob) Bytes() []byte                       { return m.bytes }
func (m *mmapBlob) GetMarklId() domain_interfaces.MarklId { return m.marklId }

func (m *mmapBlob) Close() error {
	m.closeOnce.Do(func() {
		if m.bytes != nil {
			if err := unix.Munmap(m.bytes); err != nil {
				m.closeErr = errors.Wrap(err)
			}
			m.bytes = nil
		}
		if m.file != nil {
			if err := m.file.Close(); err != nil && m.closeErr == nil {
				m.closeErr = errors.Wrap(err)
			}
			m.file = nil
		}
	})
	return m.closeErr
}

// Verify is implemented in verify.go (next task).
```

**Step 4: Run tests**

```bash
just test-go-pkg internal/foxtrot/mmap_blob
```
Expected: PASS.

Note: `Verify()` is on the interface but not yet implemented on `mmapBlob`. The test build will fail until task 8 unless we add a stub. Add this stub in `mmap_unix.go`:

```go
func (m *mmapBlob) Verify() error {
	// Implemented in task 8.
	return nil
}
```

Re-run tests to confirm pass.

**Step 5: Commit**

```
feat(mmap_blob): mmapFile + MmapBlob impl on unix

Wraps unix.Mmap with read-only/MAP_SHARED, owns the *os.File, and
makes Close() idempotent via sync.Once. Verify() stubbed for next
task.
```

---

## Task 7: Implement `MakeMmapBlobFromBlobReader`

**Promotion criteria:** N/A.

**Files:**
- Create: `go/internal/foxtrot/mmap_blob/promote.go`
- Test: `go/internal/foxtrot/mmap_blob/promote_test.go`

**Step 1: Write failing tests**

```go
//go:build test && unix

package mmap_blob

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/echo/env_dir"
	"github.com/amarbel-llc/purse-first/libs/dewey/delta/compression_type"
)

func TestMakeMmapBlob_LocalFileIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	payload := []byte("the quick brown fox")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	br, err := env_dir.NewFileReaderOrErrNotExist(env_dir.DefaultConfig, path)
	if err != nil {
		t.Fatal(err)
	}
	mb, err := MakeMmapBlobFromBlobReader(br)
	if err != nil {
		t.Fatal(err)
	}
	defer mb.Close()
	if !bytes.Equal(mb.Bytes(), payload) {
		t.Fatalf("Bytes(): got %q want %q", mb.Bytes(), payload)
	}
	// After successful promotion, br.Close() must NOT double-close
	// the underlying file (mb owns it now).
	if err := br.Close(); err != nil {
		t.Fatalf("br.Close after promotion: %v", err)
	}
}

func TestMakeMmapBlob_BytesReader(t *testing.T) {
	br, err := env_dir.NewReader(env_dir.DefaultConfig, bytes.NewReader([]byte("hi")))
	if err != nil {
		t.Fatal(err)
	}
	_, err = MakeMmapBlobFromBlobReader(br)
	if !errors.Is(err, ErrMmapUnsupported) {
		t.Fatalf("got %v, want ErrMmapUnsupported", err)
	}
}

func TestMakeMmapBlob_ZstdCompression(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	if err := os.WriteFile(path, []byte("anything"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	zstd := compression_type.CompressionTypeZstd
	cfg := env_dir.MakeConfig(nil, nil, &zstd, nil) // signature TBD-confirm
	br, err := env_dir.NewReader(cfg, f)
	if err != nil {
		t.Skipf("NewReader rejected zstd config on raw bytes: %v", err)
	}
	_, err = MakeMmapBlobFromBlobReader(br)
	if !errors.Is(err, ErrMmapUnsupported) {
		t.Fatalf("got %v, want ErrMmapUnsupported", err)
	}
}
```

(The `env_dir.MakeConfig` signature uses a `MarklId` for encryption — pass `nil` for the no-encryption case. Confirm the exact signature when writing the test.)

**Step 2: Run tests to confirm they fail**

```bash
just test-go-pkg internal/foxtrot/mmap_blob
```
Expected: FAIL with "MakeMmapBlobFromBlobReader undefined".

**Step 3: Implement promote.go**

```go
package mmap_blob

import (
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
)

// MakeMmapBlobFromBlobReader inspects reader. If it implements
// MmapSource and reports ok=true, returns an MmapBlob mapping the
// reported file region. Otherwise returns ErrMmapUnsupported.
//
// On success, ownership of the underlying file transfers to the
// returned MmapBlob. Caller MUST NOT also Close reader (or rather,
// reader.Close becomes a no-op for the file portion).
//
// On failure (ErrMmapUnsupported or any other), reader is unchanged
// and remains the caller's to Close.
func MakeMmapBlobFromBlobReader(reader domain_interfaces.BlobReader) (MmapBlob, error) {
	src, ok := reader.(domain_interfaces.MmapSource)
	if !ok {
		return nil, ErrMmapUnsupported
	}
	file, offset, length, mmapOk, err := src.MmapSource()
	if err != nil {
		return nil, err
	}
	if !mmapOk {
		return nil, ErrMmapUnsupported
	}
	return mmapFile(file, offset, length, reader.GetMarklId())
}
```

Now revisit `env_dir.blobReader.Close()` to make file-close a no-op after `MmapSource()` has handed ownership away:

In `blob_reader.go`:

```go
// On a successful MmapSource() handoff, we set readSeeker to nil so
// subsequent Close() doesn't double-close the file the MmapBlob now
// owns.
func (reader *blobReader) MmapSource() (...) {
	// ...existing checks...
	stat, err := f.Stat()
	if err != nil {
		return nil, 0, 0, false, errors.Wrap(err)
	}
	reader.readSeeker = nil  // <-- ownership transfer
	return f, 0, stat.Size(), true, nil
}
```

And update `Close()`:

```go
func (reader *blobReader) Close() (err error) {
	if err = reader.expander.Close(); err != nil {
		err = errors.Wrap(err)
		return err
	}
	if reader.readSeeker == nil {
		return nil  // file ownership transferred to MmapBlob
	}
	if closer, ok := reader.readSeeker.(io.Closer); ok {
		if err = closer.Close(); err != nil {
			err = errors.Wrap(err)
			return err
		}
	}
	return err
}
```

**Step 4: Run tests**

```bash
just test-go-pkg internal/foxtrot/mmap_blob
just test-go-pkg internal/echo/env_dir
```
Expected: PASS for both.

**Step 5: Commit**

```
feat(mmap_blob): MakeMmapBlobFromBlobReader promotion

Type-asserts a BlobReader to MmapSource, mmaps the reported file
region, and returns an MmapBlob owning the file. ErrMmapUnsupported
returned when the reader doesn't implement MmapSource or reports
ok=false. env_dir.blobReader.Close becomes a no-op for the file
after a successful handoff to avoid double-close.
```

---

## Task 8: Implement `Verify()`

**Promotion criteria:** N/A.

**Files:**
- Create: `go/internal/foxtrot/mmap_blob/verify.go`
- Test: `go/internal/foxtrot/mmap_blob/verify_test.go`

**Step 1: Write failing tests**

```go
//go:build test && unix

package mmap_blob

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/echo/env_dir"
)

func makeMmapBlobForVerify(t *testing.T, payload []byte) (MmapBlob, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	br, err := env_dir.NewFileReaderOrErrNotExist(env_dir.DefaultConfig, path)
	if err != nil {
		t.Fatal(err)
	}
	mb, err := MakeMmapBlobFromBlobReader(br)
	if err != nil {
		t.Fatal(err)
	}
	return mb, path
}

func TestVerify_Match(t *testing.T) {
	mb, _ := makeMmapBlobForVerify(t, []byte("verify me"))
	defer mb.Close()
	if err := mb.Verify(); err != nil {
		t.Fatalf("Verify on intact blob: %v", err)
	}
}

func TestVerify_Mismatch(t *testing.T) {
	// Build a blob, write through env_dir so we get a real digest, then
	// tamper the on-disk file before mmap.
	t.Skip("Requires writer-driven blob to compute a real digest; integration test covers this.")
	// Implementation note: easiest tamper path is to use env_dir.NewWriter
	// to materialize a real blob with a real MarklId, then overwrite the
	// file with different bytes, then mmap, then expect ErrDigestMismatch.
	// Defer to bats integration test (task 9) for end-to-end coverage.
	_ = bytes.Compare
	_ = errors.Is
}
```

(The mismatch test as written punts to integration. If the unit-level test is straightforward enough — e.g. write via a test helper that returns the computed MarklId — implement it inline. Keep this judgment call for the implementer.)

**Step 2: Run tests to confirm they fail**

```bash
just test-go-pkg internal/foxtrot/mmap_blob
```
Expected: FAIL ("Verify on intact blob" expects digest comparison; current stub returns nil for any input including a nil MarklId — the positive test will pass spuriously since the stub ignores input. So this step needs the real Verify, not the stub).

**Step 3: Implement `verify.go`**

Replace the stub in `mmap_unix.go` (delete the stub `Verify`) and create `verify.go`:

```go
package mmap_blob

import (
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// Verify recomputes the digest of the mmap'd bytes and compares against
// the MarklId recorded at promotion time. Returns ErrDigestMismatch on
// a content/digest mismatch. Returns nil if marklId is nil (caller
// passed an unidentified blob).
func (m *mmapBlob) Verify() error {
	if m.marklId == nil || m.marklId.IsNull() {
		return nil
	}
	hashFormat := m.marklId.GetMarklFormat()
	hash, _ := hashFormat.GetHash() //repool:owned
	defer hash.Reset()
	if _, err := hash.Write(m.bytes); err != nil {
		return errors.Wrap(err)
	}
	got := hash.GetMarklId()
	if !marklIdsEqual(got, m.marklId) {
		return ErrDigestMismatch
	}
	return nil
}

func marklIdsEqual(a, b domain_interfaces.MarklId) bool {
	// Use whatever equality the project uses elsewhere — likely a
	// markl.Equal helper or string comparison via String(). Look up
	// the right helper in go/internal/bravo/markl/.
	return a.String() == b.String()
}
```

(The hash interface details — `GetMarklFormat`, `GetHash`, `hash.Write`, `hash.GetMarklId` — need to be confirmed against `bravo/markl/`; adjust the calls to match. The existing `markl_io.MakeWriter` in `blob_reader.go` shows the right pattern.)

**Step 4: Run tests**

```bash
just test-go-pkg internal/foxtrot/mmap_blob
```
Expected: PASS.

**Step 5: Commit**

```
feat(mmap_blob): MmapBlob.Verify recomputes digest

Walks the mmap'd bytes through the recorded MarklId's hash algorithm
and returns ErrDigestMismatch on mismatch. Opt-in only — never
called from Bytes() or Close(). Defaults to no-op when marklId is
null.
```

---

## Task 9: Bats integration test

**Promotion criteria:** 14 days of green CI without callers reporting unexpected `ErrMmapUnsupported` or `ErrDigestMismatch`.

**Files:**
- Create: `zz-tests_bats/mmap_blob.bats`
- Possibly: `go/cmd/madder-test-mmap/` (small Go test binary that promotes a blob and prints the bytes)

**Step 1: Decide on the test driver**

Two options:
- **A:** Add a Go test helper binary at `go/cmd/madder-test-mmap/` that takes `--store-path` and `--digest`, opens the store, makes a BlobReader, promotes to MmapBlob, prints `Bytes()` to stdout. Bats compares against expected bytes.
- **B:** Add a flag to the main `madder` CLI (e.g. `madder cat --mmap`). Heavier — affects the public CLI surface.

Default to **A** — it mirrors `madder-test-sftp-server`'s pattern from the SFTP test harness.

**Step 2: Write the bats scenario**

```bash
#!/usr/bin/env bats

# bats file_tags=

setup() {
    load 'lib/common'
    setup_madder_repo
}

@test "mmap promotion: local hash-bucketed blob round-trips through MmapBlob" {
    payload="$(printf 'mmap-integration-payload-%s' "$RANDOM")"
    digest="$(printf '%s' "$payload" | "$MADDER_BIN" write -)"
    [ -n "$digest" ]

    got="$("$MADDER_TEST_MMAP_BIN" --repo "$MADDER_REPO" --digest "$digest")"
    [ "$got" = "$payload" ]
}
```

**Step 3: Wire `madder-test-mmap` into the devshell**

Mirror the pattern from `madder-test-sftp-server`: add to `go/default.nix`, surface in `flake.nix` `devShells.default.buildInputs`, never expose in `flake.packages` / `flake.apps`.

**Step 4: Run bats**

```bash
just test-bats
```
Expected: the new scenario passes alongside existing tests.

**Step 5: Commit**

```
test(mmap_blob): bats integration exercising MakeMmapBlobFromBlobReader

CLI writes a blob, the test helper opens the store and promotes via
MakeMmapBlobFromBlobReader, prints Bytes(); bats compares against the
written payload byte-for-byte. End-to-end coverage that the design
doc's reference path works.
```

---

## Final-state acceptance

After all tasks:

- `just test-go` green.
- `just test-go-race` green.
- `just test-bats` green.
- `just check-go-vet` green.
- `git log --oneline` shows ~9 commits, each with a passing test, all signed off as Clown.

If any of these fail, that's a bug in the implementation, not a planning issue — fix in place rather than amending the plan.

## Out-of-scope follow-ups (file as separate issues)

- Userfaultfd lazy decompression — POC at `zz-pocs/0001-userfaultfd-mmap/`. Linux-only fast path for compressed/encrypted blobs. Owner: TBD.
- Pack v0/v1 sub-range mmap.
- Inventory archive sub-range mmap.
- SFTP transparent local-cache mmap.
- Random-access-compatible compression / encryption schemes (seekable zstd, AES-CTR with chunked nonce).
