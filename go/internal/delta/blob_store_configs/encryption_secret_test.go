//go:build test

package blob_store_configs

import (
	"testing"

	_ "code.linenisgreat.com/madder/go/internal/charlie/markl_registrations"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
)

// The key `-encryption generate` mints is a secret and is named by purpose
// and format, never by its bytes.
func TestFirstSecretKeyNamesAGeneratedKeyWithoutItsBytes(t *testing.T) {
	var secret markl.Id
	if err := secret.GeneratePrivateKey(
		nil,
		markl.FormatIdAgeX25519Sec,
		markl.PurposeMadderPrivateKeyV1,
	); err != nil {
		t.Fatalf("GeneratePrivateKey: %v", err)
	}

	if !IsSecretKeyId(secret) {
		t.Fatalf("IsSecretKeyId(%s) = false", secret.GetMarklFormat().GetMarklFormatId())
	}

	description, found := FirstSecretKey([]markl.Id{storePublicKey(t), secret})
	if !found {
		t.Fatal("FirstSecretKey found nothing")
	}

	want := markl.PurposeMadderPrivateKeyV1 + "@" + markl.FormatIdAgeX25519Sec
	if description != want {
		t.Errorf("description = %q, want %q", description, want)
	}

	if description == secret.String() || len(description) >= len(secret.String()) {
		t.Errorf("description %q carries the key's bytes", description)
	}
}

// Public recipients and the empty list are what a remote config may hold.
func TestFirstSecretKeyAcceptsPublicRecipientsAndNothing(t *testing.T) {
	if IsSecretKeyId(storePublicKey(t)) {
		t.Error("an age_x25519_pub id is reported as a secret")
	}

	if IsSecretKeyId(markl.Id{}) {
		t.Error("the null id is reported as a secret")
	}

	for name, keys := range map[string][]markl.Id{
		"none":   nil,
		"empty":  {},
		"public": {storePublicKey(t)},
	} {
		if description, found := FirstSecretKey(keys); found {
			t.Errorf("%s: FirstSecretKey = %q, want none", name, description)
		}
	}
}
