package commands_hyphence

import (
	"bytes"
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
)

func TestBody_StreamsThrough(t *testing.T) {
	const input = "---\n! md\n---\n\nhello world\n"
	const want = "hello world\n"

	var out bytes.Buffer
	if err := runBody(strings.NewReader(input), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out.String(); got != want {
		t.Errorf("output mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestBody_NoBody(t *testing.T) {
	const input = "---\n! md\n---\n"
	var out bytes.Buffer
	if err := runBody(strings.NewReader(input), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out.String(); got != "" {
		t.Errorf("expected empty output, got %q", got)
	}
}

func runBody(in *strings.Reader, out *bytes.Buffer) error {
	body := &writerReaderFrom{W: out}
	reader := hyphence.Reader{
		RequireMetadata: true,
		Metadata:        &CountingDiscardReaderFrom{},
		Blob:            body,
	}
	_, err := reader.ReadFrom(in)
	return err
}
