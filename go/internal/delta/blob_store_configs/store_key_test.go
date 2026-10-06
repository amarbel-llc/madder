//go:build test

package blob_store_configs

import (
	"bytes"
	"strings"
	"testing"

	charlie_bsc "code.linenisgreat.com/madder/go/internal/charlie/blob_store_configs"
	"code.linenisgreat.com/madder/go/internal/charlie/store_key"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/piggy/go/pkgs/pigpen"
)

// A sidecar built from a real sealed store key survives the hyphence +
// TOML round trip byte for byte, including both embedded multi-line
// documents, so what is read back still opens and still parses.
func TestStoreKeySidecarRoundTrip(t *testing.T) {
	recipient := storePublicKey(t)
	recipients := []markl.Id{recipient}

	_, sealed, err := store_key.Mint(recipients)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	source := store_key.Source{
		Kind:    store_key.SourceKindPath,
		Locator: "/home/someone/.password-store/piggy-ids",
	}

	storeKey, err := MakeStoreKey(source, recipients, sealed)
	if err != nil {
		t.Fatalf("MakeStoreKey: %v", err)
	}

	encoded, err := EncodeStoreKey(storeKey)
	if err != nil {
		t.Fatalf("EncodeStoreKey: %v", err)
	}

	if !bytes.HasPrefix(encoded, []byte("---\n! toml-blob_store_key-v2\n---\n\n")) {
		t.Fatalf("unexpected header:\n%s", encoded)
	}

	decoded, err := DecodeStoreKey(encoded)
	if err != nil {
		t.Fatalf("DecodeStoreKey: %v", err)
	}

	if decoded != storeKey {
		t.Errorf("sidecar changed in the round trip:\n got %#v\nwant %#v", decoded, storeKey)
	}

	// A path source embeds a snapshot of who the pigpen named.
	snapshot, err := pigpen.ParseRecipients([]byte(decoded.Recipients.Pigpen))
	if err != nil {
		t.Fatalf("embedded pigpen does not parse: %v\n%s", err, decoded.Recipients.Pigpen)
	}

	if !pigpen.SameRecipientSet(snapshot, recipients) {
		t.Errorf("embedded snapshot %v, want %v", snapshot, recipients)
	}

	// The recipients are also readable from the sealed document without
	// opening it: that is what drift is checked against, so the sidecar
	// records no digest.
	sealedTo, err := store_key.SealedRecipients([]byte(decoded.Sealed.Document))
	if err != nil {
		t.Fatalf("SealedRecipients: %v", err)
	}

	if !pigpen.SameRecipientSet(sealedTo, recipients) {
		t.Errorf("sealed to %v, want %v", sealedTo, recipients)
	}

	// Both embedded documents are written as TOML multi-line strings, so
	// the sidecar reads as the documents it holds rather than as two long
	// escaped lines.
	for _, heredoc := range []string{"pigpen = \"\"\"", "document = \"\"\""} {
		if !bytes.Contains(encoded, []byte(heredoc)) {
			t.Errorf("sidecar has no %q heredoc:\n%s", heredoc, encoded)
		}
	}

	// A sealed document ends in ciphertext bytes; on disk the sidecar is
	// nonetheless plain printable text.
	for _, b := range encoded {
		if b != '\n' && (b < 0x20 || b > 0x7e) {
			t.Fatalf("sidecar contains the non-printable byte %#x:\n%q", b, encoded)
		}
	}

	if bytes.Contains(encoded, []byte("digest")) {
		t.Errorf("sidecar still records a digest:\n%s", encoded)
	}
}

