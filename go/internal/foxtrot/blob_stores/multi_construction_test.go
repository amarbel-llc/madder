//go:build test

package blob_stores

import (
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/ids"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/bravo/directory_layout"
	_ "github.com/amarbel-llc/madder/go/internal/charlie/markl_registrations"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
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
		blob_store_id.Make(name),
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

// digestRef returns the typed digest-bearing blob_store_id.Id a parent multi
// stores as a reference: the leaf's bare id pinned to its Config.BlobDigest.
func digestRef(bs BlobStoreInitialized) blob_store_id.Id {
	return bs.Path.GetId().WithDigest(bs.Config.BlobDigest)
}

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
	// "ghost" is digest-bearing and well-formed but not present in the map.
	ghost := blob_store_id.Make("ghost").WithDigest(digestSeeded(t, 0x09))
	orphan := multiLeafForTest(t, "orphan", &blob_store_configs.TomlMultiV0{
		Mode:         "mirror",
		MirrorStores: []blob_store_id.Id{ghost},
	})
	stores := MakeBlobStoreMap(orphan)

	if err := buildMultiStores(testCtx(t), stores); err == nil {
		t.Fatal("expected dangling-ref error, got nil")
	}
}
