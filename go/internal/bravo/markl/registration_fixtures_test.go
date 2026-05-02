//go:build test

package markl

// Test-only purpose registrations used by markl's own black-box-style
// tests. Each id is prefixed with "test-" so they cannot collide with any
// production purpose (madder's, dodder's, or any future consumer's) — the
// markl package's tests do not depend on production registrations
// existing.

const (
	testPurposeSSHPriv = "test-ssh-ed25519-private_key"
	testPurposeSig     = "test-object-sig"
	testPurposePub     = "test-public_key"
)

func init() {
	RegisterPurpose(RegisterPurposeOpts{
		Id:   testPurposeSSHPriv,
		Type: PurposeTypePrivateKey,
		FormatIds: []string{
			FormatIdEd25519Sec,
			FormatIdEd25519SSH,
			FormatIdEcdsaP256SSH,
		},
	})

	RegisterPurpose(RegisterPurposeOpts{
		Id:        testPurposeSig,
		Type:      PurposeTypeObjectSig,
		FormatIds: []string{FormatIdEd25519Sig, FormatIdEcdsaP256Sig},
	})

	RegisterPurpose(RegisterPurposeOpts{
		Id:        testPurposePub,
		Type:      PurposeTypePubKey,
		FormatIds: []string{FormatIdEd25519Pub, FormatIdEcdsaP256Pub},
	})

	// id_crypto_sec.go:108 (Id.GetPublicKey) hardcodes PurposeRepoPubKeyV1
	// as the purpose stamped on returned public-key Ids. Until that
	// hardcode is parameterised away, markl's tests have to keep this
	// dodder vocabulary registered or any test path that crosses
	// Id.GetPublicKey panics. Tracked by the same #106 cleanup line that
	// tracks the Get*TypeForSigType helpers.
	RegisterPurpose(RegisterPurposeOpts{
		Id:        PurposeRepoPubKeyV1,
		Type:      PurposeTypeRepoPubKey,
		FormatIds: []string{FormatIdEd25519Pub, FormatIdEcdsaP256Pub},
	})
}
