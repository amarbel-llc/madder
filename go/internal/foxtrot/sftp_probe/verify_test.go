package sftp_probe

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/bravo/plugins"
	_ "github.com/amarbel-llc/madder/go/internal/bravo/plugins/builtins"
	_ "github.com/amarbel-llc/madder/go/internal/charlie/markl_registrations"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_io"
)

// candidateNoneNoneSha256 is the simplest candidate: no compression,
// no encryption, sha256 hash. Reuses blob_io.DefaultConfig (the
// identity bundle) so the test doesn't have to thread through
// compression-plugin lookup yet.
func candidateNoneNoneSha256(t *testing.T) Candidate {
	t.Helper()
	return Candidate{
		IOConfig: blob_io.DefaultConfig,
		Label:    "none/none",
	}
}

// candidateForLegacyCompression builds a Candidate whose IOConfig
// uses the named legacy compression (one of "none", "gzip", "zlib",
// "zstd"). Encryption is none. Hash is sha256 (the project default).
//
// Mirrors the resolution pattern used by TomlV3.GetBlobCompression
// at internal/charlie/blob_store_configs/toml_v3.go:80.
func candidateForLegacyCompression(t *testing.T, comp string) Candidate {
	t.Helper()
	ref, err := plugins.LegacyCompressionRef(comp)
	if err != nil {
		t.Fatalf("LegacyCompressionRef(%q): %v", comp, err)
	}
	wrapper, err := plugins.Resolve(ref)
	if err != nil {
		t.Fatalf("plugins.Resolve(%q): %v", ref, err)
	}
	cfg := blob_io.MakeConfig(
		blob_store_configs.DefaultHashType,
		nil, // funcJoin unused for verification
		wrapper,
		nil, // no encryption
	)
	return Candidate{IOConfig: cfg, Label: comp + "/none"}
}

func TestVerifySample_NoneNone_OK(t *testing.T) {
	cleartext := []byte("hello probe")
	digest := sha256.Sum256(cleartext)
	expectedHex := hex.EncodeToString(digest[:])

	cand := candidateNoneNoneSha256(t)

	got := VerifySample(bytes.NewReader(cleartext), expectedHex, cand)

	if !got.Ok {
		t.Fatalf("VerifySample returned Ok=false; want true. Stage=%s Err=%v",
			got.Stage, got.Err)
	}
	if got.Stage != StageOK {
		t.Errorf("Stage = %s, want %s", got.Stage, StageOK)
	}
}

// generateAgeKeyForTest produces a fresh age-x25519 private key as
// a markl.Id. Returned by value because markl.Id is a value type;
// callers take its address when an interface receiver is needed.
func generateAgeKeyForTest(t *testing.T) markl.Id {
	t.Helper()
	var key markl.Id
	if err := key.GeneratePrivateKey(
		nil,
		markl.FormatIdAgeX25519Sec,
		markl.PurposeMadderPrivateKeyV1,
	); err != nil {
		t.Fatalf("GeneratePrivateKey: %v", err)
	}
	return key
}

// candidateForCompressionAndKey builds a Candidate combining a
// legacy compression name with an optional age key. When key is
// nil, the candidate has no encryption.
func candidateForCompressionAndKey(
	t *testing.T,
	comp string,
	key *markl.Id,
) Candidate {
	t.Helper()
	ref, err := plugins.LegacyCompressionRef(comp)
	if err != nil {
		t.Fatalf("LegacyCompressionRef(%q): %v", comp, err)
	}
	wrapper, err := plugins.Resolve(ref)
	if err != nil {
		t.Fatalf("plugins.Resolve(%q): %v", ref, err)
	}

	// MakeConfig accepts a typed nil interface for "no encryption";
	// passing a nil *markl.Id wrapped as an interface would not
	// satisfy the != nil check inside MakeConfig.
	var enc domain_interfaces.MarklId
	if key != nil {
		enc = key
	}

	cfg := blob_io.MakeConfig(
		blob_store_configs.DefaultHashType,
		nil,
		wrapper,
		enc,
	)

	label := comp + "/none"
	if key != nil {
		label = comp + "/age"
	}
	return Candidate{IOConfig: cfg, Label: label}
}

func TestVerifySample_AgeRoundTrip(t *testing.T) {
	cleartext := []byte("hello age — encrypted blob bytes round-tripping through the pipeline")
	digest := sha256.Sum256(cleartext)
	expectedHex := hex.EncodeToString(digest[:])

	key := generateAgeKeyForTest(t)
	cand := candidateForCompressionAndKey(t, "zstd", &key)

	var encoded bytes.Buffer
	w, err := blob_io.NewWriter(cand.IOConfig, &encoded)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if _, err := w.Write(cleartext); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := VerifySample(bytes.NewReader(encoded.Bytes()), expectedHex, cand)
	if !got.Ok {
		t.Fatalf("Ok=false; Stage=%s Err=%v", got.Stage, got.Err)
	}
}

func TestVerifySample_CompressionRoundTrips(t *testing.T) {
	cleartext := []byte("hello probe — non-trivial content for compression to do work")
	digest := sha256.Sum256(cleartext)
	expectedHex := hex.EncodeToString(digest[:])

	for _, comp := range []string{"none", "gzip", "zlib", "zstd"} {
		t.Run(comp, func(t *testing.T) {
			cand := candidateForLegacyCompression(t, comp)

			// Forward: encode cleartext through the candidate's IO
			// config to produce on-disk bytes.
			var encoded bytes.Buffer
			w, err := blob_io.NewWriter(cand.IOConfig, &encoded)
			if err != nil {
				t.Fatalf("NewWriter: %v", err)
			}
			if _, err := w.Write(cleartext); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if err := w.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			// Backward: VerifySample on the same candidate must
			// accept the encoded bytes.
			got := VerifySample(bytes.NewReader(encoded.Bytes()), expectedHex, cand)
			if !got.Ok {
				t.Fatalf("Ok=false; Stage=%s Err=%v", got.Stage, got.Err)
			}
		})
	}
}
