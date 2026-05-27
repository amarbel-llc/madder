//go:build test

package blob_store_configs

import (
	"bytes"
	"errors"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/ids"
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
