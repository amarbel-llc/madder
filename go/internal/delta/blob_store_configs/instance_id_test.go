//go:build test

package blob_store_configs

import (
	"bytes"
	"strings"
	"testing"

	"code.linenisgreat.com/madder/go/internal/0/ids"
	"code.linenisgreat.com/madder/go/internal/charlie/markl_registrations"
)

// newFreshLocalConfig builds a current-version local config identical to
// Default()'s, with NO instance identity yet — the state a store's config is
// in the moment before it is first written.
func newFreshLocalConfig() *TypedConfig {
	return &TypedConfig{
		Type: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigVCurrent).TypeStruct,
		Blob: &TomlV4{
			HashBuckets:     DefaultHashBuckets,
			HashTypeId:      HashTypeDefault,
			CompressionType: "zstd",
		},
	}
}

// TestEncodeWithDigestMintsInstanceId pins FDR-0010's mint funnel:
// EncodeWithDigest — the sole sanctioned config write path — stamps a fresh
// uuidv7 instance identity into a mintable config that has none. This also
// guards a future VCurrent bump that forgets the instance-id field: the
// mintable assertion would then find no identity and the test fails.
func TestEncodeWithDigestMintsInstanceId(t *testing.T) {
	cfg := newFreshLocalConfig()

	// Precondition: a freshly-built config has no identity.
	if before := cfg.Blob.(ConfigInstanceId).GetInstanceId(); len(
		before.GetBytes(),
	) != 0 {
		t.Fatalf(
			"fresh config already carries an instance-id: %q",
			before.StringWithFormat(),
		)
	}

	var buf bytes.Buffer
	if _, err := EncodeWithDigest(cfg, &buf); err != nil {
		t.Fatal(err)
	}

	got := cfg.Blob.(ConfigInstanceId).GetInstanceId()

	wantPrefix := markl_registrations.FormatIdUuidv7 + "-"
	if s := got.StringWithFormat(); !strings.HasPrefix(s, wantPrefix) {
		t.Fatalf("minted instance-id %q lacks %q prefix", s, wantPrefix)
	}
	if n := len(got.GetBytes()); n != 16 {
		t.Fatalf("minted instance-id has %d payload bytes, want 16", n)
	}
}

// TestInstanceIdMakesConfigDigestUnique is FDR-0010's core property: a store's
// uuidv7 identity lives in the config BODY, so the FDR-0008 config digest —
// which covers the body bytes — inherits its entropy. Two stores with
// byte-for-byte identical configuration therefore encode DIFFERENTLY, so
// `name@digest` identifies a store instance and not merely a configuration.
// A re-encode of an already-minted config is byte-identical: the funnel mints
// once, never on re-encode.
func TestInstanceIdMakesConfigDigestUnique(t *testing.T) {
	storeA := newFreshLocalConfig()
	storeB := newFreshLocalConfig()

	var bufA, bufB bytes.Buffer
	if _, err := EncodeWithDigest(storeA, &bufA); err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeWithDigest(storeB, &bufB); err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(bufA.Bytes(), bufB.Bytes()) {
		t.Fatal(
			"two identically-configured stores encoded identically: " +
				"instance identity did not make the config digest unique",
		)
	}

	// Determinism / no re-mint: re-encoding storeA (now minted) must be
	// byte-identical, proving the funnel does not re-mint an existing identity.
	var bufA2 bytes.Buffer
	if _, err := EncodeWithDigest(storeA, &bufA2); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bufA.Bytes(), bufA2.Bytes()) {
		t.Fatal("re-encoding an already-minted config must be deterministic (no re-mint)")
	}
}

// TestEncodeWithDigestMintsEveryCurrentType is the horizontal-sweep guard
// (FDR-0010): every current-version store config — not just the local one —
// carries an instance-id field and is minted by the EncodeWithDigest funnel.
// If a future bump adds a new store type without the mint interface, or a
// VCurrent-style alias is not advanced, the corresponding row fails.
func TestEncodeWithDigestMintsEveryCurrentType(t *testing.T) {
	cases := []struct {
		name   string
		typeId string
		blob   Config
	}{
		{"local", ids.TypeTomlBlobStoreConfigV4, &TomlV4{
			HashBuckets: DefaultHashBuckets, HashTypeId: HashTypeDefault,
			CompressionType: "zstd",
		}},
		{"sftp", ids.TypeTomlBlobStoreConfigSftpExplicitV1, &TomlSFTPV1{}},
		{"sftp-ssh", ids.TypeTomlBlobStoreConfigSftpViaSSHConfigV1, &TomlSFTPViaSSHConfigV1{}},
		{"webdav", ids.TypeTomlBlobStoreConfigWebdavV1, &TomlWebDAVV1{}},
		{"s3", ids.TypeTomlBlobStoreConfigS3V1, &TomlS3V1{Bucket: "b"}},
		{"pointer", ids.TypeTomlBlobStoreConfigPointerV2, &TomlPointerV2{}},
		{"multi", ids.TypeTomlBlobStoreConfigMultiV1, &TomlMultiV1{Mode: "mirror"}},
		{"inventory", ids.TypeTomlBlobStoreConfigInventoryArchiveV3, &TomlInventoryArchiveV3{}},
	}

	wantPrefix := markl_registrations.FormatIdUuidv7 + "-"

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &TypedConfig{
				Type: ids.GetOrPanic(tc.typeId).TypeStruct,
				Blob: tc.blob,
			}

			mintable, ok := cfg.Blob.(ConfigInstanceIdMintable)
			if !ok {
				t.Fatalf("current %s config %T is not instance-id mintable", tc.name, cfg.Blob)
			}

			var buf bytes.Buffer
			if _, err := EncodeWithDigest(cfg, &buf); err != nil {
				t.Fatal(err)
			}

			if got := mintable.GetInstanceId().StringWithFormat(); !strings.HasPrefix(
				got,
				wantPrefix,
			) {
				t.Fatalf("%s: instance-id %q lacks %q prefix", tc.name, got, wantPrefix)
			}
		})
	}
}
