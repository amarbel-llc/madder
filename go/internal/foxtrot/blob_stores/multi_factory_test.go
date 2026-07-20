//go:build test

package blob_stores

import (
	"testing"

	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/bravo/directory_layout"
	_ "code.linenisgreat.com/madder/go/internal/charlie/markl_registrations"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

// testCtx returns an ActiveContext sufficient for NewMulti(...).Build().
// The Multi builder only stashes the context for later tee/ReadFill
// callbacks; a plain build never dereferences it, so the package's
// existing spyActiveContext (store_remote_sftp_test.go) is enough.
func testCtx(t *testing.T) interfaces.ActiveContext {
	t.Helper()
	return &spyActiveContext{}
}

// bytesSeeded returns a deterministic 32-byte slice filled with seed.
// blake2b256 digests are 32 bytes, so this is a valid raw digest body.
func bytesSeeded(seed byte) []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = seed
	}
	return b
}

// digestSeeded builds a concrete blake2b256 markl.Id from a seed byte.
// Used both as a leaf's Config.BlobDigest and (via WithDigest) as a
// reference's pinned digest so resolveMultiRef's AssertEqual can match.
func digestSeeded(t *testing.T, seed byte) markl.Id {
	t.Helper()
	var d markl.Id
	if err := d.SetMarklId(markl.FormatIdHashBlake2b256, bytesSeeded(seed)); err != nil {
		t.Fatalf("SetMarklId(seed=%#x): %v", seed, err)
	}
	return d
}

// builtLeafForTest returns a BlobStoreInitialized whose BlobStore field
// is a non-nil multiModeStub (i.e. "already built") and whose
// Config.BlobDigest carries a blake2b256 markl.Id seeded by digestSeed.
//
// There is no in-memory BlobStore constructor in this package; the
// multiModeStub double (multi_test.go) is the lightest non-nil store the
// Multi builder's Build() accepts — a plain build never exercises its
// methods. The leaf's Path keys the BlobStoreMap on name (MakeBlobStoreMap
// uses Path.GetId().String()), and Config.BlobDigest is what
// resolveMultiRef asserts each reference's pinned digest against.
func builtLeafForTest(
	t *testing.T,
	name string,
	digestSeed byte,
) BlobStoreInitialized {
	t.Helper()

	var bs BlobStoreInitialized
	bs.BlobStore = &multiModeStub{}
	bs.ConfigNamed.Path = directory_layout.MakeBlobStorePath(
		scoped_id.Make(name),
		"", // base — unused by the factory for already-built leaves
		"", // config path — same
	)
	bs.ConfigNamed.Config = blob_store_configs.TypedConfig{
		BlobDigest: digestSeeded(t, digestSeed),
	}
	return bs
}

func TestMakeMultiStore_WriteThrough(t *testing.T) {
	write := builtLeafForTest(t, "default", 0x01)
	read := builtLeafForTest(t, "archive", 0x02)

	stores := MakeBlobStoreMap(write, read)

	readFill := true
	cfg := &blob_store_configs.TomlMultiV0{
		Mode:       "write_through",
		WriteStore: write.Path.GetId().WithDigest(write.Config.BlobDigest),
		ReadStores: []scoped_id.Id{
			read.Path.GetId().WithDigest(read.Config.BlobDigest),
		},
		ReadFill: &readFill,
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
	wrong := digestSeeded(t, 0xFF)

	cfg := &blob_store_configs.TomlMultiV0{
		Mode: "mirror",
		MirrorStores: []scoped_id.Id{
			write.Path.GetId().WithDigest(wrong),
		},
	}

	if _, err := makeMultiStore(testCtx(t), cfg, stores); err == nil {
		t.Fatal("expected digest-mismatch error, got nil")
	}
}
