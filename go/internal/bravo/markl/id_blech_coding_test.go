package markl

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// Regression for #157. Each fixture is an RFC 0002 vector from
// `testdata/0002-markl-id-format-vectors.json`, prefixed with the
// legacy dodder `@` to mimic the wire form that triggered the
// hex.Decode panic before the strip.
func TestSetMarklIdWithFormatBlech32_StripsLegacyAtPrefix(t *testing.T) {
	cases := []struct {
		name        string
		purposeId   string
		legacyValue string
		wantFormat  string
		wantPurpose string
		wantHex     string
	}{
		{
			name:        "sha256",
			legacyValue: "@sha256-qqqsyqcyq5rqwzqfpg9scrgwpugpzysnzs23v9ccrydpk8qarc0s7lcgm6",
			wantFormat:  FormatIdHashSha256,
			wantHex:     "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
		},
		{
			name:        "blake2b256",
			legacyValue: "@blake2b256-qqqsyqcyq5rqwzqfpg9scrgwpugpzysnzs23v9ccrydpk8qarc0s6vk400",
			wantFormat:  FormatIdHashBlake2b256,
			wantHex:     "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
		},
		{
			name:        "ed25519_pub",
			legacyValue: "@ed25519_pub-qqqsyqcyq5rqwzqfpg9scrgwpugpzysnzs23v9ccrydpk8qarc0srk9anc",
			wantFormat:  FormatIdEd25519Pub,
			wantHex:     "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
		},
		{
			name:        "ed25519_sig",
			legacyValue: "@ed25519_sig-qqqsyqcyq5rqwzqfpg9scrgwpugpzysnzs23v9ccrydpk8qarc0jqgfzyvjz2f389q5j52ev95hz7vp3xgengdfkxuurjw3m8s7nu0cy4lu83",
			wantFormat:  FormatIdEd25519Sig,
			wantHex:     "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f",
		},
		{
			// Caller-supplied purposeId pinning: the legacy token is
			// purposeless (no `purpose@` prefix in the wire form),
			// but the caller declares the purpose externally. Pins
			// that the purpose argument survives id.Set's
			// purposeless branch (which doesn't touch purposeId
			// when the input has no `@`).
			name:        "ed25519_pub_with_caller_purpose",
			purposeId:   testPurposePub,
			legacyValue: "@ed25519_pub-qqqsyqcyq5rqwzqfpg9scrgwpugpzysnzs23v9ccrydpk8qarc0srk9anc",
			wantFormat:  FormatIdEd25519Pub,
			wantPurpose: testPurposePub,
			wantHex:     "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantBytes, err := hex.DecodeString(tc.wantHex)
			if err != nil {
				t.Fatalf("decode wantHex: %v", err)
			}

			var id Id
			if err := SetMarklIdWithFormatBlech32(&id, tc.purposeId, tc.legacyValue); err != nil {
				t.Fatalf("legacy @-prefixed input %q: %v", tc.legacyValue, err)
			}

			format := id.GetMarklFormat()
			if format == nil {
				t.Fatal("format is nil after parse")
			}
			if got := format.GetMarklFormatId(); got != tc.wantFormat {
				t.Errorf("format: got %q, want %q", got, tc.wantFormat)
			}
			if got := id.GetPurposeId(); got != tc.wantPurpose {
				t.Errorf("purpose: got %q, want %q", got, tc.wantPurpose)
			}
			if !bytes.Equal(id.GetBytes(), wantBytes) {
				t.Errorf("bytes: got %x, want %x", id.GetBytes(), wantBytes)
			}
		})
	}
}
