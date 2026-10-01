//go:build test

package blob_stores

import (
	"os"
	"path/filepath"
	"testing"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_io"
)

// writeAliasedBlob writes payload natively into a blake2b256 multi-hash
// store and registers the same bytes' sha256 digest as a foreign alias for
// it — the state a cross-hash sync leaves behind. Returns the store and
// the (foreign, native) pair.
func writeAliasedBlob(
	t *testing.T,
	payload []byte,
) (store localHashBucketed, foreign, native domain_interfaces.MarklId) {
	t.Helper()

	store = makeTestStoreBlake2b256(t)

	var err error

	if native, err = writeBlob(store, payload); err != nil {
		t.Fatalf("writing native blob: %v", err)
	}

	// The sha256 digest of the same bytes, taken from a throwaway sha256
	// store rather than hashed by hand.
	if foreign, err = writeBlob(makeTestStore(t), payload); err != nil {
		t.Fatalf("computing foreign digest: %v", err)
	}

	if err = store.AddForeignBlobDigestForNativeDigest(foreign, native); err != nil {
		t.Fatalf("AddForeignBlobDigestForNativeDigest: %v", err)
	}

	return store, foreign, native
}

func TestResolveForeignBlobDigest_Alias(t *testing.T) {
	store, foreign, native := writeAliasedBlob(t, []byte("aliased payload"))

	got, ok, err := store.ResolveForeignBlobDigest(foreign)
	if err != nil {
		t.Fatalf("ResolveForeignBlobDigest: %v", err)
	}

	if !ok {
		t.Fatalf("ok = false for a registered alias")
	}

	if got.String() != native.String() {
		t.Fatalf("resolved %s, want %s", got, native)
	}
}

func TestResolveForeignBlobDigest_Absent(t *testing.T) {
	store, _, _ := writeAliasedBlob(t, []byte("aliased payload"))

	absent, err := writeBlob(makeTestStore(t), []byte("never written to store"))
	if err != nil {
		t.Fatalf("computing absent digest: %v", err)
	}

	got, ok, err := store.ResolveForeignBlobDigest(absent)
	if err != nil || ok || got != nil {
		t.Fatalf("got (%v, %v, %v), want (nil, false, nil)", got, ok, err)
	}
}

// A blob stored natively under the foreign digest's hash type is a real
// file, not an alias: there is nothing to map.
func TestResolveForeignBlobDigest_NativeFileIsNotAlias(t *testing.T) {
	store := makeTestStoreBlake2b256(t)

	writer, err := store.MakeBlobWriter(makeTestStore(t).GetDefaultHashType())
	if err != nil {
		t.Fatalf("MakeBlobWriter(sha256): %v", err)
	}

	if _, err = writer.Write([]byte("native sha256 payload")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err = writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, ok, err := store.ResolveForeignBlobDigest(writer.GetMarklId())
	if err != nil || ok || got != nil {
		t.Fatalf("got (%v, %v, %v), want (nil, false, nil)", got, ok, err)
	}
}

func TestResolveForeignBlobDigest_SingleHashStore(t *testing.T) {
	store, foreign, _ := writeAliasedBlob(t, []byte("aliased payload"))
	store.multiHash = false

	got, ok, err := store.ResolveForeignBlobDigest(foreign)
	if err != nil || ok || got != nil {
		t.Fatalf("got (%v, %v, %v), want (nil, false, nil)", got, ok, err)
	}
}

// An alias whose target escapes the store's hash-type trees cannot name a
// digest; that is corruption, reported rather than mapped.
func TestResolveForeignBlobDigest_TargetOutsideStore(t *testing.T) {
	store, foreign, _ := writeAliasedBlob(t, []byte("aliased payload"))

	foreignPath := blob_io.MakeHashBucketPathFromMerkleId(
		foreign,
		store.buckets,
		store.multiHash,
		store.basePath,
	)

	if err := os.Remove(foreignPath); err != nil {
		t.Fatalf("removing alias: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "elsewhere")

	if err := os.Symlink(outside, foreignPath); err != nil {
		t.Fatalf("planting escaping alias: %v", err)
	}

	if _, ok, err := store.ResolveForeignBlobDigest(foreign); err == nil || ok {
		t.Fatalf("got (ok=%v, err=%v), want an error", ok, err)
	}
}
