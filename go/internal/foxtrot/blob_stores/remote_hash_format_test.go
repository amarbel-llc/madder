//go:build test

package blob_stores

import (
	"strings"
	"testing"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
)

// TestResolveWriteHashFormat covers the shared write-hash-format
// decision used by the SFTP, S3, and WebDAV stores (#261, #262):
// multi-hash honors any requested type, single-hash accepts only nil
// or its own type and rejects anything else, and nil always falls back
// to the store default.
func TestResolveWriteHashFormat(t *testing.T) {
	sha := markl.FormatHashSha256
	blake := markl.FormatHashBlake2b256

	cases := []struct {
		name       string
		requested  domain_interfaces.FormatHash
		multiHash  bool
		wantFormat string // "" when an error is expected
		wantErr    bool
	}{
		{"nil/single-hash", nil, false, sha.GetMarklFormatId(), false},
		{"matching/single-hash", sha, false, sha.GetMarklFormatId(), false},
		{"foreign/single-hash", blake, false, "", true},
		{"nil/multi-hash", nil, true, sha.GetMarklFormatId(), false},
		{"matching/multi-hash", sha, true, sha.GetMarklFormatId(), false},
		{"foreign/multi-hash", blake, true, blake.GetMarklFormatId(), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveWriteHashFormat(
				tc.requested, sha, tc.multiHash, "test-store",
			)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got format %q", got.GetMarklFormatId())
				}
				if !strings.Contains(err.Error(), "single-hash") {
					t.Errorf("error %q missing 'single-hash' anchor", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.GetMarklFormatId() != tc.wantFormat {
				t.Fatalf(
					"format = %q, want %q",
					got.GetMarklFormatId(), tc.wantFormat,
				)
			}
		})
	}
}

// TestReadHashFormatForDigest confirms the shared reader-hash resolver
// derives the format from the blob id's own markl type rather than any
// store default (#261, #262).
func TestReadHashFormatForDigest(t *testing.T) {
	id, repool := markl.FormatHashBlake2b256.GetMarklIdForString("read-hash-format")
	t.Cleanup(repool)

	got, err := readHashFormatForDigest(id)
	if err != nil {
		t.Fatalf("readHashFormatForDigest: %v", err)
	}
	if want := markl.FormatHashBlake2b256.GetMarklFormatId(); got.GetMarklFormatId() != want {
		t.Fatalf("format = %q, want %q", got.GetMarklFormatId(), want)
	}
}
