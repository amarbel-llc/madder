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

func TestMetadataBuilder_PopulatesAllPrefixes(t *testing.T) {
	const input = "# desc one\n# desc two\n- tag\n@ blake2b256-abc\n! md\n"
	doc := &Document{}
	builder := &MetadataBuilder{Doc: doc}
	if _, err := builder.ReadFrom(strings.NewReader(input)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []MetadataLine{
		{Prefix: '#', Value: "desc one"},
		{Prefix: '#', Value: "desc two"},
		{Prefix: '-', Value: "tag"},
		{Prefix: '@', Value: "blake2b256-abc"},
		{Prefix: '!', Value: "md"},
	}
	if len(doc.Metadata) != len(want) {
		t.Fatalf("got %d lines, want %d: %+v", len(doc.Metadata), len(want), doc.Metadata)
	}
	for i, w := range want {
		got := doc.Metadata[i]
		if got.Prefix != w.Prefix || got.Value != w.Value {
			t.Errorf("line %d: got %+v, want %+v", i, got, w)
		}
	}
}

func TestMetadataBuilder_AnchorsLeadingComments(t *testing.T) {
	const input = "% comment one\n% comment two\n- tag\n! md\n"
	doc := &Document{}
	builder := &MetadataBuilder{Doc: doc}
	if _, err := builder.ReadFrom(strings.NewReader(input)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(doc.Metadata) != 2 {
		t.Fatalf("expected 2 non-comment lines, got %d", len(doc.Metadata))
	}
	tagLine := doc.Metadata[0]
	if tagLine.Prefix != '-' {
		t.Fatalf("first non-comment line should be '-', got %q", tagLine.Prefix)
	}
	if got := tagLine.LeadingComments; len(got) != 2 || got[0] != "comment one" || got[1] != "comment two" {
		t.Errorf("LeadingComments mismatch: %+v", got)
	}
}

func TestMetadataBuilder_TrailingComments(t *testing.T) {
	const input = "! md\n% trailing one\n% trailing two\n"
	doc := &Document{}
	builder := &MetadataBuilder{Doc: doc}
	if _, err := builder.ReadFrom(strings.NewReader(input)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(doc.Metadata) != 1 {
		t.Fatalf("expected 1 non-comment line, got %d", len(doc.Metadata))
	}
	if got := doc.TrailingComments; len(got) != 2 || got[0] != "trailing one" || got[1] != "trailing two" {
		t.Errorf("TrailingComments mismatch: %+v", got)
	}
}

func TestMetadataBuilder_RejectsMalformedLine(t *testing.T) {
	// Per RFC 0001 §Metadata Lines, every line must be `<prefix> <content>`.
	// A line with no space after the prefix is malformed.
	const input = "!nospace\n"
	doc := &Document{}
	builder := &MetadataBuilder{Doc: doc}
	_, err := builder.ReadFrom(strings.NewReader(input))
	if !errors.Is(err, ErrMalformedMetadataLine) {
		t.Errorf("expected ErrMalformedMetadataLine, got %v", err)
	}
}

func TestMetadataBuilder_RejectsInvalidPrefix(t *testing.T) {
	const input = "X bad\n"
	doc := &Document{}
	builder := &MetadataBuilder{Doc: doc}
	_, err := builder.ReadFrom(strings.NewReader(input))
	if !errors.Is(err, ErrInvalidPrefix) {
		t.Errorf("expected ErrInvalidPrefix, got %v", err)
	}
}

func TestMetadataValidator_ValidInputAcceptsAllPrefixes(t *testing.T) {
	const input = "# desc\n- tag\n@ blake2b256-abc\n< object/id\n% comment\n! md\n"
	v := &MetadataValidator{}
	if _, err := v.ReadFrom(strings.NewReader(input)); err != nil {
		t.Errorf("expected nil error on valid input, got %v", err)
	}
	if !v.SawAtLine {
		t.Errorf("validator should have observed @ line, SawAtLine=false")
	}
}

func TestMetadataValidator_RejectsInvalidPrefix(t *testing.T) {
	const input = "! md\nX bad\n"
	v := &MetadataValidator{}
	_, err := v.ReadFrom(strings.NewReader(input))
	if !errors.Is(err, ErrInvalidPrefix) {
		t.Errorf("expected ErrInvalidPrefix, got %v", err)
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("expected line number 2 in error, got %v", err)
	}
}

func TestMetadataValidator_RejectsMissingSpace(t *testing.T) {
	const input = "!nospace\n"
	v := &MetadataValidator{}
	_, err := v.ReadFrom(strings.NewReader(input))
	if !errors.Is(err, ErrMalformedMetadataLine) {
		t.Errorf("expected ErrMalformedMetadataLine, got %v", err)
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Errorf("expected line number 1, got %v", err)
	}
}

func TestMetadataValidator_RejectsCarriageReturn(t *testing.T) {
	// Per RFC 0001, embedded \r in a metadata line is malformed
	// (content is "arbitrary UTF-8 except LF" — \r is allowed by
	// that rule but the boundary scanner already rejects \r in
	// boundary lines; for content lines we choose to surface CR
	// as malformed because tooling round-trips assume LF-only).
	// If this proves too strict, soften it later.
	const input = "! md\r\n"
	v := &MetadataValidator{}
	_, err := v.ReadFrom(strings.NewReader(input))
	if !errors.Is(err, ErrMalformedMetadataLine) {
		t.Errorf("expected ErrMalformedMetadataLine for \\r, got %v", err)
	}
}
