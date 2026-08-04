//go:build test

package blob_stores

import (
	"testing"

	"code.linenisgreat.com/madder/go/internal/0/ids"
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/bravo/directory_layout"
	_ "code.linenisgreat.com/madder/go/internal/charlie/markl_registrations"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
)

// multiLeafForTest returns a BlobStoreInitialized representing a not-yet-built
// multi store: BlobStore is nil, Path keys the map on name, and Config carries
// the multi type struct, the given *TomlMultiV0 blob, and a non-null seeded
// BlobDigest so a parent multi's resolveMultiRef can assert against it.
//
// digestRef(bs) yields the typed digest-bearing reference a parent multi
// stores; buildMultiStores resolves it against this leaf's Config.BlobDigest.
func multiLeafForTest(
	t *testing.T,
	name string,
	cfg *blob_store_configs.TomlMultiV0,
) BlobStoreInitialized {
	t.Helper()

	var bs BlobStoreInitialized
	// BlobStore intentionally nil — buildMultiStores is what builds it.
	bs.ConfigNamed.Path = directory_layout.MakeBlobStorePath(
		scoped_id.Make(name),
		"", // base — unused by the multi factory
		"", // config path — same
	)
	bs.ConfigNamed.Config = blob_store_configs.TypedConfig{
		Type:       ids.GetOrPanic(ids.TypeTomlBlobStoreConfigMultiV0).TypeStruct,
		Blob:       cfg,
		BlobDigest: digestSeeded(t, 0x42),
	}
	return bs
}

// digestRef returns the typed digest-bearing scoped_id.Id a parent multi
// stores as a reference: the leaf's bare id pinned to its Config.BlobDigest.
func digestRef(bs BlobStoreInitialized) scoped_id.Id {
	return bs.Path.GetId().WithDigest(bs.Config.BlobDigest)
}

