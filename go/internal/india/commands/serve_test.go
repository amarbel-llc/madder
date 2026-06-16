//go:build test

package commands

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A well-formed blake2b256 digest (canonical form). Borrowed from the
// MCP smoke recipe's fixture; not present in any store, which is fine —
// these tests exercise only digest parsing, not store lookup.
const validDigest = "blake2b256-c5xgv9eyuv6g49mcwqks24gd3dh39w8220l0kl60qxt60rnt60lsc8fqv0"

// TestParseDigest_RejectsMalformed pins that a malformed {digest} path
// value yields a 400 and ok=false (the handlers return early).
func TestParseDigest_RejectsMalformed(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/blobs/not-a-valid-digest", nil)
	r.SetPathValue("digest", "not-a-valid-digest")

	if _, ok := parseDigest(w, r); ok {
		t.Fatal("parseDigest accepted a malformed digest")
	}

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestParseDigest_AcceptsValid pins that a canonical digest parses and
// round-trips through String() unchanged — the equality handlePut relies
// on when verifying a PUT body's content-addressed digest against the URL.
func TestParseDigest_AcceptsValid(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/blobs/"+validDigest, nil)
	r.SetPathValue("digest", validDigest)

	id, ok := parseDigest(w, r)
	if !ok {
		t.Fatalf("parseDigest rejected a valid digest (status %d)", w.Code)
	}

	if id.String() != validDigest {
		t.Fatalf("id round-trip = %q, want %q", id.String(), validDigest)
	}
}
