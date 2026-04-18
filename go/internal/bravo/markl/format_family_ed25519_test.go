package markl

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
)

// Ed25519GetPublicKey must accept only Go's 64-byte ed25519.PrivateKey form.
// The 32-byte RFC 8032 seed is a distinct representation — accepting it
// silently lets callers skew against Ed25519Sign, which assumes 64 bytes
// (via `ed25519.PrivateKey(sec.GetBytes())`) and panics otherwise. See #15.

func TestEd25519GetPublicKey_Accepts64BytePrivateKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	var id Id
	if err := id.SetMarklId(FormatIdEd25519Sec, priv); err != nil {
		t.Fatalf("SetMarklId with 64-byte private key should succeed, got: %v", err)
	}

	bites, err := Ed25519GetPublicKey(&id)
	if err != nil {
		t.Fatalf("Ed25519GetPublicKey on 64-byte key should succeed, got: %v", err)
	}

	expected := priv.Public().(ed25519.PublicKey)
	if !bytes.Equal(bites, expected) {
		t.Errorf("derived pubkey mismatch: got %x, want %x", bites, expected)
	}
}

func TestEd25519GetPublicKey_RejectsSeedSizedBytes(t *testing.T) {
	seed := bytes.Repeat([]byte{0x01}, ed25519.SeedSize) // 32 bytes

	// Can't go through SetMarklId — the ed25519_sec format rejects non-64
	// byte inputs post-#13. Construct directly to simulate an external
	// caller passing an Id of the wrong shape to the exported function.
	id := Id{
		format: nil, // irrelevant to this path
		data:   append([]byte(nil), seed...),
	}

	_, err := Ed25519GetPublicKey(&id)
	if err == nil {
		t.Fatal("Ed25519GetPublicKey on 32-byte seed should error, got nil")
	}

	if !errors.Is(err, ErrEd25519SeedNotPrivateKey) {
		t.Errorf("expected ErrEd25519SeedNotPrivateKey, got: %v", err)
	}
}

func TestEd25519GetPublicKey_RejectsOtherSizes(t *testing.T) {
	for _, size := range []int{0, 16, 33, 63, 65, 128} {
		size := size
		t.Run("", func(t *testing.T) {
			id := Id{
				data: make([]byte, size),
			}

			_, err := Ed25519GetPublicKey(&id)
			if err == nil {
				t.Errorf("size %d should error", size)
			}
		})
	}
}

// End-to-end: verify Id.GetPublicKey (which delegates to Ed25519GetPublicKey
// via the FormatSec registration) still produces the correct public key for
// a 64-byte private key. This is the path exercised by every internal caller.
func TestIdGetPublicKey_Ed25519_MatchesStdlib(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	var secId Id
	if err := secId.SetPurposeId(PurposeRepoPrivateKeyV1); err != nil {
		t.Fatal(err)
	}
	if err := secId.SetMarklId(FormatIdEd25519Sec, priv); err != nil {
		t.Fatal(err)
	}

	pubId, err := secId.GetPublicKey(PurposeRepoPrivateKeyV1)
	if err != nil {
		t.Fatalf("Id.GetPublicKey should succeed, got: %v", err)
	}

	expected := priv.Public().(ed25519.PublicKey)
	if !bytes.Equal(pubId.GetBytes(), expected) {
		t.Errorf("pubkey mismatch: got %x, want %x", pubId.GetBytes(), expected)
	}
}
