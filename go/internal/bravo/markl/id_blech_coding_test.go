package markl

import (
	"bytes"
	"testing"
)

// Regression for #157: dodder's legacy box_format wire form prefixes
// purposeless markl-ID tokens with `@` to distinguish them from other
// tokens (type tags `!type`, etc.). After RFC 0002 landed in madder
// v0.3.16, SetMarklIdWithFormatBlech32 fed the `@`-prefixed string
// straight into id.Set; blech32 decoded HRP=`@<algo>` (with the `@`)
// and the checksum failed because the encoder side had written with
// HRP=`<algo>`. The function-level fallback to setSha256 then ran
// hex.DecodeString on the `@`-bearing input, surfacing as
// `encoding/hex: invalid byte: U+0040 '@'`. Stripping the leading `@`
// before Set fixes all three dodder call sites in box_format/read.go.
//
// Each fixture below is a real RFC 0002 string from the madder
// fixture (testdata/0002-markl-id-format-vectors.json), prefixed with
// `@` to mimic the legacy dodder wire form.
func TestSetMarklIdWithFormatBlech32_StripsLegacyAtPrefix(t *testing.T) {
	cases := []struct {
		name        string
		canonical   string
		legacyValue string
		wantFormat  string
	}{
		{
			name:        "sha256",
			canonical:   "sha256-qqqsyqcyq5rqwzqfpg9scrgwpugpzysnzs23v9ccrydpk8qarc0s7lcgm6",
			legacyValue: "@sha256-qqqsyqcyq5rqwzqfpg9scrgwpugpzysnzs23v9ccrydpk8qarc0s7lcgm6",
			wantFormat:  FormatIdHashSha256,
		},
		{
			name:        "blake2b256",
			canonical:   "blake2b256-qqqsyqcyq5rqwzqfpg9scrgwpugpzysnzs23v9ccrydpk8qarc0s6vk400",
			legacyValue: "@blake2b256-qqqsyqcyq5rqwzqfpg9scrgwpugpzysnzs23v9ccrydpk8qarc0s6vk400",
			wantFormat:  FormatIdHashBlake2b256,
		},
		{
			name:        "ed25519_pub",
			canonical:   "ed25519_pub-qqqsyqcyq5rqwzqfpg9scrgwpugpzysnzs23v9ccrydpk8qarc0srk9anc",
			legacyValue: "@ed25519_pub-qqqsyqcyq5rqwzqfpg9scrgwpugpzysnzs23v9ccrydpk8qarc0srk9anc",
			wantFormat:  FormatIdEd25519Pub,
		},
		{
			name:        "ed25519_sig",
			canonical:   "ed25519_sig-qqqsyqcyq5rqwzqfpg9scrgwpugpzysnzs23v9ccrydpk8qarc0jqgfzyvjz2f389q5j52ev95hz7vp3xgengdfkxuurjw3m8s7nu0cy4lu83",
			legacyValue: "@ed25519_sig-qqqsyqcyq5rqwzqfpg9scrgwpugpzysnzs23v9ccrydpk8qarc0jqgfzyvjz2f389q5j52ev95hz7vp3xgengdfkxuurjw3m8s7nu0cy4lu83",
			wantFormat:  FormatIdEd25519Sig,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var canonicalId Id
			if err := canonicalId.Set(tc.canonical); err != nil {
				t.Fatalf("canonical %q: %v", tc.canonical, err)
			}

			var legacyId Id
			if err := SetMarklIdWithFormatBlech32(&legacyId, "", tc.legacyValue); err != nil {
				t.Fatalf("legacy @-prefixed input %q: %v", tc.legacyValue, err)
			}

			format := legacyId.GetMarklFormat()
			if format == nil {
				t.Fatal("legacy: format is nil after parse")
			}
			if got := format.GetMarklFormatId(); got != tc.wantFormat {
				t.Errorf("legacy format: got %q, want %q", got, tc.wantFormat)
			}
			if !bytes.Equal(legacyId.GetBytes(), canonicalId.GetBytes()) {
				t.Errorf("legacy and canonical decode produced different bytes:\n legacy:    %x\n canonical: %x",
					legacyId.GetBytes(), canonicalId.GetBytes())
			}
		})
	}
}
