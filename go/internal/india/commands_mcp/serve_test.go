//go:build test

package commands_mcp

import (
	"strings"
	"testing"
)

const validBlobDigest = "blake2b256-c5xgv9eyuv6g49mcwqks24gd3dh39w8220l0kl60qxt60rnt60lsc8fqv0"

func TestParseBlobURI_ValidDigest(t *testing.T) {
	uri := blobURIPrefix + validBlobDigest

	id, err := parseBlobURI(uri)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if id.String() != validBlobDigest {
		t.Errorf("round-trip mismatch: got %q, want %q", id.String(), validBlobDigest)
	}
}

func TestParseBlobURI_RejectsMissingPrefix(t *testing.T) {
	cases := []struct {
		name string
		uri  string
	}{
		{"empty", ""},
		{"plain digest", validBlobDigest},
		{"http scheme", "http://blobs/" + validBlobDigest},
		{"different host", "madder://other/" + validBlobDigest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseBlobURI(tc.uri)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), blobURIPrefix) {
				t.Errorf("error %q does not mention the expected prefix", err.Error())
			}
		})
	}
}

func TestParseBlobURI_RejectsMalformedDigest(t *testing.T) {
	cases := []struct {
		name string
		uri  string
	}{
		{"empty digest", blobURIPrefix},
		{"missing hash type", blobURIPrefix + "abc123"},
		{"unknown hash type", blobURIPrefix + "fakehash-abc123"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseBlobURI(tc.uri); err == nil {
				t.Fatalf("expected error for %q, got nil", tc.uri)
			}
		})
	}
}
