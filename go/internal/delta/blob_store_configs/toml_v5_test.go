//go:build test

package blob_store_configs

import (
	"bytes"
	"strings"
	"testing"

	"code.linenisgreat.com/madder/go/internal/0/ids"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
)

// storePublicKey builds an age_x25519_pub id like the one store_key.Mint
// returns. The bytes need not be a real key for config coding.
func storePublicKey(t *testing.T) markl.Id {
	t.Helper()

	var id markl.Id

	if err := id.SetPurposeId(markl.PurposePiggyRecipientV1); err != nil {
		t.Fatalf("SetPurposeId: %v", err)
	}

	bites := make([]byte, 32)
	for i := range bites {
		bites[i] = byte(i + 1)
	}

	if err := id.SetMarklId(markl.FormatIdAgeX25519Pub, bites); err != nil {
		t.Fatalf("SetMarklId: %v", err)
	}

	return id
}

func newSealedKeyConfig(t *testing.T) *TypedConfig {
	t.Helper()

	return &TypedConfig{
		Type: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigV5).TypeStruct,
		Blob: &TomlV5{
			HashBuckets:     DefaultHashBuckets,
			HashTypeId:      HashTypeDefault,
			CompressionType: "zstd",
			Encryption:      []markl.Id{storePublicKey(t)},
			KeyCustody:      KeyCustody{Holder: KeyHolderProcess},
		},
	}
}

func encodeConfig(t *testing.T, cfg *TypedConfig) string {
	t.Helper()

	var buf bytes.Buffer

	if _, err := EncodeWithDigest(cfg, &buf); err != nil {
		t.Fatalf("EncodeWithDigest: %v", err)
	}

	return buf.String()
}

// A sealed-key config round-trips through the digest-pinned coder, keeps
// its public key and holder, and never carries secret key material.
func TestTomlV5RoundTrip(t *testing.T) {
	encoded := encodeConfig(t, newSealedKeyConfig(t))

	for _, want := range []string{
		"! toml-blob_store_config-v5",
		"[key-custody]",
		`holder = "process"`,
		markl.FormatIdAgeX25519Pub,
	} {
		if !strings.Contains(encoded, want) {
			t.Errorf("encoded config lacks %q:\n%s", want, encoded)
		}
	}

	if strings.Contains(encoded, markl.FormatIdAgeX25519Sec) {
		t.Fatalf("encoded sealed-key config contains a secret key id:\n%s", encoded)
	}

	var decoded TypedConfig

	if _, err := DecodeAndVerify(&decoded, strings.NewReader(encoded)); err != nil {
		t.Fatalf("DecodeAndVerify: %v", err)
	}

	sealed, ok := decoded.Blob.(ConfigSealedKey)
	if !ok {
		t.Fatalf("decoded %T does not implement ConfigSealedKey", decoded.Blob)
	}

	if got := sealed.GetKeyCustodyHolder(); got != KeyHolderProcess {
		t.Errorf("holder = %q, want %q", got, KeyHolderProcess)
	}

	want := storePublicKey(t)
	if got := sealed.GetStorePublicKey(); got.String() != want.String() {
		t.Errorf("public key = %s, want %s", got, want)
	}

	if got := TypeStructForConfig(decoded.Blob); got.String() !=
		ids.GetOrPanic(ids.TypeTomlBlobStoreConfigV5).TypeStruct.String() {
		t.Errorf("TypeStructForConfig = %s", got)
	}
}

// A V5 config that this madder cannot safely use must fail to decode, not
// open as something else: an unknown holder, a missing key, a second key,
// or a key that is not an age public key.
func TestTomlV5RejectsUnusableConfigs(t *testing.T) {
	var secret markl.Id
	if err := secret.GeneratePrivateKey(
		nil,
		markl.FormatIdAgeX25519Sec,
		markl.PurposeMadderPrivateKeyV1,
	); err != nil {
		t.Fatalf("GeneratePrivateKey: %v", err)
	}

	for name, mutate := range map[string]func(*TomlV5){
		"unknown holder": func(cfg *TomlV5) { cfg.KeyCustody.Holder = "agent" },
		"no holder":      func(cfg *TomlV5) { cfg.KeyCustody.Holder = "" },
		"no key":         func(cfg *TomlV5) { cfg.Encryption = nil },
		"two keys": func(cfg *TomlV5) {
			cfg.Encryption = append(cfg.Encryption, cfg.Encryption[0])
		},
		"secret key": func(cfg *TomlV5) { cfg.Encryption = []markl.Id{secret} },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := newSealedKeyConfig(t)
			mutate(cfg.Blob.(*TomlV5))

			// Either side may refuse it; what must not happen is a
			// write followed by a successful read.
			var buf bytes.Buffer

			if _, err := EncodeWithDigest(cfg, &buf); err != nil {
				return
			}

			var decoded TypedConfig

			if _, err := DecodeAndVerify(
				&decoded,
				strings.NewReader(buf.String()),
			); err == nil {
				t.Fatalf("wrote and read back an unusable config:\n%s", buf.String())
			}
		})
	}
}

// The default config version stays V4: an ordinary new store must remain
// readable by a madder that predates the sealed-key type.
func TestDefaultConfigVersionIsNotV5(t *testing.T) {
	if got := Default().Type.String(); strings.Contains(got, "-v5") {
		t.Fatalf("default config type is %s; V5 must be opt-in", got)
	}
}