func TestBuildMultiStores_Nested(t *testing.T) {
	ssd := builtLeafForTest(t, ".ssd", 0x01)
	nvme := builtLeafForTest(t, ".nvme", 0x02)

	fast := multiLeafForTest(t, "fast", &blob_store_configs.TomlMultiV0{
		Mode: "mirror",
		MirrorStores: []scoped_id.Id{
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

// cwdId parses a Cwd-scoped id (`.name`) and tags it with the given
// walk-up depth — the shape makeAncestorOverrideStores produces for a
// store discovered `depth` ancestors up (`.name` at depth 0, `..name` at
// depth 1, …).
func cwdId(t *testing.T, name string, depth uint) scoped_id.Id {
	t.Helper()

	var id scoped_id.Id
	if err := id.Set("." + name); err != nil {
		t.Fatalf("Set(.%s): %v", name, err)
	}
	return id.WithCwdDepth(depth)
}

// cwdRef builds the reference an ancestor multi config stores for a
// cwd-scoped member: canonical single-dot (`.name`, depth 0 — the on-wire
// form, #145) pinned to the member's config digest. The parent multi's
// own discovered depth, not the ref's, decides which ancestor it resolves
// to (FDR-0010 config-location-relative resolution).
func cwdRef(t *testing.T, name string, digestSeed byte) scoped_id.Id {
	t.Helper()
	return cwdId(t, name, 0).WithDigest(digestSeeded(t, digestSeed))
}

// builtCwdLeafForTest is builtLeafForTest with a Cwd-scoped, depth-tagged
// id instead of an XDG-user one, so its map key carries the dot-prefix
// (`.name` / `..name`) the ancestor walk-up assigns.
func builtCwdLeafForTest(
	t *testing.T,
	name string,
	depth uint,
	digestSeed byte,
) BlobStoreInitialized {
	t.Helper()

	var bs BlobStoreInitialized
	bs.BlobStore = &multiModeStub{}
	bs.ConfigNamed.Path = directory_layout.MakeBlobStorePath(
		cwdId(t, name, depth), "", "",
	)
	bs.ConfigNamed.Config = blob_store_configs.TypedConfig{
		BlobDigest: digestSeeded(t, digestSeed),
	}
	return bs
}

// multiCwdLeafForTest is multiLeafForTest with a Cwd-scoped, depth-tagged
// id: an as-yet-unbuilt multi discovered `depth` ancestors up.
func multiCwdLeafForTest(
	t *testing.T,
	name string,
	depth uint,
	cfg *blob_store_configs.TomlMultiV0,
) BlobStoreInitialized {
	t.Helper()

	var bs BlobStoreInitialized
	bs.ConfigNamed.Path = directory_layout.MakeBlobStorePath(
		cwdId(t, name, depth), "", "",
	)
	bs.ConfigNamed.Config = blob_store_configs.TypedConfig{
		Type:       ids.GetOrPanic(ids.TypeTomlBlobStoreConfigMultiV0).TypeStruct,
		Blob:       cfg,
		BlobDigest: digestSeeded(t, 0x42),
	}
	return bs
}

// TestBuildMultiStores_LocationRelativeMemberResolution reproduces the
// cross-scope resolution bug FDR-0010 M1's instance-id entropy exposed:
// two nested CWD scopes, each with its own `default-local` (distinct
// instance digests), and an ancestor `default-notes` multi that pins the
// ANCESTOR's `default-local`.
//
// Building the store map from the child directory must resolve the
// ancestor multi's `.default-local` member relative to the multi config's
// OWN location (`..default-local`, the ancestor's store), not the process
// cwd's `.default-local` (the child's store). Before the fix the ancestor
// multi resolved to the child's store, whose distinct instance digest
// failed the pin assertion — the exact
// `resolveMultiRef -> buildMultiStores` failure caught in dodder's bats
// lane. This test fails against the pre-fix resolveMultiRef.
func TestBuildMultiStores_LocationRelativeMemberResolution(t *testing.T) {
	const childSeed, ancestorSeed = 0x01, 0x02

	childLocal := builtCwdLeafForTest(t, "default-local", 0, childSeed)
	ancestorLocal := builtCwdLeafForTest(t, "default-local", 1, ancestorSeed)

	childNotes := multiCwdLeafForTest(t, "default-notes", 0,
		&blob_store_configs.TomlMultiV0{
			Mode:       "write_through",
			WriteStore: cwdRef(t, "default-local", childSeed),
		})
	ancestorNotes := multiCwdLeafForTest(t, "default-notes", 1,
		&blob_store_configs.TomlMultiV0{
			Mode:       "write_through",
			WriteStore: cwdRef(t, "default-local", ancestorSeed),
		})

	stores := MakeBlobStoreMap(childLocal, ancestorLocal, childNotes, ancestorNotes)

	if err := buildMultiStores(testCtx(t), stores); err != nil {
		t.Fatalf("buildMultiStores: %v", err)
	}

	for _, key := range []string{".default-notes", "..default-notes"} {
		if stores[key].BlobStore == nil {
			t.Errorf("multi %q not built", key)
		}
	}
}

// TestResolveMultiRef_CwdRefRebasedToConfigDepth pins the resolution
// primitive directly: the SAME canonical `.widgets@<ancestor-digest>`
// reference resolves to the child store at configDepth 0 (digest
// mismatch, since the ref pins the ancestor's instance) and to the
// ancestor store at configDepth 1 (match). The resolved store's own id
// string is the ground truth for which physical store was selected.
func TestResolveMultiRef_CwdRefRebasedToConfigDepth(t *testing.T) {
	childLocal := builtCwdLeafForTest(t, "widgets", 0, 0x01)
	ancestorLocal := builtCwdLeafForTest(t, "widgets", 1, 0x02)
	stores := MakeBlobStoreMap(childLocal, ancestorLocal)

	// The reference as written in the ancestor's config: canonical
	// single-dot, pinned to the ANCESTOR's instance digest (0x02).
	ref := cwdRef(t, "widgets", 0x02)

	// configDepth 0 resolves the process cwd's own `.widgets` (the child,
	// 0x01) — a different instance, so the pin fails.
	if _, err := resolveMultiRef(ref, 0, stores); err == nil {
		t.Fatal("configDepth 0: expected digest mismatch against the child store")
	}

	// configDepth 1 (multi discovered at `..widgets`) rebases `.widgets`
	// -> `..widgets`, resolving the ancestor store, whose digest matches.
	resolved, err := resolveMultiRef(ref, 1, stores)
	if err != nil {
		t.Fatalf("configDepth 1: %v", err)
	}
	if got := resolved.Path.GetId().String(); got != "..widgets" {
		t.Errorf("resolved to %q, want %q (the ancestor store)", got, "..widgets")
	}
}

func TestBuildMultiStores_DanglingRef(t *testing.T) {
	// "ghost" is digest-bearing and well-formed but not present in the map.
	ghost := scoped_id.Make("ghost").WithDigest(digestSeeded(t, 0x09))
	orphan := multiLeafForTest(t, "orphan", &blob_store_configs.TomlMultiV0{
		Mode:         "mirror",
		MirrorStores: []scoped_id.Id{ghost},
	})
	stores := MakeBlobStoreMap(orphan)

	if err := buildMultiStores(testCtx(t), stores); err == nil {
		t.Fatal("expected dangling-ref error, got nil")
	}
}
