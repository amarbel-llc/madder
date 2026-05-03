package commands_hyphence

import (
	"bytes"
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
)

func TestFormat_Canonicalizes(t *testing.T) {
	const input = "---\n! md\n# desc\n---\n\nbody\n"
	const want = "---\n# desc\n! md\n---\n\nbody\n"

	var out bytes.Buffer
	if err := runFormat(strings.NewReader(input), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out.String(); got != want {
		t.Errorf("output mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestFormat_Idempotent(t *testing.T) {
	const input = "---\n# desc\n! md\n---\n\nbody\n"
	var first, second bytes.Buffer
	if err := runFormat(strings.NewReader(input), &first); err != nil {
		t.Fatalf("first format: %v", err)
	}
	if err := runFormat(strings.NewReader(first.String()), &second); err != nil {
		t.Fatalf("second format: %v", err)
	}
	if first.String() != second.String() {
		t.Errorf("format is not idempotent:\nfirst:  %q\nsecond: %q", first.String(), second.String())
	}
}

func runFormat(in *strings.Reader, out *bytes.Buffer) error {
	doc := &hyphence.Document{}
	builder := &hyphence.MetadataBuilder{Doc: doc}
	emitter := &hyphence.FormatBodyEmitter{Doc: doc, Out: out}
	reader := hyphence.Reader{
		RequireMetadata: true,
		Metadata:        builder,
		Blob:            emitter,
	}
	_, err := reader.ReadFrom(in)
	return err
}
