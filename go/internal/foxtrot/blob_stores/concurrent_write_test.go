//go:build test

package blob_stores

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	charlie_bsc "github.com/amarbel-llc/madder/go/internal/charlie/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/echo/env_dir"
	"github.com/amarbel-llc/purse-first/libs/dewey/delta/compression_type"
)

// makeTestStore constructs a localHashBucketed backed by t.TempDir() paths.
// Both basePath and tempPath come from t.TempDir(), which places them under
// the test runner's tmpdir — same filesystem, so link(2) (ADR 0003) works
// in tests mirroring the production expectation that XDG_CACHE_HOME and
// the blob store's base path share a mount.
func makeTestStore(t *testing.T) localHashBucketed {
	return makeTestStoreWithVerify(t, false)
}

// makeTestStoreWithVerify is the variant used by the #31 happy-path test.
// When verifyOnCollision is true, the EEXIST branch in blob_mover.Close
// opens both paths as BlobReaders and byte-compares their decoded streams.
func makeTestStoreWithVerify(t *testing.T, verifyOnCollision bool) localHashBucketed {
	t.Helper()

	basePath := t.TempDir()
	tempPath := t.TempDir()

	config := &blob_store_configs.DefaultType{
		HashBuckets:       blob_store_configs.DefaultHashBuckets,
		HashTypeId:        charlie_bsc.HashTypeSha256,
		CompressionType:   compression_type.CompressionTypeNone,
		VerifyOnCollision: verifyOnCollision,
	}

	return localHashBucketed{
		config:            config,
		multiHash:         config.SupportsMultiHash(),
		defaultHashFormat: markl.FormatHashSha256,
		buckets:           config.GetHashBuckets(),
		basePath:          basePath,
		tempFS:            env_dir.TemporaryFS{BasePath: tempPath},
		verifyOnCollision: verifyOnCollision,
	}
}

// writeBlob writes payload via a fresh BlobWriter and returns the final
// digest, or the first error encountered.
func writeBlob(
	store localHashBucketed,
	payload []byte,
) (domain_interfaces.MarklId, error) {
	writer, err := store.MakeBlobWriter(nil)
	if err != nil {
		return nil, fmt.Errorf("MakeBlobWriter: %w", err)
	}

	if _, err := writer.Write(payload); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("Write: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("Close: %w", err)
	}

	return writer.GetMarklId(), nil
}

// readBlob reads the blob for the given id and returns its bytes.
func readBlob(
	t *testing.T,
	store localHashBucketed,
	id domain_interfaces.MarklId,
) []byte {
	t.Helper()

	reader, err := store.MakeBlobReader(id)
	if err != nil {
		t.Fatalf("MakeBlobReader(%s): %v", id, err)
	}

	defer reader.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}

	return buf.Bytes()
}

// runConcurrent launches n goroutines that each call fn(i), releases them
// together via a starting-gun channel, and returns their collected digests in
// index order. Any error from fn fails the test.
func runConcurrent(
	t *testing.T,
	n int,
	fn func(i int) (domain_interfaces.MarklId, error),
) []domain_interfaces.MarklId {
	t.Helper()

	var (
		wg      sync.WaitGroup
		ready   = make(chan struct{})
		digests = make([]domain_interfaces.MarklId, n)
		errs    = make([]error, n)
	)

	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-ready
			digests[idx], errs[idx] = fn(idx)
		}(i)
	}

	close(ready)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}

	return digests
}

// TestConcurrentBlobWritesSameContent spawns N concurrent writers that all
// write identical payloads. All must produce the same digest, the blob must
// be present at that digest, and reading it back must yield the original
// bytes. Exercises the same-digest race that motivates ADR 0002.
func TestConcurrentBlobWritesSameContent(t *testing.T) {
	const n = 32

	store := makeTestStore(t)
	payload := []byte("shared payload for same-content concurrent writers")

	digests := runConcurrent(t, n, func(int) (domain_interfaces.MarklId, error) {
		return writeBlob(store, payload)
	})

	first := digests[0].String()
	for i, d := range digests {
		if d.String() != first {
			t.Fatalf("goroutine %d produced digest %s, expected %s", i, d, first)
		}
	}

	if !store.HasBlob(digests[0]) {
		t.Fatalf("HasBlob(%s) = false after concurrent writes", digests[0])
	}

	got := readBlob(t, store, digests[0])
	if !bytes.Equal(got, payload) {
		t.Fatalf("read-back mismatch: got %q, want %q", got, payload)
	}

	// ADR 0003: published blobs are chmod 0o444 before link, so the inode
	// is read-only from birth regardless of which writer won the race.
	blobPath := env_dir.MakeHashBucketPathFromMerkleId(
		digests[0],
		store.buckets,
		store.multiHash,
		store.basePath,
	)

	info, err := os.Stat(blobPath)
	if err != nil {
		t.Fatalf("Stat(%s): %v", blobPath, err)
	}

	if mode := info.Mode().Perm(); mode != 0o444 {
		t.Errorf("blob mode = %o, want 0o444", mode)
	}

	// ADR 0003: every write unlinks its temp file on both the happy path and
	// the EEXIST (duplicate-write) path. After 32 same-content writers, the
	// per-store tmp dir must be empty.
	entries, err := os.ReadDir(store.tempFS.BasePath)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", store.tempFS.BasePath, err)
	}

	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("temp dir %s has %d leftover entries: %v", store.tempFS.BasePath, len(entries), names)
	}
}

