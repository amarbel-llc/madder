package sftp_probe

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

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
