//go:build test

package blob_store_configs

import (
	"testing"

	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	_ "github.com/amarbel-llc/madder/go/internal/charlie/markl_registrations"
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
		WriteStore: mustId(t, "default@blake2b256-c5xgv9eyuv6g49mcwqks24gd3dh39w8220l0kl60qxt60rnt60lsc8fqv0"),
		ReadStores: []blob_store_id.Id{mustId(t, "archive@blake2b256-qqqsyqcyq5rqwzqfpg9scrgwpugpzysnzs23v9ccrydpk8qarc0s6vk400")},
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
		MirrorStores: []blob_store_id.Id{mustId(t, "default@blake2b256-c5xgv9eyuv6g49mcwqks24gd3dh39w8220l0kl60qxt60rnt60lsc8fqv0")},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate rejected a digest-bearing ref: %v", err)
	}
}

func TestTomlMultiV0_SatisfiesConfigMulti(t *testing.T) {
	var _ ConfigMulti = TomlMultiV0{}
}
