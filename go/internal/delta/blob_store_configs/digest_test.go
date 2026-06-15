//go:build test

package blob_store_configs

import (
	"bytes"
	"errors"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/ids"
	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	_ "github.com/amarbel-llc/madder/go/internal/charlie/markl_registrations"
)

// TestEncodeWithDigestRoundTrip: encoding a config through
// EncodeWithDigest produces output whose @ line carries the blake2b256
// digest of the body bytes; the same digest survives a Coder.DecodeFrom
// round-trip.
func TestEncodeWithDigestRoundTrip(t *testing.T) {
	typedConfig := defaultTypedConfigForTest(t)

	var buf bytes.Buffer
	if _, err := EncodeWithDigest(typedConfig, &buf); err != nil {
		t.Fatalf("EncodeWithDigest: %v", err)
	}

	if typedConfig.BlobDigest.GetPurposeId() != markl.PurposeBlobStoreConfigDigestV1 {
		t.Fatalf("post-encode purpose: got %q, want %q",
			typedConfig.BlobDigest.GetPurposeId(),
			markl.PurposeBlobStoreConfigDigestV1)
	}

	decoded := &TypedConfig{}
	if _, err := Coder.DecodeFrom(decoded, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("Coder.DecodeFrom: %v", err)
	}

	if decoded.BlobDigest.IsNull() {
		t.Fatal("expected non-null BlobDigest after round-trip")
	}

	if got := decoded.BlobDigest.GetMarklFormat().GetMarklFormatId(); got != markl.FormatIdHashBlake2b256 {
		t.Fatalf("expected blake2b256 digest, got %v", got)
	}
}

// TestEncodeWithDigestDetectsTamper: mutate one byte of an encoded
// config; DecodeAndVerify surfaces the mismatch as markl.ErrNotEqual
// carrying both Expected and Actual digests.
func TestEncodeWithDigestDetectsTamper(t *testing.T) {
	typedConfig := defaultTypedConfigForTest(t)

	var buf bytes.Buffer
	if _, err := EncodeWithDigest(typedConfig, &buf); err != nil {
		t.Fatalf("EncodeWithDigest: %v", err)
	}

	bs := buf.Bytes()
	boundary := []byte("---\n\n")
	idx := bytes.LastIndex(bs, boundary)
	if idx < 0 {
		t.Fatalf("could not locate body start in encoded output:\n%s", bs)
	}
	bodyStart := idx + len(boundary)
	if bodyStart >= len(bs) {
		t.Fatalf("body is empty in encoded output:\n%s", bs)
	}
	bs[bodyStart] ^= 0x01

	decoded := &TypedConfig{}
	_, err := DecodeAndVerify(decoded, bytes.NewReader(bs))
	if err == nil {
		t.Fatal("expected mismatch error, got nil")
	}
	var notEqual markl.ErrNotEqual
	if !errors.As(err, &notEqual) {
		t.Fatalf("expected markl.ErrNotEqual, got %T: %v", err, err)
	}
	if notEqual.Expected.IsNull() || notEqual.Actual.IsNull() {
		t.Fatal("expected both Expected and Actual to be populated")
	}
}

// TestDecodeAndVerifyAcceptsLegacy: a config with no @ line
// (pre-FDR-0008) is trusted silently.
func TestDecodeAndVerifyAcceptsLegacy(t *testing.T) {
	typedConfig := defaultTypedConfigForTest(t)

	var buf bytes.Buffer
	if _, err := Coder.EncodeTo(typedConfig, &buf); err != nil {
		t.Fatalf("Coder.EncodeTo: %v", err)
	}

	decoded := &TypedConfig{}
	if _, err := DecodeAndVerify(decoded, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("DecodeAndVerify on legacy config: %v", err)
	}
	if !decoded.BlobDigest.IsNull() {
		t.Fatal("legacy config should not have BlobDigest populated")
	}
}

// TestDecodeAndVerifyRoundTrip: encode via EncodeWithDigest, decode via
// DecodeAndVerify, no error, BlobDigest populated after round-trip.
func TestDecodeAndVerifyRoundTrip(t *testing.T) {
	typedConfig := defaultTypedConfigForTest(t)

	var buf bytes.Buffer
	if _, err := EncodeWithDigest(typedConfig, &buf); err != nil {
		t.Fatalf("EncodeWithDigest: %v", err)
	}

	decoded := &TypedConfig{}
	if _, err := DecodeAndVerify(decoded, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("DecodeAndVerify: %v", err)
	}
	if decoded.BlobDigest.IsNull() {
		t.Fatal("BlobDigest should be populated after round-trip")
	}
}