// TestConcurrentBlobWritesDistinctContent spawns N concurrent writers with
// distinct payloads. Each must produce a unique digest, all N blobs must be
// present, and read-back must yield each writer's original bytes. Exercises
// the happy path with no digest collisions.
func TestConcurrentBlobWritesDistinctContent(t *testing.T) {
	const n = 32

	store := makeTestStore(t)

	payloads := make([][]byte, n)
	for i := range payloads {
		payloads[i] = []byte(fmt.Sprintf("distinct blob %03d with padding %s", i, strings.Repeat("x", i*3)))
	}

	digests := runConcurrent(t, n, func(i int) (domain_interfaces.MarklId, error) {
		return writeBlob(store, payloads[i])
	})

	seen := make(map[string]int, n)
	for i, d := range digests {
		if prev, dup := seen[d.String()]; dup {
			t.Fatalf("goroutines %d and %d collided on digest %s", prev, i, d)
		}
		seen[d.String()] = i
	}

	for i, d := range digests {
		if !store.HasBlob(d) {
			t.Fatalf("HasBlob(%s) = false for goroutine %d", d, i)
		}

		got := readBlob(t, store, d)
		if !bytes.Equal(got, payloads[i]) {
			t.Fatalf("goroutine %d read-back mismatch", i)
		}
	}
}

// TestConcurrentBlobWritesMixed spawns N concurrent writers split into two
// halves writing payload X and payload Y respectively. Both final blobs must
// exist and read back correctly. Exercises interleaved same-digest races
// alongside distinct-digest writes.
func TestConcurrentBlobWritesMixed(t *testing.T) {
	const n = 32

	store := makeTestStore(t)
	payloadX := []byte("payload X — goroutines with even indices race on this digest")
	payloadY := []byte("payload Y — goroutines with odd indices race on this one")

	digests := runConcurrent(t, n, func(i int) (domain_interfaces.MarklId, error) {
		if i%2 == 0 {
			return writeBlob(store, payloadX)
		}
		return writeBlob(store, payloadY)
	})

	var (
		digestX = digests[0]
		digestY = digests[1]
	)

	for i, d := range digests {
		var want domain_interfaces.MarklId
		if i%2 == 0 {
			want = digestX
		} else {
			want = digestY
		}
		if d.String() != want.String() {
			t.Fatalf("goroutine %d: digest %s, expected %s", i, d, want)
		}
	}

	if digestX.String() == digestY.String() {
		t.Fatalf("payloads X and Y produced identical digest %s; pick distinct payloads", digestX)
	}

	for _, pair := range []struct {
		id      domain_interfaces.MarklId
		want    []byte
		payload string
	}{
		{digestX, payloadX, "X"},
		{digestY, payloadY, "Y"},
	} {
		if !store.HasBlob(pair.id) {
			t.Fatalf("HasBlob(%s) = false for payload %s", pair.id, pair.payload)
		}

		got := readBlob(t, store, pair.id)
		if !bytes.Equal(got, pair.want) {
			t.Fatalf("payload %s read-back mismatch: got %q, want %q", pair.payload, got, pair.want)
		}
	}
}

// TestConcurrentBlobWritesSameContent_VerifyOnCollision is the happy-path
// exercise of the #31 verify-on-collision flag: a store with the flag on
// runs the same 32-goroutine same-content write pattern as
// TestConcurrentBlobWritesSameContent. Because all goroutines write the
// same bytes, every EEXIST branch should pass checkCollision and unlink
// the temp cleanly. All writers must succeed, the resulting blob must be
// readable, and no temp files should remain.
func TestConcurrentBlobWritesSameContent_VerifyOnCollision(t *testing.T) {
	const n = 32

	store := makeTestStoreWithVerify(t, true)
	payload := []byte("shared payload for verify-on-collision happy path")

	digests := runConcurrent(t, n, func(int) (domain_interfaces.MarklId, error) {
		return writeBlob(store, payload)
	})

	first := digests[0].String()
	for i, d := range digests {
		if d.String() != first {
			t.Fatalf("goroutine %d produced digest %s, expected %s", i, d, first)
		}
	}

	if !store.HasBlob(digests[0]) {
		t.Fatalf("HasBlob(%s) = false after concurrent verify-on-collision writes", digests[0])
	}

	got := readBlob(t, store, digests[0])
	if !bytes.Equal(got, payload) {
		t.Fatalf("read-back mismatch: got %q, want %q", got, payload)
	}

	entries, err := os.ReadDir(store.tempFS.BasePath)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", store.tempFS.BasePath, err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("temp dir %s has %d leftover entries after verify-on-collision run: %v",
			store.tempFS.BasePath, len(entries), names)
	}
}
