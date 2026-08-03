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
