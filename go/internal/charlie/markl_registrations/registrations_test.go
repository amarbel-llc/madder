//go:build test

package markl_registrations_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/charlie/markl_registrations"
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

// markl.GetDigestTypeForSigType is a thin wrapper over the registered
// Related["digest"] entry. Verify it returns the canonical mapping for
// each sig that declares one.
func TestGetDigestTypeForSigType_Canonical(t *testing.T) {
	cases := []struct {
		sigId    string
		digestId string
	}{
		{markl.PurposeObjectSigV1, markl.PurposeObjectDigestV1},
		{markl.PurposeObjectSigV2, markl.PurposeObjectDigestV2},
	}

	for _, c := range cases {
		c := c
		t.Run(c.sigId, func(t *testing.T) {
			if got := markl.GetDigestTypeForSigType(c.sigId); got != c.digestId {
				t.Errorf("got %q, want %q", got, c.digestId)
			}
		})
	}
}

// Same shape as GetDigestTypeForSigType but for the mother_sig role.
func TestGetMotherSigTypeForSigType_Canonical(t *testing.T) {
	cases := []struct {
		sigId       string
		motherSigId string
	}{
		{markl.PurposeObjectSigV1, markl.PurposeObjectMotherSigV1},
		{markl.PurposeObjectSigV2, markl.PurposeObjectMotherSigV2},
	}

	for _, c := range cases {
		c := c
		t.Run(c.sigId, func(t *testing.T) {
			if got := markl.GetMotherSigTypeForSigType(c.sigId); got != c.motherSigId {
				t.Errorf("got %q, want %q", got, c.motherSigId)
			}
		})
	}
}

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

// End-to-end: Id.GetPublicKey delegates to the registered FormatSec via
// PurposeRepoPrivateKeyV1, stamps the result with PurposeRepoPubKeyV1,
// and the result bytes match Go's stdlib ed25519. Originally lived in
// internal/bravo/markl; relocated here because markl's tests don't
// register the dodder vocabulary that this path crosses.
func TestIdGetPublicKey_Ed25519_MatchesStdlib(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	var secId markl.Id
	if err := secId.SetPurposeId(markl.PurposeRepoPrivateKeyV1); err != nil {
		t.Fatal(err)
	}
	if err := secId.SetMarklId(markl.FormatIdEd25519Sec, priv); err != nil {
		t.Fatal(err)
	}

	pubId, err := secId.GetPublicKey(markl.PurposeRepoPrivateKeyV1)
	if err != nil {
		t.Fatalf("Id.GetPublicKey: %v", err)
	}

	want := priv.Public().(ed25519.PublicKey)
	if !bytes.Equal(pubId.GetBytes(), want) {
		t.Errorf("pubkey mismatch:\n got  %x\n want %x", pubId.GetBytes(), want)
	}
}
