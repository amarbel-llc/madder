//go:build test

package blob_store_configs

import (
	"bytes"
	"strings"
	"testing"

	"code.linenisgreat.com/madder/go/internal/charlie/store_key"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
)

// A sidecar built from a real sealed store key survives the hyphence +
// TOML round trip byte for byte, including the multi-line sealed
// document, so what is read back still opens.
func TestStoreKeySidecarRoundTrip(t *testing.T) {
	recipient := storePublicKey(t)

	_, sealed, err := store_key.Mint([]markl.Id{recipient})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	digest, err := store_key.RecipientSetDigest([]markl.Id{recipient})
	if err != nil {
		t.Fatalf("RecipientSetDigest: %v", err)
	}

	encoded, err := EncodeStoreKey(TomlStoreKeyV1{
		Recipients: StoreKeyRecipients{
			Source: "/home/someone/.password-store/piggy-ids",
			Digest: digest,
		},
		Sealed: StoreKeySealed{Document: string(sealed)},
	})
	if err != nil {
		t.Fatalf("EncodeStoreKey: %v", err)
	}

	if !bytes.HasPrefix(encoded, []byte("---\n! toml-blob_store_key-v1\n---\n\n")) {
		t.Fatalf("unexpected header:\n%s", encoded)
	}

	decoded, err := DecodeStoreKey(encoded)
	if err != nil {
		t.Fatalf("DecodeStoreKey: %v", err)
	}

	if decoded.Sealed.Document != string(sealed) {
		t.Errorf(
			"sealed document changed in the round trip:\n got %q\nwant %q",
			decoded.Sealed.Document,
			sealed,
		)
	}

	if decoded.Recipients.Source != "/home/someone/.password-store/piggy-ids" {
		t.Errorf("source = %q", decoded.Recipients.Source)
	}

	if decoded.Recipients.Digest.String() != digest.String() {
		t.Errorf("digest = %s, want %s", decoded.Recipients.Digest, digest)
	}

	// The recipients are readable from the sealed document without
	// opening it, and match what the digest was taken over.
	recipients, err := store_key.SealedRecipients([]byte(decoded.Sealed.Document))
	if err != nil {
		t.Fatalf("SealedRecipients: %v", err)
	}

	again, err := store_key.RecipientSetDigest(recipients)
	if err != nil {
		t.Fatalf("RecipientSetDigest: %v", err)
	}

	if again.String() != digest.String() {
		t.Errorf("digest of the sealed recipients %s != recorded %s", again, digest)
	}
}

// Only exactly a store-key document is accepted: a config, a truncated
// file, or a sidecar with no sealed document is refused.
func TestStoreKeySidecarRejectsOtherDocuments(t *testing.T) {
	config := encodeConfig(t, newSealedKeyConfig(t))

	for name, raw := range map[string]string{
		"a blob_store-config": config,
		"empty":               "",
		"no body":             "---\n! toml-blob_store_key-v1\n---\n\n",
		"other type": strings.Replace(
			"---\n! toml-blob_store_key-v1\n---\n\n[sealed]\ndocument = \"x\"\n",
			"key-v1",
			"key-v2",
			1,
		),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeStoreKey([]byte(raw)); err == nil {
				t.Fatalf("decoded %q as a store key sidecar", raw)
			}
		})
	}

	if _, err := EncodeStoreKey(TomlStoreKeyV1{}); err == nil {
		t.Fatalf("encoded a sidecar with no sealed document")
	}
}
