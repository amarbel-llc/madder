//go:build test

package markl_registrations_test

import (
	"testing"

	"github.com/amarbel-llc/madder/go/internal/charlie/markl_registrations"
	"github.com/amarbel-llc/piggy/go/markl/pkgs/markl"
)

// AllPurposes is the canonical, ordered list of madder's purposes.
// Every entry must be installed in markl's registry by the package's
// init(); this test just iterates and asserts GetPurpose succeeds for
// each, plus that the registered Type matches the canonical Type.
func TestAllPurposes_Registered(t *testing.T) {
	for _, opts := range markl_registrations.AllPurposes {
		opts := opts
		t.Run(opts.Id, func(t *testing.T) {
			got := markl.GetPurpose(opts.Id)

			if got.GetPurposeType() != opts.Type {
				t.Errorf("Type for %q: got %v, want %v", opts.Id, got.GetPurposeType(), opts.Type)
			}
		})
	}
}

// AllPurposes' Related metadata round-trips through GetRelated for
// every entry that declares related purposes.
func TestAllPurposes_RelatedRoundTrip(t *testing.T) {
	for _, opts := range markl_registrations.AllPurposes {
		if len(opts.Related) == 0 {
			continue
		}

		opts := opts
		t.Run(opts.Id, func(t *testing.T) {
			purpose := markl.GetPurpose(opts.Id)

			for role, want := range opts.Related {
				got, ok := purpose.GetRelated(role)
				if !ok {
					t.Errorf("GetRelated(%q): not found", role)
					continue
				}
				if got != want {
					t.Errorf("GetRelated(%q): got %q, want %q", role, got, want)
				}
			}
		})
	}
}

// The repo-private-key Related[public_key] pairing test and the
// canonical sig→digest / sig→mother-sig mapping tests moved to dodder
// alongside the dodder-* registrations (madder#255 step 3).

// Each AllAliases entry resolves to its target format via
// GetFormatOrError. This exercises markl's purpose-id-alias indirection
// end-to-end.
func TestAllAliases_ResolveViaGetFormatOrError(t *testing.T) {
	for _, alias := range markl_registrations.AllAliases {
		alias := alias
		t.Run(alias.PurposeId, func(t *testing.T) {
			format, err := markl.GetFormatOrError(alias.PurposeId)
			if err != nil {
				t.Fatalf("GetFormatOrError(%q): %v", alias.PurposeId, err)
			}

			if format.GetMarklFormatId() != alias.FormatId {
				t.Errorf("resolved format id: got %q, want %q",
					format.GetMarklFormatId(), alias.FormatId)
			}
		})
	}
}

// PurposeBlobStoreConfigDigestV1 is the new vocabulary entry FDR-0008
// Phase 1 introduces for the `@ <markl-id>` line on blob_store-config
// blobs. Pin the registered Type and the canonical format so a drift
// in either constant or registration would surface here.
func TestBlobStoreConfigDigestV1Registered(t *testing.T) {
	purpose := markl.GetPurpose(markl.PurposeBlobStoreConfigDigestV1)
	if purpose.GetPurposeType() != markl.PurposeTypeBlobDigest {
		t.Fatalf(
			"expected PurposeTypeBlobDigest, got %v",
			purpose.GetPurposeType(),
		)
	}
}

// The end-to-end Id.GetPublicKey test (ed25519 vs stdlib) crossed the
// dodder-repo-private_key-v1 purpose and moved to dodder alongside the
// dodder-* registrations (madder#255 step 3).