func mustBlobStoreId(t *testing.T, s string) scoped_id.Id {
	t.Helper()
	var id scoped_id.Id
	if err := id.Set(s); err != nil {
		t.Fatalf("Set(%q): %v", s, err)
	}
	return id
}

func TestEncodeWithDigest_MultiRoundTrip(t *testing.T) {
	readFill := true
	typedConfig := &TypedConfig{
		Type: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigMultiV0).TypeStruct,
		Blob: &TomlMultiV0{
			Mode:       "write_through",
			WriteStore: mustBlobStoreId(t, "default@blake2b256-c5xgv9eyuv6g49mcwqks24gd3dh39w8220l0kl60qxt60rnt60lsc8fqv0"),
			ReadStores: []scoped_id.Id{mustBlobStoreId(t, "archive@blake2b256-qqqsyqcyq5rqwzqfpg9scrgwpugpzysnzs23v9ccrydpk8qarc0s6vk400")},
			ReadFill:   &readFill,
		},
	}

	var buf bytes.Buffer
	if _, err := EncodeWithDigest(typedConfig, &buf); err != nil {
		t.Fatalf("EncodeWithDigest: %v", err)
	}

	decoded := &TypedConfig{}
	if _, err := DecodeAndVerify(decoded, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("DecodeAndVerify: %v", err)
	}

	multi, ok := decoded.Blob.(ConfigMulti)
	if !ok {
		t.Fatalf("decoded blob is %T, want ConfigMulti", decoded.Blob)
	}
	if multi.GetMode() != "write_through" {
		t.Errorf("GetMode = %q", multi.GetMode())
	}
	if !multi.GetReadFill() {
		t.Error("GetReadFill = false, want true")
	}
	if decoded.BlobDigest.IsNull() {
		t.Error("BlobDigest null after round-trip")
	}
}

// TestDecodeAndVerify_RejectsBareMultiRef: a multi config whose
// reference lacks a digest must fail at decode. TomlMultiV0.Validate()
// is wired by tommy into both the generated Encode AND Decode, so a bare
// ref cannot even be produced through the Coder — the realistic threat
// is a hand-edited config file on disk. This test crafts those on-disk
// bytes by stripping the digest suffix off a validly-encoded reference,
// then asserts DecodeAndVerify rejects them. Coder.DecodeFrom (which
// runs the validating DecodeTomlMultiV0) fires before the digest
// comparison, so the bare-ref check is what trips, not a digest
// mismatch.
func TestDecodeAndVerify_RejectsBareMultiRef(t *testing.T) {
	typedConfig := &TypedConfig{
		Type: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigMultiV0).TypeStruct,
		Blob: &TomlMultiV0{
			Mode:         "mirror",
			MirrorStores: []scoped_id.Id{mustBlobStoreId(t, "default@blake2b256-c5xgv9eyuv6g49mcwqks24gd3dh39w8220l0kl60qxt60rnt60lsc8fqv0")},
		},
	}

	var buf bytes.Buffer
	if _, err := EncodeWithDigest(typedConfig, &buf); err != nil {
		t.Fatalf("EncodeWithDigest: %v", err)
	}

	// Strip the digest suffix to forge a hand-edited bare reference.
	bare := bytes.Replace(buf.Bytes(),
		[]byte("default@blake2b256-c5xgv9eyuv6g49mcwqks24gd3dh39w8220l0kl60qxt60rnt60lsc8fqv0"),
		[]byte("default"), 1)
	if bytes.Equal(bare, buf.Bytes()) {
		t.Fatal("digest-bearing reference not found in encoded output")
	}

	decoded := &TypedConfig{}
	if _, err := DecodeAndVerify(decoded, bytes.NewReader(bare)); err == nil {
		t.Fatal("expected decode to reject bare multi reference, got nil")
	}
}

func defaultTypedConfigForTest(t *testing.T) *TypedConfig {
	t.Helper()
	return &TypedConfig{
		Type: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigVCurrent).TypeStruct,
		Blob: &DefaultType{
			HashBuckets:     DefaultHashBuckets,
			HashTypeId:      HashTypeDefault,
			CompressionType: "zstd",
		},
	}
}