// A papi source embeds the pointer to the published pigpen, quotes and
// all, and it survives the TOML round trip as a parseable pointer.
func TestStoreKeySidecarEmbedsPapiPointer(t *testing.T) {
	recipients := []markl.Id{storePublicKey(t)}

	_, sealed, err := store_key.Mint(recipients)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	source, err := store_key.MakeSource(store_key.SourceKindPapi, "example.com")
	if err != nil {
		t.Fatalf("MakeSource: %v", err)
	}

	storeKey, err := MakeStoreKey(source, recipients, sealed)
	if err != nil {
		t.Fatalf("MakeStoreKey: %v", err)
	}

	encoded, err := EncodeStoreKey(storeKey)
	if err != nil {
		t.Fatalf("EncodeStoreKey: %v", err)
	}

	decoded, err := DecodeStoreKey(encoded)
	if err != nil {
		t.Fatalf("DecodeStoreKey: %v", err)
	}

	if decoded.Recipients.SourceKind != "papi" || decoded.Recipients.Source != "example.com" {
		t.Errorf("recipients = %#v", decoded.Recipients)
	}

	pointer, err := pigpen.ParsePointer([]byte(decoded.Recipients.Pigpen))
	if err != nil {
		t.Fatalf("embedded pointer does not parse: %v\n%s", err, decoded.Recipients.Pigpen)
	}

	if pointer.Kind != "papi-http" || pointer.Locator != "example.com" {
		t.Errorf("pointer = %#v", pointer)
	}
}

// A v1 sidecar, as stores created before v2 have on disk, still decodes:
// its path becomes a path source and its digest is dropped.
func TestStoreKeySidecarReadsV1(t *testing.T) {
	_, sealed, err := store_key.Mint([]markl.Id{storePublicKey(t)})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	// Built with the v1 coder itself, so the bytes are what madder 0.4.6
	// put on disk: the sealed document as one escaped TOML string.
	doc, err := charlie_bsc.DecodeTomlStoreKeyV1(nil)
	if err != nil {
		t.Fatalf("DecodeTomlStoreKeyV1: %v", err)
	}

	digest, err := store_key.RecipientSetDigest([]markl.Id{storePublicKey(t)})
	if err != nil {
		t.Fatalf("RecipientSetDigest: %v", err)
	}

	*doc.Data() = charlie_bsc.TomlStoreKeyV1{
		Recipients: charlie_bsc.StoreKeyRecipients{
			Source: "/home/someone/piggy-ids",
			Digest: digest,
		},
		Sealed: charlie_bsc.StoreKeySealed{Document: string(sealed)},
	}

	body, err := doc.Encode()
	if err != nil {
		t.Fatalf("encoding a v1 sidecar: %v", err)
	}

	v1 := append(storeKeyHeader(TypeTomlStoreKeyV1), body...)

	decoded, err := DecodeStoreKey(v1)
	if err != nil {
		t.Fatalf("DecodeStoreKey(v1): %v", err)
	}

	if decoded.Sealed.Document != string(sealed) {
		t.Errorf("sealed document changed reading a v1 sidecar")
	}

	if decoded.Recipients.SourceKind != store_key.SourceKindPath ||
		decoded.Recipients.Source != "/home/someone/piggy-ids" {
		t.Errorf("recipients = %#v", decoded.Recipients)
	}

	if _, err = store_key.SealedRecipients([]byte(decoded.Sealed.Document)); err != nil {
		t.Errorf("sealed document from a v1 sidecar does not parse: %v", err)
	}
}

// Only exactly a store-key document is accepted: a config, a truncated
// file, or a sidecar with no sealed document is refused.
func TestStoreKeySidecarRejectsOtherDocuments(t *testing.T) {
	config := encodeConfig(t, newSealedKeyConfig(t))

	for name, raw := range map[string]string{
		"a blob_store-config": config,
		"empty":               "",
		"no body":             "---\n! toml-blob_store_key-v2\n---\n\n",
		"v1 with no body":     "---\n! toml-blob_store_key-v1\n---\n\n",
		"other type": strings.Replace(
			"---\n! toml-blob_store_key-v2\n---\n\n[sealed]\ndocument = \"x\"\n",
			"key-v2",
			"key-v9",
			1,
		),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeStoreKey([]byte(raw)); err == nil {
				t.Fatalf("decoded %q as a store key sidecar", raw)
			}
		})
	}

	if _, err := EncodeStoreKey(StoreKey{}); err == nil {
		t.Fatalf("encoded a sidecar with no sealed document")
	}
}
