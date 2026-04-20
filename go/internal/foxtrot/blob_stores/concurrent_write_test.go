//go:build test

package blob_stores

import (
	"bytes"
	"fmt"
	"io"
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

// makeTestStore constructs a localHashBucketed backed entirely by t.TempDir()
// paths, with LockInternalFiles disabled so the test does not interact with
// the chmod race tracked in https://github.com/amarbel-llc/madder/issues/29.
func makeTestStore(t *testing.T) localHashBucketed {
	t.Helper()

	basePath := t.TempDir()
	tempPath := t.TempDir()

	config := &blob_store_configs.DefaultType{
		HashBuckets:       blob_store_configs.DefaultHashBuckets,
		HashTypeId:        charlie_bsc.HashTypeSha256,
		CompressionType:   compression_type.CompressionTypeNone,
		LockInternalFiles: false,
	}

	return localHashBucketed{
		config:            config,
		multiHash:         config.SupportsMultiHash(),
		defaultHashFormat: markl.FormatHashSha256,
		buckets:           config.GetHashBuckets(),
		basePath:          basePath,
		tempFS:            env_dir.TemporaryFS{BasePath: tempPath},
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
