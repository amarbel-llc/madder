package hyphence

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"os"
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

func TestCanonicalize_PrefixOrder(t *testing.T) {
	// RFC §Canonical Line Order: # → < (locked refs in source order
	// — we don't yet model the lock distinction, see #128, so all <
	// stays in source order) → - → @ → !.
	doc := &Document{
		Metadata: []MetadataLine{
			{Prefix: '!', Value: "md"},
			{Prefix: '@', Value: "blake2b256-abc"},
			{Prefix: '-', Value: "tag-one"},
			{Prefix: '#', Value: "desc"},
			{Prefix: '<', Value: "object/id"},
			{Prefix: '-', Value: "tag-two"},
		},
	}

	Canonicalize(doc)

	wantOrder := []byte{'#', '<', '-', '-', '@', '!'}
	got := make([]byte, len(doc.Metadata))
	for i, ml := range doc.Metadata {
		got[i] = ml.Prefix
	}
	if string(got) != string(wantOrder) {
		t.Errorf("prefix order: got %q, want %q", got, wantOrder)
	}

	// Within the `-` bucket, source order preserved (stable sort).
	var dashValues []string
	for _, ml := range doc.Metadata {
		if ml.Prefix == '-' {
			dashValues = append(dashValues, ml.Value)
		}
	}
	if len(dashValues) != 2 || dashValues[0] != "tag-one" || dashValues[1] != "tag-two" {
		t.Errorf("dash bucket should preserve source order, got %v", dashValues)
	}
}

func TestCanonicalize_PreservesLeadingComments(t *testing.T) {
	doc := &Document{
		Metadata: []MetadataLine{
			{Prefix: '!', Value: "md", LeadingComments: []string{"about-type"}},
			{Prefix: '#', Value: "desc"},
		},
	}
	Canonicalize(doc)

	if doc.Metadata[0].Prefix != '#' {
		t.Fatalf("# should sort first, got %q", doc.Metadata[0].Prefix)
	}
	if doc.Metadata[1].Prefix != '!' {
		t.Fatalf("! should sort last, got %q", doc.Metadata[1].Prefix)
	}
	if got := doc.Metadata[1].LeadingComments; len(got) != 1 || got[0] != "about-type" {
		t.Errorf("LeadingComments should travel with their line: %+v", got)
	}
}

