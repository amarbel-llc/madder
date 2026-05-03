//go:build test

package hyphence

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"testing"
)

// Test vectors live in testdata/rfc_vectors.txt; format is documented in
// docs/rfcs/0001-hyphence.md (and the file's own comment header). Every
// conforming implementation MUST agree with these outcomes. Adding a new
// vector requires only a new line in the testdata file — no test-code
// edits.
func TestRFCConformance_HyphenceTestVectors(t *testing.T) {
	bites, err := os.ReadFile("testdata/rfc_vectors.txt")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}

	for lineNo, raw := range strings.Split(string(bites), "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) != 4 {
			t.Errorf("line %d: want 4 tab-separated fields, got %d", lineNo+1, len(parts))
			continue
		}

		name, inputB64, outcome, expectedB64 := parts[0], parts[1], parts[2], parts[3]

		input, err := base64.StdEncoding.DecodeString(inputB64)
		if err != nil {
			t.Errorf("%s: decode input b64: %v", name, err)
			continue
		}

		t.Run(name, func(t *testing.T) {
			runRFCVector(t, input, outcome, expectedB64)
		})
	}
}

func runRFCVector(t *testing.T, input []byte, outcome, expectedB64 string) {
	t.Helper()

	var blobCapture bytes.Buffer

	decoder := Decoder[*TypedBlob[struct{}]]{
		Metadata:      TypedMetadataCoder[struct{}]{},
		Blob:          testBlobDecoder{},
		BlobTeeWriter: &blobCapture,
	}

	typedBlob := &TypedBlob[struct{}]{}
	reader := bufio.NewReader(bytes.NewReader(input))

	_, err := decoder.DecodeFrom(typedBlob, reader)

	switch outcome {
	case "legacy/parse-ok":
		if err != nil {
			t.Fatalf("expected parse-ok, got error: %v", err)
		}

		var expected []byte
		if expectedB64 != "" && expectedB64 != "-" {
			expected, err = base64.StdEncoding.DecodeString(expectedB64)
			if err != nil {
				t.Fatalf("decode expected b64: %v", err)
			}
		}

		if !bytes.Equal(blobCapture.Bytes(), expected) {
			t.Errorf("blob mismatch: got %q, want %q", blobCapture.String(), string(expected))
		}

	case "legacy/parse-error-missing-separator":
		if err == nil {
			t.Fatal("expected parse-error-missing-separator, got nil")
		}
		if !errors.Is(err, errMissingNewlineAfterBoundary) {
			t.Errorf("expected errMissingNewlineAfterBoundary, got: %v", err)
		}

	default:
		if !strings.HasPrefix(outcome, "legacy/") {
			t.Skipf("outcome %q owned by another harness", outcome)
		}
		t.Fatalf("unknown outcome %q in legacy/ namespace", outcome)
	}
}
