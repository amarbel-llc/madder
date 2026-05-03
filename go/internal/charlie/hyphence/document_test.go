package hyphence

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestDocument_Zero(t *testing.T) {
	var doc Document
	if doc.HasBody {
		t.Errorf("zero Document should have HasBody=false")
	}
	if len(doc.Metadata) != 0 {
		t.Errorf("zero Document should have empty Metadata, got %d entries", len(doc.Metadata))
	}
	if len(doc.TrailingComments) != 0 {
		t.Errorf("zero Document should have empty TrailingComments, got %d entries", len(doc.TrailingComments))
	}
}

func TestMetadataLine_Zero(t *testing.T) {
	var line MetadataLine
	if line.Prefix != 0 {
		t.Errorf("zero MetadataLine should have Prefix=0, got %q", line.Prefix)
	}
	if line.Value != "" {
		t.Errorf("zero MetadataLine should have empty Value")
	}
}

func TestSentinelErrors_AreDistinct(t *testing.T) {
	all := []error{ErrMalformedMetadataLine, ErrInvalidPrefix, ErrInlineBodyWithAtReference}
	for i, a := range all {
		for j, b := range all {
			if i != j && errors.Is(a, b) {
				t.Errorf("sentinel errors %v and %v should not match via errors.Is", a, b)
			}
		}
	}
}

func TestMetadataStreamer_CopiesVerbatim(t *testing.T) {
	// MetadataStreamer is fed the metadata content (between the
	// two `---` lines) by hyphence.Reader's piped reader. Test it
	// in isolation by writing the same bytes directly.
	const input = "# desc\n- tag\n! md\n"
	var out bytes.Buffer
	streamer := &MetadataStreamer{W: &out}
	n, err := streamer.ReadFrom(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != int64(len(input)) {
		t.Errorf("byte count mismatch: got %d, want %d", n, len(input))
	}
	if got := out.String(); got != input {
		t.Errorf("output mismatch:\n got: %q\nwant: %q", got, input)
	}
}

func TestMetadataStreamer_EmptyInput(t *testing.T) {
	var out bytes.Buffer
	streamer := &MetadataStreamer{W: &out}
	n, err := streamer.ReadFrom(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("byte count mismatch: got %d, want 0", n)
	}
	if out.Len() != 0 {
		t.Errorf("output should be empty, got %q", out.String())
	}
}