func TestFormatBodyEmitter_EmitsCanonicalizedMetadataThenBody(t *testing.T) {
	doc := &Document{
		Metadata: []MetadataLine{
			{Prefix: '!', Value: "md"},
			{Prefix: '#', Value: "desc"},
		},
	}
	const body = "hello\n"
	var out bytes.Buffer
	emitter := &FormatBodyEmitter{Doc: doc, Out: &out}
	if _, err := emitter.ReadFrom(strings.NewReader(body)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const want = "---\n# desc\n! md\n---\n\nhello\n"
	if got := out.String(); got != want {
		t.Errorf("output mismatch:\n got: %q\nwant: %q", got, want)
	}
	if !doc.HasBody {
		t.Error("Doc.HasBody should be set after emitter saw bytes")
	}
}

func TestFormatBodyEmitter_NoBody(t *testing.T) {
	doc := &Document{
		Metadata: []MetadataLine{{Prefix: '!', Value: "md"}},
	}
	var out bytes.Buffer
	emitter := &FormatBodyEmitter{Doc: doc, Out: &out}
	if _, err := emitter.ReadFrom(strings.NewReader("")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const want = "---\n! md\n---\n"
	if got := out.String(); got != want {
		t.Errorf("output mismatch:\n got: %q\nwant: %q", got, want)
	}
	if doc.HasBody {
		t.Error("Doc.HasBody should be false when no bytes followed")
	}
}

func TestFormatBodyEmitter_LeadingAndTrailingComments(t *testing.T) {
	doc := &Document{
		Metadata: []MetadataLine{
			{Prefix: '!', Value: "md", LeadingComments: []string{"about-type"}},
		},
		TrailingComments: []string{"end note"},
	}
	var out bytes.Buffer
	emitter := &FormatBodyEmitter{Doc: doc, Out: &out}
	if _, err := emitter.ReadFrom(strings.NewReader("")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const want = "---\n% about-type\n! md\n% end note\n---\n"
	if got := out.String(); got != want {
		t.Errorf("output mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestDocumentRFCConformance(t *testing.T) {
	f, err := os.Open("testdata/rfc_vectors.txt")
	if err != nil {
		t.Fatalf("open vectors: %v", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 4 {
			t.Errorf("malformed vector line: %q", line)
			continue
		}
		name := fields[0]
		input, decErr := base64.StdEncoding.DecodeString(fields[1])
		if decErr != nil {
			t.Errorf("vector %s: base64 decode: %v", name, decErr)
			continue
		}
		outcome := fields[2]

		t.Run(name, func(t *testing.T) {
			runDocumentVector(t, name, input, outcome)
		})
	}
	if err := sc.Err(); err != nil {
		t.Errorf("scan: %v", err)
	}
}

func runDocumentVector(t *testing.T, name string, input []byte, outcome string) {
	switch outcome {
	case "document/parse-error-invalid-prefix":
		v := &MetadataValidator{}
		reader := Reader{RequireMetadata: true, Metadata: v, Blob: discardReaderFrom{}}
		_, err := reader.ReadFrom(bytes.NewReader(input))
		if !errors.Is(err, ErrInvalidPrefix) {
			t.Errorf("expected ErrInvalidPrefix, got %v", err)
		}
	case "document/parse-error-malformed-line":
		v := &MetadataValidator{}
		reader := Reader{RequireMetadata: true, Metadata: v, Blob: discardReaderFrom{}}
		_, err := reader.ReadFrom(bytes.NewReader(input))
		if !errors.Is(err, ErrMalformedMetadataLine) {
			t.Errorf("expected ErrMalformedMetadataLine, got %v", err)
		}
	case "document/parse-error-inline-body-with-at":
		v := &MetadataValidator{}
		body := &countingDiscard{}
		reader := Reader{RequireMetadata: true, Metadata: v, Blob: body}
		_, err := reader.ReadFrom(bytes.NewReader(input))
		if err != nil {
			t.Fatalf("unexpected scan error: %v", err)
		}
		if !v.SawAtLine || !body.SawBody {
			t.Errorf("expected SawAtLine && SawBody, got SawAtLine=%v SawBody=%v", v.SawAtLine, body.SawBody)
		}
	default:
		if !strings.HasPrefix(outcome, "document/") {
			t.Skipf("outcome %q owned by another harness", outcome)
		}
		t.Fatalf("unknown outcome %q in document/ namespace", outcome)
	}
}

type discardReaderFrom struct{}

func (discardReaderFrom) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(io.Discard, r)
}

type countingDiscard struct {
	SawBody bool
}

func (c *countingDiscard) ReadFrom(r io.Reader) (int64, error) {
	n, err := io.Copy(io.Discard, r)
	if n > 0 {
		c.SawBody = true
	}
	return n, err
}

func TestMetadataBuilder_RejectsCarriageReturn(t *testing.T) {
	// MetadataBuilder must reject embedded \r the same way
	// MetadataValidator does — otherwise CRLF input round-trips
	// \r-contaminated MetadataLine values, and a format ->
	// validate pipeline disagrees about whether the document is
	// well-formed.
	const input = "! md\r\n"
	doc := &Document{}
	builder := &MetadataBuilder{Doc: doc}
	_, err := builder.ReadFrom(strings.NewReader(input))
	if !errors.Is(err, ErrMalformedMetadataLine) {
		t.Errorf("expected ErrMalformedMetadataLine for \\r, got %v", err)
	}
}
