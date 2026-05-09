//go:build test

package blob_stores

import (
	"testing"

	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
)

// TestConfigFromDiscoveredConfig_PropagatesSingleHash pins #149: the
// helper that translates a DiscoveredConfig into a DefaultType for
// WriteRemoteConfig must propagate the discovered MultiHash flag as
// the inverse of TomlV3.SingleHash. Without this, init-sftp-explicit
// -discover writes a multi-hash=true config for legacy single-hash
// stores and downstream reads (fsck, info-repo, sync) walk the wrong
// shape and fail.
func TestConfigFromDiscoveredConfig_PropagatesSingleHash(t *testing.T) {
	cases := []struct {
		name       string
		multiHash  bool
		wantSingle bool
	}{
		{"single-hash legacy layout", false, true},
		{"multi-hash modern layout", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := DiscoveredConfig{
				HashTypeId: "sha256",
				MultiHash:  tc.multiHash,
				Buckets:    []int{2},
			}
			got := configFromDiscoveredConfig(d)
			if got.SingleHash != tc.wantSingle {
				t.Errorf("SingleHash = %v, want %v",
					got.SingleHash, tc.wantSingle)
			}
		})
	}
}

// TestConfigFromDiscoveredConfig_PreservesOtherFields makes sure the
// extraction didn't drop the non-MultiHash fields the original
// inline construction set: hash type, buckets, compression default,
// encryption.
func TestConfigFromDiscoveredConfig_PreservesOtherFields(t *testing.T) {
	d := DiscoveredConfig{
		HashTypeId: "sha256",
		MultiHash:  true,
		Buckets:    []int{2, 2},
		Encryption: []markl.Id{},
	}
	got := configFromDiscoveredConfig(d)
	if string(got.HashTypeId) != "sha256" {
		t.Errorf("HashTypeId = %q, want sha256", got.HashTypeId)
	}
	if len(got.HashBuckets) != 2 || got.HashBuckets[0] != 2 || got.HashBuckets[1] != 2 {
		t.Errorf("HashBuckets = %v, want [2 2]", got.HashBuckets)
	}
	if got.CompressionType != "zstd" {
		t.Errorf("CompressionType = %q, want zstd", got.CompressionType)
	}
}
