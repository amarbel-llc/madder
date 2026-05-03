package commands_hyphence

import (
	"bytes"
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
)

func TestMeta_HappyPath(t *testing.T) {
	const input = "---\n# desc\n! md\n---\n\nbody bytes ignored\n"
	const want = "# desc\n! md\n"

	var out bytes.Buffer
	if err := runMeta(strings.NewReader(input), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out.String(); got != want {
		t.Errorf("output mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestMeta_NoBody(t *testing.T) {
	const input = "---\n! md\n---\n"
	const want = "! md\n"

	var out bytes.Buffer
	if err := runMeta(strings.NewReader(input), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out.String(); got != want {
		t.Errorf("output mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func runMeta(in *strings.Reader, out *bytes.Buffer) error {
	streamer := &hyphence.MetadataStreamer{W: out}
	body := &CountingDiscardReaderFrom{}
	reader := hyphence.Reader{
		RequireMetadata: true,
		Metadata:        streamer,
		Blob:            body,
	}
	_, err := reader.ReadFrom(in)
	return err
}
