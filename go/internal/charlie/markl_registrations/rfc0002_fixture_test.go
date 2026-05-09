//go:build test

package markl_registrations_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// rfc0002FixturePath is the on-disk RFC 0002 conformance fixture,
// relative to this test package's directory. Both the generator
// (TestGenerateRFC0002Vectors, gated by the rfc0002_generate build tag)
// and the round-trip verifier (TestRFC0002VectorsRoundTrip) read this
// constant. Lives under testdata/ so the file is part of the Go module
// source tree (visible to the nix sandbox build) rather than under
// docs/rfcs/, which is outside the buildGoApplication src closure.
// RFC 0002 §7.3 names this path canonically.
const rfc0002FixturePath = "testdata/0002-markl-id-format-vectors.json"

// rfc0002Fixture is the on-disk JSON shape pinned by RFC 0002 §7.1.
// Independent implementations load this same shape and verify each
// vector byte-for-byte.
type rfc0002Fixture struct {
	Vectors []rfc0002Vector        `json:"vectors"`
	Invalid []rfc0002InvalidVector `json:"invalid"`
}

// rfc0002Vector is one round-trip-conformant markl ID. Encoding
// PayloadHex (decoded to bytes) under Format and Purpose MUST produce
// Encoded; decoding Encoded MUST produce (Purpose, Format, payload).
type rfc0002Vector struct {
	Name       string `json:"name"`
	Purpose    string `json:"purpose,omitempty"`
	Format     string `json:"format"`
	PayloadHex string `json:"payload_hex"`
	Encoded    string `json:"encoded"`
}

// rfc0002InvalidVector is an encoded string the decoder MUST reject.
// Error names a structural failure category from RFC 0002 §4 — the
// exact error type is implementation-specific but the rejection MUST
// happen.
type rfc0002InvalidVector struct {
	Name    string `json:"name"`
	Encoded string `json:"encoded"`
	Error   string `json:"error"`
}

func loadRFC0002Fixture(t *testing.T) rfc0002Fixture {
	t.Helper()

	bites, err := os.ReadFile(rfc0002FixturePath)
	if err != nil {
		t.Fatalf("read %s: %v", rfc0002FixturePath, err)
	}

	var fixture rfc0002Fixture
	if err := json.Unmarshal(bites, &fixture); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	return fixture
}

func decodePayloadHex(t *testing.T, name, payloadHex string) []byte {
	t.Helper()

	out, err := hex.DecodeString(payloadHex)
	if err != nil {
		t.Fatalf("%s: decode payload_hex: %v", name, err)
	}

	return out
}
