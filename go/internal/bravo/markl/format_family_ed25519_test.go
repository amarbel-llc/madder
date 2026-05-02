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

// End-to-end Id.GetPublicKey coverage lives in
// internal/charlie/markl_registrations because the implementation in
// id_crypto_sec.go hardcodes PurposeRepoPubKeyV1 — a dodder vocabulary
// constant that this package's tests deliberately do not depend on.

// Ed25519Sign: same size-validation contract as Ed25519GetPublicKey (#15).
// Internal callers reach it through Id.Sign → FormatSec.Sign and can't
// reach these branches (FormatSec enforces 64-byte size by construction);
// the checks guard external callers of go/pkgs/markl.Ed25519Sign that
// pass a foreign MarklId of the wrong size. See #23.

func TestEd25519Sign_RejectsSeedSizedBytes(t *testing.T) {
	seed := bytes.Repeat([]byte{0x01}, ed25519.SeedSize) // 32 bytes

	// Bypass SetMarklId to simulate an external caller presenting an Id
	// with raw data of the wrong size (matches the Get test pattern).
	secId := Id{data: append([]byte(nil), seed...)}
	msgId := Id{data: []byte("hello")}

	_, err := Ed25519Sign(&secId, &msgId, nil)
	if err == nil {
		t.Fatal("Ed25519Sign on 32-byte seed should error, got nil")
	}

	if !errors.Is(err, ErrEd25519SeedNotPrivateKey) {
		t.Errorf("expected ErrEd25519SeedNotPrivateKey, got: %v", err)
	}
}

func TestEd25519Sign_RejectsOtherSizes(t *testing.T) {
	msgId := Id{data: []byte("hello")}

	for _, size := range []int{0, 16, 33, 63, 65, 128} {
		size := size
		t.Run("", func(t *testing.T) {
			secId := Id{data: make([]byte, size)}

			_, err := Ed25519Sign(&secId, &msgId, nil)
			if err == nil {
				t.Errorf("size %d should error", size)
			}
		})
	}
}

func TestEd25519Sign_RoundTripMatchesStdlib(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	var secId Id
	if err := secId.SetMarklId(FormatIdEd25519Sec, priv); err != nil {
		t.Fatal(err)
	}

	msg := []byte("hello, world")
	msgId := Id{data: msg}

	sig, err := Ed25519Sign(&secId, &msgId, nil)
	if err != nil {
		t.Fatalf("Ed25519Sign on valid 64-byte key should succeed, got: %v", err)
	}

	if !ed25519.Verify(pub, msg, sig) {
		t.Error("signature produced by Ed25519Sign did not verify against stdlib ed25519.Verify")
	}
}

// Ed25519Verify: external callers could pass a MarklId with a pubkey or
// signature of the wrong size; stdlib VerifyWithOptions panics. See #23.

func TestEd25519Verify_RejectsWrongPubkeySize(t *testing.T) {
	msgId := Id{data: []byte("hello")}
	sigId := Id{data: make([]byte, ed25519.SignatureSize)}

	for _, size := range []int{0, 16, 31, 33, 64} {
		size := size
		t.Run("", func(t *testing.T) {
			pubId := Id{data: make([]byte, size)}

			err := Ed25519Verify(&pubId, &msgId, &sigId)
			if err == nil {
				t.Errorf("pubkey size %d should error", size)
			}
		})
	}
}

func TestEd25519Verify_RejectsWrongSigSize(t *testing.T) {
	pubId := Id{data: make([]byte, ed25519.PublicKeySize)}
	msgId := Id{data: []byte("hello")}

	for _, size := range []int{0, 32, 63, 65, 128} {
		size := size
		t.Run("", func(t *testing.T) {
			sigId := Id{data: make([]byte, size)}

			err := Ed25519Verify(&pubId, &msgId, &sigId)
			if err == nil {
				t.Errorf("sig size %d should error", size)
			}
		})
	}
}

func TestEd25519Verify_RoundTripAcceptsValidSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	var secId Id
	if err := secId.SetMarklId(FormatIdEd25519Sec, priv); err != nil {
		t.Fatal(err)
	}

	msg := []byte("round-trip")
	msgId := Id{data: msg}

	sig, err := Ed25519Sign(&secId, &msgId, nil)
	if err != nil {
		t.Fatal(err)
	}

	pubId := Id{data: append([]byte(nil), pub...)}
	sigId := Id{data: sig}

	if err := Ed25519Verify(&pubId, &msgId, &sigId); err != nil {
		t.Errorf("Ed25519Verify on valid inputs should succeed, got: %v", err)
	}
}
