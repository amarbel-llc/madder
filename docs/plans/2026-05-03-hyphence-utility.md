# Hyphence Utility Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use eng:subagent-driven-development to implement this plan task-by-task.

**Goal:** Land a new sibling utility `hyphence` (binary at `go/cmd/hyphence/`) with subcommands `validate`, `meta`, `body`, `format` for format-only inspection and re-emission of on-disk hyphence documents per RFC 0001.

**Architecture:** Extend the existing `go/internal/charlie/hyphence/` package with a `Document` data type, four `io.ReaderFrom` consumers (`MetadataStreamer`, `MetadataBuilder`, `MetadataValidator`, `FormatBodyEmitter`), a `Canonicalize` helper, and sentinel errors. Subcommands wire the existing `hyphence.Reader{Metadata, Blob}` engine to these consumers; both halves stream, only `format` buffers metadata. Utility scaffolding mirrors `commands_cache` and `commands_cutting_garden`.

**Tech Stack:** Go 1.26, `futility` framework, dagnabit (re-export generation), `cli_main.Run` (CLI entrypoint), bats (integration tests), nix (build via `pkgs.buildGoApplication`).

**Rollback:** Net-new utility, no existing consumer to migrate. Revert the commit series; no dual-architecture period.

**Source design:** [`docs/plans/2026-05-03-hyphence-utility-design.md`](2026-05-03-hyphence-utility-design.md) (commit `260d31a`).

**Out of scope** (filed as follow-ups, do NOT implement here):
- Dodder convergence — [#126](https://github.com/amarbel-llc/madder/issues/126).
- `--all` mode for validate — [#127](https://github.com/amarbel-llc/madder/issues/127).
- Structured `! type@lock` / `- value < lock` parsing — [#128](https://github.com/amarbel-llc/madder/issues/128).
- Canonical-line-order tie-breaking spec in RFC 0001 — [#129](https://github.com/amarbel-llc/madder/issues/129).
- Cross-utility bats integration — [#130](https://github.com/amarbel-llc/madder/issues/130).

**Universal verification commands** (used at end of every task):
- Go tests: `just test-go`  *(carries the required `test` build tag; bare `go test ./...` produces spurious `undefined: ui.T` failures)*
- Go build: `just build-go`
- Full nix build: `just build` *(end-of-slice only — slow)*
- Bats integration: `just test-bats` *(end-of-slice only)*
- Status check: `git status --short` (verify no stray files; commit when expected)

---

## Slice 1 — Shared parser surface

Lands the `Document` data model, sentinel errors, `Canonicalize`, and the four `io.ReaderFrom` consumers in the existing `go/internal/charlie/hyphence/` package, with full unit-test coverage and RFC vector additions. After this slice, the public Go API for format-only document handling is complete; subcommands plug into it without further library changes.

### Task 1.1: Document data type and sentinel errors

**Promotion criteria:** N/A (additive).

**Files:**
- Create: `go/internal/charlie/hyphence/document.go`
- Create: `go/internal/charlie/hyphence/document_test.go`

**Step 1: Write the failing test**

Create `go/internal/charlie/hyphence/document_test.go`:

```go
package hyphence

import (
	"errors"
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
```

**Step 2: Run test to verify it fails**

Run: `just test-go`
Expected: FAIL with "undefined: Document" or similar — Document/MetadataLine/sentinels do not exist yet.

**Step 3: Write minimal implementation**

Create `go/internal/charlie/hyphence/document.go`:

```go
package hyphence

import "errors"

// Document is the parsed metadata section of a hyphence document. The
// body is never buffered into Document — body bytes are streamed by
// the Blob consumer attached to a Reader. Document is the format-only
// model used by `hyphence meta`, `hyphence format`, and the
// `hyphence validate` subcommand; the type-aware Coder/Reader
// machinery in this package is independent.
type Document struct {
	Metadata         []MetadataLine
	TrailingComments []string
	HasBody          bool
}

// MetadataLine is a single metadata line keyed by its single-character
// prefix. Per RFC 0001 §Metadata Lines, prefixes are one of '!', '@',
// '#', '-', '<', '%'. LeadingComments captures '%' lines that
// preceded this line in source order — comments are entangled with
// the next non-comment line per RFC, so reordering carries them
// along.
type MetadataLine struct {
	Prefix          byte
	Value           string
	LeadingComments []string
}

var (
	// ErrMalformedMetadataLine is returned when a line in the
	// metadata section does not match `<prefix> <content>` shape.
	ErrMalformedMetadataLine = errors.New("malformed metadata line")

	// ErrInvalidPrefix is returned when a metadata line's prefix is
	// not one of !@#-<%.
	ErrInvalidPrefix = errors.New("invalid metadata prefix")

	// ErrInlineBodyWithAtReference is returned when a document has
	// both an `@` blob-reference line in its metadata and a body
	// section after the closing boundary. RFC 0001 §Metadata Lines
	// says decoders SHOULD reject such documents.
	ErrInlineBodyWithAtReference = errors.New("blob reference '@' line forbidden when body section follows")
)
```

**Step 4: Run test to verify it passes**

Run: `just test-go`
Expected: PASS.

**Step 5: Regenerate dagnabit facade**

Run: `just generate-facades`
Expected: `go/pkgs/hyphence/main.go` is updated to re-export `Document`, `MetadataLine`, `ErrMalformedMetadataLine`, `ErrInvalidPrefix`, `ErrInlineBodyWithAtReference`.

Verify with: `git diff go/pkgs/hyphence/main.go` — should show added type aliases and var re-exports.

**Step 6: Commit**

```bash
git add go/internal/charlie/hyphence/document.go go/internal/charlie/hyphence/document_test.go go/pkgs/hyphence/main.go
git commit -m "feat(hyphence): add Document data type and sentinel errors

Format-only document model used by the new hyphence utility's meta,
body, format, and validate subcommands. Distinct from the type-aware
Coder/Reader machinery already in this package — Document does not
decode body payloads, only structures the metadata section.

:clown:"
```

---

### Task 1.2: MetadataStreamer consumer

**Files:**
- Modify: `go/internal/charlie/hyphence/document.go` (or new `consumers.go`)
- Modify: `go/internal/charlie/hyphence/document_test.go`

For consistency, put each consumer in its own file. Create `go/internal/charlie/hyphence/consumers.go`.

**Step 1: Write the failing test**

Append to `go/internal/charlie/hyphence/document_test.go`:

```go
import (
	"bytes"
	"strings"
)

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
```

**Step 2: Run test to verify it fails**

Run: `just test-go`
Expected: FAIL with "undefined: MetadataStreamer".

**Step 3: Write minimal implementation**

Create `go/internal/charlie/hyphence/consumers.go`:

```go
package hyphence

import (
	"io"

	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// MetadataStreamer is the metadata consumer for `hyphence meta`. It
// copies metadata bytes verbatim from the piped reader supplied by
// hyphence.Reader's metadata pipeline to W. No per-line validation
// happens here — `hyphence meta` is intentionally lenient; users who
// want strict checks run `hyphence validate` first.
type MetadataStreamer struct {
	W io.Writer
}

func (m *MetadataStreamer) ReadFrom(r io.Reader) (int64, error) {
	n, err := io.Copy(m.W, r)
	if err != nil {
		return n, errors.Wrap(err)
	}
	return n, nil
}
```

**Step 4: Run test to verify it passes**

Run: `just test-go`
Expected: PASS.

**Step 5: Regenerate facade**

Run: `just generate-facades`

**Step 6: Commit**

```bash
git add go/internal/charlie/hyphence/consumers.go go/internal/charlie/hyphence/document_test.go go/pkgs/hyphence/main.go
git commit -m "feat(hyphence): add MetadataStreamer consumer

Verbatim byte-copy from piped metadata reader to a writer. Used by
\`hyphence meta\` to print the metadata section unchanged.

:clown:"
```

---

### Task 1.3: MetadataBuilder consumer

**Files:**
- Modify: `go/internal/charlie/hyphence/consumers.go`
- Modify: `go/internal/charlie/hyphence/document_test.go`

**Step 1: Write the failing test**

Append to `document_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `just test-go`
Expected: FAIL with "undefined: MetadataBuilder".

**Step 3: Write minimal implementation**

Append to `consumers.go`:

```go
import (
	"bufio"
)

// MetadataBuilder is the metadata consumer for `hyphence format`. It
// parses each metadata line into a structured MetadataLine and
// appends to Doc.Metadata in source order. Comment lines (`%`) are
// buffered as LeadingComments on the next non-comment line, or
// TrailingComments if none follows. Malformed lines abort with
// ErrMalformedMetadataLine or ErrInvalidPrefix.
type MetadataBuilder struct {
	Doc *Document
}

func (m *MetadataBuilder) ReadFrom(r io.Reader) (int64, error) {
	br := bufio.NewReader(r)
	var n int64
	var pendingComments []string

	for {
		raw, err := br.ReadString('\n')
		n += int64(len(raw))
		if err != nil && err != io.EOF {
			return n, errors.Wrap(err)
		}

		line := raw
		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}

		if line == "" {
			if err == io.EOF {
				break
			}
			return n, errors.Errorf("%w: empty metadata line", ErrMalformedMetadataLine)
		}

		prefix := line[0]
		if !isValidPrefix(prefix) {
			return n, errors.Errorf("%w: %q", ErrInvalidPrefix, string(prefix))
		}

		if len(line) < 2 || line[1] != ' ' {
			return n, errors.Errorf("%w: missing space after prefix in %q", ErrMalformedMetadataLine, line)
		}

		value := line[2:]

		if prefix == '%' {
			pendingComments = append(pendingComments, value)
		} else {
			ml := MetadataLine{Prefix: prefix, Value: value}
			if len(pendingComments) > 0 {
				ml.LeadingComments = pendingComments
				pendingComments = nil
			}
			m.Doc.Metadata = append(m.Doc.Metadata, ml)
		}

		if err == io.EOF {
			break
		}
	}

	if len(pendingComments) > 0 {
		m.Doc.TrailingComments = pendingComments
	}

	return n, nil
}

func isValidPrefix(b byte) bool {
	switch b {
	case '!', '@', '#', '-', '<', '%':
		return true
	default:
		return false
	}
}
```

**Step 4: Run test to verify it passes**

Run: `just test-go`
Expected: PASS.

**Step 5: Regenerate facade and commit**

```bash
just generate-facades
git add go/internal/charlie/hyphence/consumers.go go/internal/charlie/hyphence/document_test.go go/pkgs/hyphence/main.go
git commit -m "feat(hyphence): add MetadataBuilder consumer

Structures each metadata line into a Document. Comment lines are
anchored as LeadingComments on the next non-comment line, or as
Document.TrailingComments if none follows. Malformed lines surface
ErrMalformedMetadataLine; non-RFC prefixes surface ErrInvalidPrefix.

:clown:"
```

---

### Task 1.4: MetadataValidator consumer

**Files:**
- Modify: `go/internal/charlie/hyphence/consumers.go`
- Modify: `go/internal/charlie/hyphence/document_test.go`

**Step 1: Write the failing test**

Append to `document_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `just test-go`
Expected: FAIL with "undefined: MetadataValidator".

**Step 3: Write minimal implementation**

Append to `consumers.go`:

```go
import (
	"strings"
)

// MetadataValidator is the metadata consumer for `hyphence validate`.
// It parses each metadata line strictly per RFC 0001 §Metadata Lines.
// Tracks SawAtLine for the post-ReadFrom inline-body-AND-@ cross-
// check the validate subcommand performs.
type MetadataValidator struct {
	SawAtLine bool

	line int // 1-based, internal
}

func (m *MetadataValidator) ReadFrom(r io.Reader) (int64, error) {
	br := bufio.NewReader(r)
	var n int64

	for {
		raw, err := br.ReadString('\n')
		n += int64(len(raw))
		if err != nil && err != io.EOF {
			return n, errors.Wrap(err)
		}

		m.line++

		line := raw
		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}

		if line == "" {
			if err == io.EOF {
				break
			}
			return n, errors.Errorf("line %d: %w: empty metadata line", m.line, ErrMalformedMetadataLine)
		}

		if strings.ContainsRune(line, '\r') {
			return n, errors.Errorf("line %d: %w: contains carriage return", m.line, ErrMalformedMetadataLine)
		}

		prefix := line[0]
		if !isValidPrefix(prefix) {
			return n, errors.Errorf("line %d: %w: %q (must be one of !@#-<%%)", m.line, ErrInvalidPrefix, string(prefix))
		}

		if len(line) < 2 || line[1] != ' ' {
			return n, errors.Errorf("line %d: %w: missing space after prefix", m.line, ErrMalformedMetadataLine)
		}

		if prefix == '@' {
			m.SawAtLine = true
		}

		if err == io.EOF {
			break
		}
	}

	return n, nil
}
```

**Step 4: Run test to verify it passes**

Run: `just test-go`
Expected: PASS.

**Step 5: Regenerate facade and commit**

```bash
just generate-facades
git add go/internal/charlie/hyphence/consumers.go go/internal/charlie/hyphence/document_test.go go/pkgs/hyphence/main.go
git commit -m "feat(hyphence): add MetadataValidator consumer

Strict RFC 0001 §Metadata Lines validator. Reports first violation
with a 1-based line number. Tracks SawAtLine for the validate
subcommand's post-ReadFrom inline-body-AND-@ cross-check.

:clown:"
```

---

### Task 1.5: Canonicalize and FormatBodyEmitter

**Files:**
- Create: `go/internal/charlie/hyphence/canonicalize.go`
- Modify: `go/internal/charlie/hyphence/consumers.go`
- Modify: `go/internal/charlie/hyphence/document_test.go`

**Step 1: Write the failing test for Canonicalize**

Append to `document_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `just test-go`
Expected: FAIL with "undefined: Canonicalize".

**Step 3: Write minimal implementation**

Create `go/internal/charlie/hyphence/canonicalize.go`:

```go
package hyphence

import "sort"

// canonicalOrder maps each metadata prefix to its sort rank per RFC
// 0001 §Canonical Line Order. Lower rank emits first. Comments ('%')
// don't appear here because Document captures them as anchored
// LeadingComments / TrailingComments rather than as standalone
// MetadataLine entries — Canonicalize only sorts non-comment lines.
//
// Locked vs aliased vs bare object reference distinction is not
// modeled today (see #128); all '<' lines share rank 1 and rely on
// stable sort to preserve source order.
var canonicalOrder = map[byte]int{
	'#': 0, // description
	'<': 1, // object reference
	'-': 2, // tag / reference
	'@': 3, // blob reference
	'!': 4, // type
}

// Canonicalize sorts doc.Metadata in place per RFC 0001 §Canonical
// Line Order. Stable sort within each prefix bucket preserves
// insertion order. Each MetadataLine carries its LeadingComments
// across the sort. TrailingComments remain at the document tail.
func Canonicalize(doc *Document) {
	sort.SliceStable(doc.Metadata, func(i, j int) bool {
		return canonicalOrder[doc.Metadata[i].Prefix] < canonicalOrder[doc.Metadata[j].Prefix]
	})
}
```

**Step 4: Run test to verify Canonicalize passes**

Run: `just test-go`
Expected: PASS.

**Step 5: Write the failing test for FormatBodyEmitter**

Append to `document_test.go`:

```go
func TestFormatBodyEmitter_EmitsCanonicalizedMetadataThenBody(t *testing.T) {
	doc := &Document{
		Metadata: []MetadataLine{
			{Prefix: '!', Value: "md"},
			{Prefix: '#', Value: "desc"},
		},
		HasBody: true,
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
}

func TestFormatBodyEmitter_NoBody(t *testing.T) {
	doc := &Document{
		Metadata: []MetadataLine{{Prefix: '!', Value: "md"}},
		HasBody:  false,
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
}

func TestFormatBodyEmitter_LeadingAndTrailingComments(t *testing.T) {
	doc := &Document{
		Metadata: []MetadataLine{
			{Prefix: '!', Value: "md", LeadingComments: []string{"about-type"}},
		},
		TrailingComments: []string{"end note"},
		HasBody:          false,
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
```

**Step 6: Run test to verify it fails**

Run: `just test-go`
Expected: FAIL with "undefined: FormatBodyEmitter".

**Step 7: Write minimal implementation**

Append to `consumers.go`:

```go
import (
	"fmt"
)

// FormatBodyEmitter is the Blob consumer for `hyphence format`. By
// the time ReadFrom fires, MetadataBuilder has populated Doc; this
// emits the canonicalized metadata section to Out, then streams the
// body bytes from r to Out.
type FormatBodyEmitter struct {
	Doc *Document
	Out io.Writer
}

func (e *FormatBodyEmitter) ReadFrom(r io.Reader) (int64, error) {
	Canonicalize(e.Doc)

	if _, err := fmt.Fprint(e.Out, "---\n"); err != nil {
		return 0, errors.Wrap(err)
	}
	for _, ml := range e.Doc.Metadata {
		for _, c := range ml.LeadingComments {
			if _, err := fmt.Fprintf(e.Out, "%% %s\n", c); err != nil {
				return 0, errors.Wrap(err)
			}
		}
		if _, err := fmt.Fprintf(e.Out, "%c %s\n", ml.Prefix, ml.Value); err != nil {
			return 0, errors.Wrap(err)
		}
	}
	for _, c := range e.Doc.TrailingComments {
		if _, err := fmt.Fprintf(e.Out, "%% %s\n", c); err != nil {
			return 0, errors.Wrap(err)
		}
	}
	if _, err := fmt.Fprint(e.Out, "---\n"); err != nil {
		return 0, errors.Wrap(err)
	}

	if !e.Doc.HasBody {
		// Drain r to release any underlying buffer; but no body
		// separator + body bytes are emitted when there's no body.
		_, _ = io.Copy(io.Discard, r)
		return 0, nil
	}

	if _, err := fmt.Fprint(e.Out, "\n"); err != nil {
		return 0, errors.Wrap(err)
	}

	n, err := io.Copy(e.Out, r)
	if err != nil {
		return n, errors.Wrap(err)
	}
	return n, nil
}
```

**Step 8: Run test to verify it passes**

Run: `just test-go`
Expected: PASS.

**Step 9: Regenerate facade and commit**

```bash
just generate-facades
git add go/internal/charlie/hyphence/canonicalize.go go/internal/charlie/hyphence/consumers.go go/internal/charlie/hyphence/document_test.go go/pkgs/hyphence/main.go
git commit -m "feat(hyphence): add Canonicalize and FormatBodyEmitter

Canonicalize sorts metadata lines per RFC 0001 §Canonical Line Order
with stable sort within each prefix bucket. FormatBodyEmitter is the
Blob consumer for the format subcommand: by the time ReadFrom fires,
MetadataBuilder has populated Doc; emits the canonicalized metadata
section then streams body bytes through.

:clown:"
```

---

### Task 1.6: RFC vector additions for new sentinel errors

**Files:**
- Modify: `go/internal/charlie/hyphence/testdata/rfc_vectors.txt`
- Read: `go/internal/charlie/hyphence/rfc_conformance_test.go`

**Step 1: Read the existing harness to understand the outcome vocabulary**

```bash
cat go/internal/charlie/hyphence/rfc_conformance_test.go
```

The existing harness recognizes outcomes `parse-ok` and `parse-error-missing-separator`. We need to add new outcomes for the new sentinel errors so the harness can match them.

**Step 2: Extend the harness to recognize new outcomes**

Modify `go/internal/charlie/hyphence/rfc_conformance_test.go` (read first to see exact structure; the change should be: add cases to the switch on `outcome` that map `parse-error-malformed-line`, `parse-error-invalid-prefix`, `parse-error-inline-body-with-at` to `errors.Is(err, ErrMalformedMetadataLine)` etc.).

If the existing harness only tests via the type-aware Coder/Reader (which doesn't surface the new sentinels because those are emitted by the new consumers), add a separate test in `document_test.go` driven from the same TSV file but invoked through `MetadataValidator` directly.

**Step 3: Add vectors to `testdata/rfc_vectors.txt`**

Append at the end:

```
# New vectors for the format-only document model (Task 1.6).
# These are scanned by MetadataValidator (see document_test.go).

invalid-prefix-X	LS0tCiEgbWQKWCBiYWQKLS0tCgo=	parse-error-invalid-prefix	-
malformed-no-space	LS0tCiFub3NwYWNlCi0tLQoK	parse-error-malformed-line	-
inline-body-with-at-ref	LS0tCkAgYmxha2UyYjI1Ni1hYmMKISBtZAotLS0KCmlubGluZQo=	parse-error-inline-body-with-at	-
```

The base64 strings encode:
- `invalid-prefix-X`: `---\n! md\nX bad\n---\n\n`
- `malformed-no-space`: `---\n!nospace\n---\n\n`
- `inline-body-with-at-ref`: `---\n@ blake2b256-abc\n! md\n---\n\ninline\n`

Verify each base64 with `printf '%s' '<base64>' | base64 -d` before committing.

**Step 4: Add the document-level conformance test**

Append to `document_test.go`:

```go
import (
	"bufio"
	"encoding/base64"
	"os"
)

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
	case "parse-error-invalid-prefix":
		v := &MetadataValidator{}
		reader := Reader{RequireMetadata: true, Metadata: v, Blob: discardReaderFrom{}}
		_, err := reader.ReadFrom(bytes.NewReader(input))
		if !errors.Is(err, ErrInvalidPrefix) {
			t.Errorf("expected ErrInvalidPrefix, got %v", err)
		}
	case "parse-error-malformed-line":
		v := &MetadataValidator{}
		reader := Reader{RequireMetadata: true, Metadata: v, Blob: discardReaderFrom{}}
		_, err := reader.ReadFrom(bytes.NewReader(input))
		if !errors.Is(err, ErrMalformedMetadataLine) {
			t.Errorf("expected ErrMalformedMetadataLine, got %v", err)
		}
	case "parse-error-inline-body-with-at":
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
		// outcomes unrelated to MetadataValidator are skipped here;
		// the legacy harness in rfc_conformance_test.go covers them.
		t.Skipf("outcome %q handled by legacy harness", outcome)
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
```

(`countingDiscard` lives here for now; it'll move to a non-test file in a later task when subcommand wiring needs it.)

**Step 5: Run tests to verify**

Run: `just test-go`
Expected: PASS — three new vectors pass under their `runDocumentVector` arms; legacy harness vectors (`minimal-no-body`, `body-with-separator`, `body-without-separator`) skip per the `default` arm.

**Step 6: Commit**

```bash
git add go/internal/charlie/hyphence/testdata/rfc_vectors.txt go/internal/charlie/hyphence/document_test.go
git commit -m "test(hyphence): add RFC vectors for new sentinel errors

Three new vectors exercise MetadataValidator's strict checks via the
existing testdata/rfc_vectors.txt format. Legacy harness in
rfc_conformance_test.go is unchanged.

:clown:"
```

---

### Task 1.7: Slice 1 verification

**Step 1: Full test run**

Run: `just test-go`
Expected: PASS, including all new tests in this slice.

**Step 2: Race detector run**

Run: `just test-go-race`
Expected: PASS.

**Step 3: Verify dagnabit facade is up to date**

Run: `just generate-facades`

Run: `git status --short`
Expected: clean tree (no changes from re-running `generate-facades`). If `pkgs/hyphence/main.go` shows changes, commit them.

**Step 4: Verify build**

Run: `just build-go`
Expected: PASS.

---

## Slice 2 — Utility scaffolding

Lands the empty `commands_hyphence` utility (no subcommands yet) and the `cmd/hyphence` binary entry point. Slice 1 must be merged or staged first because the subcommand files in Slice 3 will import from this package.

### Task 2.1: commands_hyphence package skeleton

**Files:**
- Create: `go/internal/india/commands_hyphence/main.go`
- Create: `go/internal/india/commands_hyphence/globals.go`
- Create: `go/internal/india/commands_hyphence/CLAUDE.md`

**Step 1: Create main.go**

```go
// go/internal/india/commands_hyphence/main.go
package commands_hyphence

import (
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/flags"
)

var utility = futility.NewUtility(
	"hyphence",
	"format-only inspection and re-emission of on-disk hyphence documents (RFC 0001)",
)

var globalFlags = &Globals{}

func init() {
	utility.GlobalFlags = globalFlags
	utility.GlobalParams = []futility.Param{
		futility.BoolFlag{
			Name:        "no-inventory-log",
			Description: "Suppress the per-blob audit inventory-log under $XDG_LOG_HOME/madder/inventory_log/. No-op for hyphence (which performs no blob writes); kept for cross-utility flag-set consistency.",
		},
	}
	utility.GlobalFlagDefiner = func(fs *flags.FlagSet) {
		fs.BoolVar(
			&globalFlags.NoInventoryLog,
			"no-inventory-log",
			false,
			"Suppress the per-blob audit inventory-log. No-op for hyphence.",
		)
	}

	utility.Examples = append(utility.Examples,
		futility.Example{
			Description: "Validate a capture-receipt file against RFC 0001.",
			Command:     "hyphence validate receipt.hyphence",
		},
		futility.Example{
			Description: "Print just the metadata section of a document.",
			Command:     "hyphence meta receipt.hyphence | grep '^!'",
		},
		futility.Example{
			Description: "Pipe the body of an inventory-log file through jq.",
			Command:     "hyphence body $XDG_LOG_HOME/madder/inventory_log/2026-05-03/log.hyphence | jq -r '.entry_path'",
		},
		futility.Example{
			Description: "Canonicalize an old document.",
			Command:     "hyphence format old.hyphence > canonical.hyphence",
		},
	)

	utility.Files = append(utility.Files,
		futility.FilePath{
			Path:        "<any hyphence document>",
			Description: "Plain-text RFC 0001 document on disk. hyphence reads from a file path or stdin (use '-' for stdin).",
		},
	)
}

func GetUtility() *futility.Utility {
	return utility
}
```

**Step 2: Create globals.go**

```go
// go/internal/india/commands_hyphence/globals.go
package commands_hyphence

// Globals carries hyphence's global flag values. The --no-inventory-log
// flag is mounted for cross-utility consistency but has no effect on
// hyphence's operation since hyphence performs no blob writes.
type Globals struct {
	NoInventoryLog bool
}

func (g *Globals) IsInventoryLogDisabled() bool {
	if g == nil {
		return false
	}
	return g.NoInventoryLog
}
```

**Step 3: Create CLAUDE.md**

```markdown
# commands_hyphence

`hyphence` CLI commands. The binary is a sibling of `madder`,
`madder-cache`, and `cutting-garden` — its own utility identity for
CLI purposes, with no blob-store integration. Format-only tooling
for on-disk hyphence documents per RFC 0001.

## Subcommands

- `validate`: strict RFC 0001 conformance check
- `meta`: print metadata section verbatim
- `body`: print body section verbatim
- `format`: re-emit canonicalized per RFC §Canonical Line Order

Wire format documented in `docs/rfcs/0001-hyphence.md` and
`docs/man.7/hyphence.md`. Library used: `go/internal/charlie/hyphence/`
(`Document`, `MetadataStreamer`, `MetadataBuilder`,
`MetadataValidator`, `FormatBodyEmitter`, `Canonicalize`).
```

**Step 4: Verify the package compiles**

Run: `just build-go`
Expected: PASS — package builds cleanly even with no subcommands registered.

**Step 5: Commit**

```bash
git add go/internal/india/commands_hyphence/
git commit -m "feat(hyphence-cli): scaffold commands_hyphence utility package

Empty utility with global flag set, examples, and Files metadata.
Subcommands land in subsequent commits. Mirrors commands_cache
shape.

:clown:"
```

---

### Task 2.2: cmd/hyphence entry point

**Files:**
- Create: `go/cmd/hyphence/main.go`

**Step 1: Create the entry point**

```go
// go/cmd/hyphence/main.go
package main

import (
	"github.com/amarbel-llc/madder/go/internal/0/buildinfo"
	"github.com/amarbel-llc/madder/go/internal/charlie/cli_main"
	_ "github.com/amarbel-llc/madder/go/internal/charlie/markl_registrations"
	"github.com/amarbel-llc/madder/go/internal/india/commands_hyphence"
)

// Populated at link time via `-X main.version` / `-X main.commit`.
var (
	version = "dev"
	commit  = "unknown"
)

func init() {
	buildinfo.Set(version, commit)
}

func main() {
	cli_main.Run(commands_hyphence.GetUtility(), "hyphence")
}
```

**Step 2: Verify the binary builds**

Run: `cd go && go build ./cmd/hyphence/`
Expected: PASS, binary produced (then deleted by go clean if you want — `rm hyphence`).

**Step 3: Hook into the nix build**

Modify `go/default.nix:88-93` (the `subPackages` array on the `madder` derivation) to add `"cmd/hyphence"`:

Before:
```nix
    subPackages = [
      "cmd/madder"
      "cmd/madder-cache"
      "cmd/madder-gen_man"
      "cmd/cutting-garden"
      "cmd/cg"
    ];
```

After:
```nix
    subPackages = [
      "cmd/madder"
      "cmd/madder-cache"
      "cmd/madder-gen_man"
      "cmd/cutting-garden"
      "cmd/cg"
      "cmd/hyphence"
    ];
```

**Step 4: Verify with nix**

Run: `just build`
Expected: PASS, `result/bin/hyphence` exists.

```bash
ls -la result/bin/hyphence
result/bin/hyphence --help 2>&1 | head -20
```

The `--help` output should show the four examples from `commands_hyphence/main.go` and a (currently empty) subcommand list.

**Step 5: Commit**

```bash
git add go/cmd/hyphence/main.go go/default.nix
git commit -m "feat(hyphence-cli): add cmd/hyphence entry point and nix subPackage

Thin entrypoint mirroring cmd/madder-cache and cmd/cutting-garden.
Wire the binary into go/default.nix subPackages so nix build emits
result/bin/hyphence alongside the other utilities.

:clown:"
```

---

### Task 2.3: Hook hyphence into man-page generation

**Files:**
- Modify: `go/cmd/madder-gen_man/main.go`

**Step 1: Add commands_hyphence to the utilities slice**

Modify `go/cmd/madder-gen_man/main.go:34-38`:

Before:
```go
	utilities := []*futility.Utility{
		commands.GetUtility(),
		commands_cache.GetUtility(),
		commands_cutting_garden.GetUtility(),
	}
```

After:
```go
	utilities := []*futility.Utility{
		commands.GetUtility(),
		commands_cache.GetUtility(),
		commands_cutting_garden.GetUtility(),
		commands_hyphence.GetUtility(),
	}
```

And add the import:

```go
import (
	// ... existing imports ...
	"github.com/amarbel-llc/madder/go/internal/india/commands_hyphence"
)
```

**Step 2: Verify man-page generation**

Run: `just build`
Expected: PASS. Inside `result/share/man/man1/`, there should now be `hyphence.1` (top-level utility man page). Per-subcommand pages will appear after Slice 3.

```bash
ls result/share/man/man1/hyphence*
man -l result/share/man/man1/hyphence.1
```

**Step 3: Commit**

```bash
git add go/cmd/madder-gen_man/main.go
git commit -m "feat(hyphence-cli): generate man pages from futility metadata

madder-gen_man iterates registered utilities; adding
commands_hyphence to the slice makes hyphence(1) regenerate
on every nix build.

:clown:"
```

---

### Task 2.4: Slice 2 verification

**Step 1: Full test run**

Run: `just test-go`
Expected: PASS.

**Step 2: Nix build**

Run: `just build`
Expected: PASS, `result/bin/hyphence` and `result/share/man/man1/hyphence.1` both present.

**Step 3: Smoke test the binary**

```bash
result/bin/hyphence --help
```

Should print usage including the four examples.

```bash
result/bin/hyphence
```

Should print help and exit 64 (no subcommand provided). Verify with `echo $?`.

```bash
result/bin/hyphence version
```

Should fail because no `version` subcommand has been registered — an acceptable outcome at this slice. (We deliberately don't add a `version` subcommand to keep the four-subcommand surface clean.)

---

## Slice 3 — Subcommands

Each subcommand is independent — they can land in any order once the slice 2 scaffolding is in. Standard structure: a single Go file in `commands_hyphence/`, an `init()` calling `utility.AddCmd`, a struct with `GetParams`, `GetDescription`, `SetFlagDefinitions`, `Run`, and a unit test in the same package using in-memory buffers.

A small **shared input helper** (Task 3.0) is added first so each subcommand can resolve `<path|->` consistently.

### Task 3.0: Shared input-source helper

**Files:**
- Create: `go/internal/india/commands_hyphence/input.go`
- Create: `go/internal/india/commands_hyphence/input_test.go`

**Step 1: Write the failing test**

```go
// go/internal/india/commands_hyphence/input_test.go
package commands_hyphence

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestOpenInput_FilePath(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "hyphence-input-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("hello"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	r, source, closer, err := OpenInput(f.Name(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closer.Close()
	if source != f.Name() {
		t.Errorf("source mismatch: got %q, want %q", source, f.Name())
	}
	got, _ := io.ReadAll(r)
	if string(got) != "hello" {
		t.Errorf("content mismatch: got %q, want %q", got, "hello")
	}
}

func TestOpenInput_Stdin(t *testing.T) {
	stdin := strings.NewReader("piped")
	r, source, closer, err := OpenInput("-", stdin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closer.Close()
	if source != "-" {
		t.Errorf("source for stdin should be '-', got %q", source)
	}
	got, _ := io.ReadAll(r)
	if string(got) != "piped" {
		t.Errorf("content mismatch: got %q, want %q", got, "piped")
	}
}

func TestOpenInput_FileNotFound(t *testing.T) {
	_, _, _, err := OpenInput("/nonexistent/path/xyz", nil)
	if err == nil {
		t.Errorf("expected error for nonexistent path, got nil")
	}
	var noInput *NoInputError
	if !errorsAs(err, &noInput) {
		t.Errorf("expected *NoInputError, got %T: %v", err, err)
	}
}

// errorsAs is a tiny shim to avoid importing the full errors package
// only for tests; in non-test code use errors.As.
func errorsAs(err error, target interface{}) bool {
	type asErr interface{ As(any) bool }
	if a, ok := err.(asErr); ok {
		return a.As(target)
	}
	if e := err; e != nil {
		// fallback for stdlib-style wrap chains
		for e != nil {
			if t, ok := target.(**NoInputError); ok {
				if v, ok := e.(*NoInputError); ok {
					*t = v
					return true
				}
			}
			type unwrapper interface{ Unwrap() error }
			u, _ := e.(unwrapper)
			if u == nil {
				break
			}
			e = u.Unwrap()
		}
	}
	return false
}

var _ = bytes.NewBuffer // silence unused
```

**Step 2: Run test to verify it fails**

Run: `just test-go`
Expected: FAIL — `OpenInput`, `NoInputError` undefined.

**Step 3: Write minimal implementation**

```go
// go/internal/india/commands_hyphence/input.go
package commands_hyphence

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// NoInputError wraps a file-open failure that should map to the
// CLI's EX_NOINPUT (66) exit code at the top level. The wrapped err
// preserves the os.PathError/os.IsNotExist semantics for callers
// that need to inspect them.
type NoInputError struct {
	Path string
	Err  error
}

func (e *NoInputError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path, e.Err)
}
func (e *NoInputError) Unwrap() error { return e.Err }
func (e *NoInputError) As(target any) bool {
	if t, ok := target.(**NoInputError); ok {
		*t = e
		return true
	}
	return false
}

// OpenInput resolves the positional <path|-> argument for every
// hyphence subcommand. When path is "-", the supplied stdin reader
// is used and source is reported as "-". When path is anything else,
// the file is opened; failure to open is wrapped in *NoInputError so
// the CLI maps it to EX_NOINPUT.
//
// The returned closer is always non-nil; callers should defer
// closer.Close().
func OpenInput(path string, stdin io.Reader) (io.Reader, string, io.Closer, error) {
	if path == "-" {
		if stdin == nil {
			return nil, "", io.NopCloser(nil), errors.New("stdin is nil")
		}
		return stdin, "-", io.NopCloser(stdin), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, path, io.NopCloser(nil), &NoInputError{Path: path, Err: err}
	}
	return f, path, f, nil
}
```

**Step 4: Run test to verify it passes**

Run: `just test-go`
Expected: PASS.

**Step 5: Commit**

```bash
git add go/internal/india/commands_hyphence/input.go go/internal/india/commands_hyphence/input_test.go
git commit -m "feat(hyphence-cli): shared OpenInput helper

Single resolver for the <path|-> positional that every subcommand
takes. NoInputError wraps file-open failures so the top-level CLI
can map them to EX_NOINPUT (66).

:clown:"
```

---

### Task 3.1: Validate subcommand

**Files:**
- Create: `go/internal/india/commands_hyphence/validate.go`
- Create: `go/internal/india/commands_hyphence/validate_test.go`

**Step 1: Write the failing test**

```go
// go/internal/india/commands_hyphence/validate_test.go
package commands_hyphence

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
)

func TestValidate_HappyPath(t *testing.T) {
	const input = "---\n! md\n---\n\nhello\n"
	if err := runValidate(strings.NewReader(input), "fixture.hyphence"); err != nil {
		t.Errorf("expected no error on valid input, got %v", err)
	}
}

func TestValidate_NoBody(t *testing.T) {
	const input = "---\n! md\n---\n"
	if err := runValidate(strings.NewReader(input), "fixture.hyphence"); err != nil {
		t.Errorf("expected no error on no-body document, got %v", err)
	}
}

func TestValidate_RejectsInlineBodyWithAt(t *testing.T) {
	const input = "---\n@ blake2b256-abc\n! md\n---\n\ninline\n"
	err := runValidate(strings.NewReader(input), "fixture.hyphence")
	if !errors.Is(err, hyphence.ErrInlineBodyWithAtReference) {
		t.Errorf("expected ErrInlineBodyWithAtReference, got %v", err)
	}
}

func TestValidate_RejectsInvalidPrefix(t *testing.T) {
	const input = "---\n! md\nX bad\n---\n"
	err := runValidate(strings.NewReader(input), "-")
	if !errors.Is(err, hyphence.ErrInvalidPrefix) {
		t.Errorf("expected ErrInvalidPrefix, got %v", err)
	}
}

func TestValidate_RejectsMissingBodySeparator(t *testing.T) {
	const input = "---\n! md\n---\nhello\n" // no blank line after closing ---
	err := runValidate(strings.NewReader(input), "fixture.hyphence")
	if err == nil {
		t.Errorf("expected error for missing body separator, got nil")
	}
}

// runValidate calls the same plumbing as Validate.Run but takes
// concrete I/O and returns the error directly. This is the
// in-memory-buffer test seam mentioned in the plan.
func runValidate(in *strings.Reader, source string) error {
	v := &hyphence.MetadataValidator{}
	body := &countingDiscard{}
	reader := hyphence.Reader{
		RequireMetadata: true,
		Metadata:        v,
		Blob:            body,
	}
	if _, err := reader.ReadFrom(in); err != nil {
		return err
	}
	if v.SawAtLine && body.SawBody {
		return hyphence.ErrInlineBodyWithAtReference
	}
	return nil
}

// countingDiscard mirrors the production helper added in the same task
// (kept here for now until the production file lands; once landed the
// helper file is the source and this duplicate is deleted).
type countingDiscard struct {
	SawBody bool
}

func (c *countingDiscard) ReadFrom(r interface {
	Read(p []byte) (int, error)
}) (int64, error) {
	var n int64
	buf := make([]byte, 4096)
	for {
		read, err := r.Read(buf)
		n += int64(read)
		if read > 0 {
			c.SawBody = true
		}
		if err != nil {
			return n, err
		}
	}
}

var _ = bytes.NewBuffer
```

(The duplicate `countingDiscard` here matches the one in Slice 1's `document_test.go`. When the validate.go helper file lands in Step 3, delete this duplicate and import from the production location.)

**Step 2: Run test to verify it fails**

Run: `just test-go`
Expected: FAIL — references to `runValidate` will compile, but the test plus `Validate.Run` itself doesn't exist.

**Step 3: Write minimal implementation**

```go
// go/internal/india/commands_hyphence/validate.go
package commands_hyphence

import (
	"fmt"
	"io"
	"os"

	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("validate", &Validate{})
}

type Validate struct{}

var (
	_ futility.CommandWithParams = (*Validate)(nil)
)

func (cmd *Validate) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "path",
			Description: "path to a hyphence document, or '-' for stdin",
			Required:    true,
		},
	}
}

func (cmd Validate) GetDescription() futility.Description {
	return futility.Description{
		Short: "strict RFC 0001 conformance check",
		Long: "Read a hyphence document and verify it conforms to RFC " +
			"0001. Exits 0 silent on pass; exits 65 with one line- " +
			"numbered diagnostic on stderr on the first violation. " +
			"Validate also enforces the inline-body-AND-@ rule (RFC " +
			"0001 §Metadata Lines): a document MUST NOT carry both an " +
			"@ blob-reference line and a body section.",
	}
}

func (cmd *Validate) SetFlagDefinitions(interfaces.CLIFlagDefinitions) {}

func (cmd Validate) Run(req futility.Request) {
	path := req.PopArg("path")
	req.AssertNoMoreArgs()

	in, source, closer, err := OpenInput(path, os.Stdin)
	if err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
		return
	}
	defer closer.Close()

	v := &hyphence.MetadataValidator{}
	body := &CountingDiscardReaderFrom{}
	reader := hyphence.Reader{
		RequireMetadata: true,
		Metadata:        v,
		Blob:            body,
	}

	if _, err := reader.ReadFrom(in); err != nil {
		fmt.Fprintf(os.Stderr, "hyphence: validate: %s: %s\n", source, err)
		errors.ContextCancelWithBadRequestError(req, err)
		return
	}

	if v.SawAtLine && body.SawBody {
		fmt.Fprintf(os.Stderr, "hyphence: validate: %s: %s\n", source, hyphence.ErrInlineBodyWithAtReference)
		errors.ContextCancelWithBadRequestError(req, hyphence.ErrInlineBodyWithAtReference)
		return
	}
}

// CountingDiscardReaderFrom is the Blob consumer for validate, meta,
// and (if needed) any subcommand that wants to drain the body
// section without preserving it. SawBody is true after ReadFrom if
// at least one byte followed the body separator.
type CountingDiscardReaderFrom struct {
	SawBody bool
}

func (c *CountingDiscardReaderFrom) ReadFrom(r io.Reader) (int64, error) {
	n, err := io.Copy(io.Discard, r)
	if n > 0 {
		c.SawBody = true
	}
	return n, err
}
```

After this lands, **delete the duplicate `countingDiscard` in validate_test.go** and switch the test to import `*CountingDiscardReaderFrom` from this file. Re-run `just test-go` to confirm.

**Step 4: Run test to verify it passes**

Run: `just test-go`
Expected: PASS.

**Step 5: Smoke test against the real binary**

Run: `just build`

```bash
echo -e '---\n! md\n---\n\nhello\n' | result/bin/hyphence validate -
echo $?  # expect 0

echo -e '---\nX bad\n---\n' | result/bin/hyphence validate -
echo $?  # expect non-zero (CLI top-level decides exact code; 65 is the design target)
```

If exit code is something other than 65 for the failure case, that's the top-level `cli_main.Run` mapping — record it and revisit in Task 3.5 (post-slice CLI exit-code reconciliation, if needed).

**Step 6: Commit**

```bash
git add go/internal/india/commands_hyphence/validate.go go/internal/india/commands_hyphence/validate_test.go
git commit -m "feat(hyphence-cli): validate subcommand

Reads a hyphence document and verifies it conforms to RFC 0001.
Strict mode (no AllowMissingSeparator). Cross-line check for the
inline-body-AND-@ rule runs after Reader.ReadFrom returns.

:clown:"
```

---

### Task 3.2: Meta subcommand

**Files:**
- Create: `go/internal/india/commands_hyphence/meta.go`
- Create: `go/internal/india/commands_hyphence/meta_test.go`

**Step 1: Write the failing test**

```go
// go/internal/india/commands_hyphence/meta_test.go
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
```

**Step 2: Run test to verify it fails**

Run: `just test-go`
Expected: FAIL — `runMeta` plumbing references `hyphence.MetadataStreamer` (already exists) but the production `Meta` command doesn't.

**Step 3: Write minimal implementation**

```go
// go/internal/india/commands_hyphence/meta.go
package commands_hyphence

import (
	"fmt"
	"os"

	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("meta", &Meta{})
}

type Meta struct{}

var _ futility.CommandWithParams = (*Meta)(nil)

func (cmd *Meta) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "path",
			Description: "path to a hyphence document, or '-' for stdin",
			Required:    true,
		},
	}
}

func (cmd Meta) GetDescription() futility.Description {
	return futility.Description{
		Short: "print metadata section verbatim",
		Long: "Read a hyphence document and print the metadata section " +
			"to stdout, with the surrounding `---` boundaries " +
			"stripped. No per-line validation runs — malformed prefixes " +
			"are printed through. Boundary-level errors (missing closing " +
			"`---`, missing body separator) still abort. Run `hyphence " +
			"validate` first if strict checks matter.",
	}
}

func (cmd *Meta) SetFlagDefinitions(interfaces.CLIFlagDefinitions) {}

func (cmd Meta) Run(req futility.Request) {
	path := req.PopArg("path")
	req.AssertNoMoreArgs()

	in, source, closer, err := OpenInput(path, os.Stdin)
	if err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
		return
	}
	defer closer.Close()

	streamer := &hyphence.MetadataStreamer{W: os.Stdout}
	body := &CountingDiscardReaderFrom{}
	reader := hyphence.Reader{
		RequireMetadata: true,
		Metadata:        streamer,
		Blob:            body,
	}

	if _, err := reader.ReadFrom(in); err != nil {
		fmt.Fprintf(os.Stderr, "hyphence: meta: %s: %s\n", source, err)
		errors.ContextCancelWithBadRequestError(req, err)
		return
	}
}
```

**Step 4: Run test to verify it passes**

Run: `just test-go`
Expected: PASS.

**Step 5: Smoke test**

Run: `just build`

```bash
printf '%s' '---
# desc
! md
---

body
' | result/bin/hyphence meta -
```

Expected output:
```
# desc
! md
```

**Step 6: Commit**

```bash
git add go/internal/india/commands_hyphence/meta.go go/internal/india/commands_hyphence/meta_test.go
git commit -m "feat(hyphence-cli): meta subcommand

Prints the metadata section verbatim with --- boundaries stripped.
Lenient by design: malformed prefixes pass through, but boundary-
level errors still abort.

:clown:"
```

---

### Task 3.3: Body subcommand

**Files:**
- Create: `go/internal/india/commands_hyphence/body.go`
- Create: `go/internal/india/commands_hyphence/body_test.go`

**Step 1: Write the failing test**

```go
// go/internal/india/commands_hyphence/body_test.go
package commands_hyphence

import (
	"bytes"
	"io"
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

type writerReaderFrom struct{ W io.Writer }

func (w *writerReaderFrom) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(w.W, r)
}
```

**Step 2: Run test to verify it fails**

Run: `just test-go`
Expected: FAIL.

**Step 3: Write minimal implementation**

```go
// go/internal/india/commands_hyphence/body.go
package commands_hyphence

import (
	"fmt"
	"io"
	"os"

	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("body", &Body{})
}

type Body struct{}

var _ futility.CommandWithParams = (*Body)(nil)

func (cmd *Body) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "path",
			Description: "path to a hyphence document, or '-' for stdin",
			Required:    true,
		},
	}
}

func (cmd Body) GetDescription() futility.Description {
	return futility.Description{
		Short: "print body section verbatim",
		Long: "Read a hyphence document and stream its body section " +
			"(the bytes after the closing --- and the body separator) " +
			"to stdout. If the document has no body, prints nothing and " +
			"exits 0.",
	}
}

func (cmd *Body) SetFlagDefinitions(interfaces.CLIFlagDefinitions) {}

func (cmd Body) Run(req futility.Request) {
	path := req.PopArg("path")
	req.AssertNoMoreArgs()

	in, source, closer, err := OpenInput(path, os.Stdin)
	if err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
		return
	}
	defer closer.Close()

	body := &writerReaderFrom{W: os.Stdout}
	reader := hyphence.Reader{
		RequireMetadata: true,
		Metadata:        &CountingDiscardReaderFrom{},
		Blob:            body,
	}

	if _, err := reader.ReadFrom(in); err != nil {
		fmt.Fprintf(os.Stderr, "hyphence: body: %s: %s\n", source, err)
		errors.ContextCancelWithBadRequestError(req, err)
		return
	}
}

// writerReaderFrom is the Blob consumer for the body subcommand:
// stream bytes from r straight to W. Lives next to Body.Run so the
// body command's wiring is self-contained.
type writerReaderFrom struct{ W io.Writer }

func (w *writerReaderFrom) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(w.W, r)
}
```

(The duplicate `writerReaderFrom` in body_test.go can stay or be removed — production and test versions are identical and don't share a file. Pick one in the next clean-up task; for v1, leaving them separate is fine.)

**Step 4: Run test to verify it passes**

Run: `just test-go`
Expected: PASS.

**Step 5: Smoke test**

Run: `just build`

```bash
printf '%s' '---
! md
---

body bytes
' | result/bin/hyphence body -
```

Expected output:
```
body bytes
```

**Step 6: Commit**

```bash
git add go/internal/india/commands_hyphence/body.go go/internal/india/commands_hyphence/body_test.go
git commit -m "feat(hyphence-cli): body subcommand

Streams the body section to stdout. No-body documents produce no
output and exit 0.

:clown:"
```

---

### Task 3.4: Format subcommand

**Files:**
- Create: `go/internal/india/commands_hyphence/format.go`
- Create: `go/internal/india/commands_hyphence/format_test.go`

**Step 1: Write the failing test**

```go
// go/internal/india/commands_hyphence/format_test.go
package commands_hyphence

import (
	"bytes"
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
)

func TestFormat_Canonicalizes(t *testing.T) {
	// `! md` arrived first in source; `# desc` should sort first
	// after Canonicalize.
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
	if err == nil {
		// Ensure HasBody is set correctly before emitter.ReadFrom
		// is called by the Reader pipeline. Reader doesn't expose
		// "did body bytes follow"; we infer it from the emitter's
		// view of the body Reader. For tests this is implicit in
		// the fact that emitter ran at all.
	}
	return err
}
```

**Step 2: Run test to verify it fails**

Run: `just test-go`
Expected: FAIL — `Format.Run` doesn't exist yet, but more importantly: the `runFormat` helper has a subtle bug. The `FormatBodyEmitter` needs `Doc.HasBody` set correctly **before** it emits. We'll need to detect "did Reader hand us body bytes?" inside the emitter.

Actually re-read `FormatBodyEmitter.ReadFrom` (Slice 1, Task 1.5): it's invoked by the Reader pipeline only after metadata parsing is complete. We never set `Doc.HasBody` from inside MetadataBuilder. We need to either (a) make MetadataBuilder set `Doc.HasBody = true` whenever the boundary scanner advances past the body separator, or (b) have FormatBodyEmitter detect body presence by reading at least one byte from r before deciding.

(a) is cleaner but requires plumbing the boundary scanner's state into the metadata consumer, which it doesn't have.

(b) is achievable: change FormatBodyEmitter to peek the first byte before emitting the metadata block — but that's awkward because the metadata block is emitted *first*.

**Resolution:** use the Reader's behavior. Looking at `hyphence.Reader.readMetadataFrom`, after parsing the metadata it positions the underlying reader at the body. The Reader then calls `Blob.ReadFrom(bufio.NewReader(r))`. So `FormatBodyEmitter.ReadFrom` is only ever called when the metadata section closed cleanly — but it might be called with an empty body too. We need to detect "did body bytes arrive?" via a peek.

**Revised FormatBodyEmitter** (replaces the Task 1.5 version):

```go
func (e *FormatBodyEmitter) ReadFrom(r io.Reader) (int64, error) {
	Canonicalize(e.Doc)

	// Peek at the body to determine HasBody. If r delivers any
	// byte, HasBody is true; otherwise it's false.
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	_, peekErr := br.Peek(1)
	hasBody := peekErr == nil
	e.Doc.HasBody = hasBody

	if _, err := fmt.Fprint(e.Out, "---\n"); err != nil {
		return 0, errors.Wrap(err)
	}
	for _, ml := range e.Doc.Metadata {
		for _, c := range ml.LeadingComments {
			if _, err := fmt.Fprintf(e.Out, "%% %s\n", c); err != nil {
				return 0, errors.Wrap(err)
			}
		}
		if _, err := fmt.Fprintf(e.Out, "%c %s\n", ml.Prefix, ml.Value); err != nil {
			return 0, errors.Wrap(err)
		}
	}
	for _, c := range e.Doc.TrailingComments {
		if _, err := fmt.Fprintf(e.Out, "%% %s\n", c); err != nil {
			return 0, errors.Wrap(err)
		}
	}
	if _, err := fmt.Fprint(e.Out, "---\n"); err != nil {
		return 0, errors.Wrap(err)
	}

	if !hasBody {
		return 0, nil
	}

	if _, err := fmt.Fprint(e.Out, "\n"); err != nil {
		return 0, errors.Wrap(err)
	}
	n, err := io.Copy(e.Out, br)
	if err != nil {
		return n, errors.Wrap(err)
	}
	return n, nil
}
```

**Step 3: Update FormatBodyEmitter implementation in `go/internal/charlie/hyphence/consumers.go` per the revised version above.**

Update the corresponding tests in `document_test.go` from Task 1.5: pass an explicit body source rather than relying on `Doc.HasBody` being pre-set. The existing two tests should now look like:

```go
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
```

Run: `just test-go`
Expected: PASS for the updated Slice 1 tests AND the new `format_test.go` tests.

**Step 4: Write minimal Format subcommand**

```go
// go/internal/india/commands_hyphence/format.go
package commands_hyphence

import (
	"fmt"
	"os"

	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("format", &Format{})
}

type Format struct{}

var _ futility.CommandWithParams = (*Format)(nil)

func (cmd *Format) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "path",
			Description: "path to a hyphence document, or '-' for stdin",
			Required:    true,
		},
	}
}

func (cmd Format) GetDescription() futility.Description {
	return futility.Description{
		Short: "re-emit canonicalized per RFC §Canonical Line Order",
		Long: "Read a hyphence document and re-emit it with metadata " +
			"lines sorted per RFC 0001 §Canonical Line Order: " +
			"description (#) → object references (<) → tags (-) → blob " +
			"reference (@) → type (!). Within each prefix, source " +
			"order is preserved. Comments (%) stay anchored to their " +
			"following non-comment line. Body bytes pass through " +
			"unchanged.",
	}
}

func (cmd *Format) SetFlagDefinitions(interfaces.CLIFlagDefinitions) {}

func (cmd Format) Run(req futility.Request) {
	path := req.PopArg("path")
	req.AssertNoMoreArgs()

	in, source, closer, err := OpenInput(path, os.Stdin)
	if err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
		return
	}
	defer closer.Close()

	doc := &hyphence.Document{}
	builder := &hyphence.MetadataBuilder{Doc: doc}
	emitter := &hyphence.FormatBodyEmitter{Doc: doc, Out: os.Stdout}
	reader := hyphence.Reader{
		RequireMetadata: true,
		Metadata:        builder,
		Blob:            emitter,
	}

	if _, err := reader.ReadFrom(in); err != nil {
		fmt.Fprintf(os.Stderr, "hyphence: format: %s: %s\n", source, err)
		errors.ContextCancelWithBadRequestError(req, err)
		return
	}
}
```

**Step 5: Run tests to verify**

Run: `just test-go`
Expected: PASS.

**Step 6: Smoke test**

Run: `just build`

```bash
printf '%s' '---
! md
# desc
---

body
' | result/bin/hyphence format -
```

Expected output:
```
---
# desc
! md
---

body
```

Idempotence:
```bash
printf '%s' '---
! md
# desc
---

body
' | result/bin/hyphence format - | result/bin/hyphence format -
```

Should match the previous output byte-for-byte.

**Step 7: Commit**

```bash
git add go/internal/india/commands_hyphence/format.go go/internal/india/commands_hyphence/format_test.go go/internal/charlie/hyphence/consumers.go go/internal/charlie/hyphence/document_test.go
git commit -m "feat(hyphence-cli): format subcommand + FormatBodyEmitter HasBody peek

Format re-emits the document with canonicalized metadata. Body bytes
stream through unchanged. FormatBodyEmitter now peeks the body
reader to set Doc.HasBody, removing the requirement that callers
pre-populate it.

:clown:"
```

---

### Task 3.5: Slice 3 verification

**Step 1: Full test run**

Run: `just test-go`
Expected: PASS.

**Step 2: Race detector**

Run: `just test-go-race`
Expected: PASS.

**Step 3: Nix build + smoke tests**

Run: `just build`

```bash
# All four subcommands work end-to-end:
echo -e '---\n! md\n---\n' | result/bin/hyphence validate -
echo -e '---\n! md\n# desc\n---\n' | result/bin/hyphence meta -
echo -e '---\n! md\n---\n\nhello\n' | result/bin/hyphence body -
echo -e '---\n! md\n# desc\n---\n\nhello\n' | result/bin/hyphence format -

# Helper output:
result/bin/hyphence --help
```

**Step 4: Verify per-subcommand man pages exist**

```bash
ls result/share/man/man1/hyphence-*.1
```

Expected: `hyphence-validate.1`, `hyphence-meta.1`, `hyphence-body.1`, `hyphence-format.1`. (`madder-gen_man` automatically generates per-subcommand pages from the `Description.Long` strings.)

---

## Slice 4 — Bats integration

### Task 4.1: hyphence.bats

**Files:**
- Create: `zz-tests_bats/hyphence.bats`

**Step 1: Write the bats file**

```bash
# zz-tests_bats/hyphence.bats
setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output

  HYPHENCE_BIN="$(dirname "${MADDER_BIN:-madder}")/hyphence"
  if [[ ! -x $HYPHENCE_BIN ]]; then
    skip "hyphence binary not found at $HYPHENCE_BIN"
  fi
}

# bats file_tags=hyphence

run_hyphence() {
  run timeout --preserve-status 2s "$HYPHENCE_BIN" "$@"
}

# --- validate -----------------------------------------------------

function validate_accepts_valid_document { # @test
  local f="$BATS_TEST_TMPDIR/valid.hyphence"
  printf -- '---\n! md\n---\n\nhello\n' > "$f"
  run_hyphence validate "$f"
  assert_success
  assert_output ''
}

function validate_accepts_no_body { # @test
  local f="$BATS_TEST_TMPDIR/no-body.hyphence"
  printf -- '---\n! md\n---\n' > "$f"
  run_hyphence validate "$f"
  assert_success
  assert_output ''
}

function validate_rejects_invalid_prefix { # @test
  local f="$BATS_TEST_TMPDIR/bad-prefix.hyphence"
  printf -- '---\n! md\nX bad\n---\n' > "$f"
  run_hyphence validate "$f"
  refute [ "$status" -eq 0 ]
  assert_output --partial 'invalid metadata prefix'
}

function validate_rejects_inline_body_with_at { # @test
  local f="$BATS_TEST_TMPDIR/inline-and-at.hyphence"
  printf -- '---\n@ blake2b256-abc\n! md\n---\n\nbody\n' > "$f"
  run_hyphence validate "$f"
  refute [ "$status" -eq 0 ]
  assert_output --partial "blob reference '@' line forbidden"
}

function validate_reads_stdin_with_dash { # @test
  printf -- '---\n! md\n---\n' | run timeout --preserve-status 2s "$HYPHENCE_BIN" validate -
  assert_success
}

function validate_reports_missing_file { # @test
  run_hyphence validate /nonexistent/path/xyz.hyphence
  refute [ "$status" -eq 0 ]
}

# --- meta ---------------------------------------------------------

function meta_strips_boundaries { # @test
  local f="$BATS_TEST_TMPDIR/m.hyphence"
  printf -- '---\n# desc\n! md\n---\n\nignored\n' > "$f"
  run_hyphence meta "$f"
  assert_success
  assert_output "$(printf '# desc\n! md')"
}

function meta_handles_no_body { # @test
  local f="$BATS_TEST_TMPDIR/m.hyphence"
  printf -- '---\n! md\n---\n' > "$f"
  run_hyphence meta "$f"
  assert_success
  assert_output '! md'
}

# --- body ---------------------------------------------------------

function body_streams_body_bytes { # @test
  local f="$BATS_TEST_TMPDIR/b.hyphence"
  printf -- '---\n! md\n---\n\nhello world\n' > "$f"
  run_hyphence body "$f"
  assert_success
  assert_output 'hello world'
}

function body_empty_when_no_body_section { # @test
  local f="$BATS_TEST_TMPDIR/b.hyphence"
  printf -- '---\n! md\n---\n' > "$f"
  run_hyphence body "$f"
  assert_success
  assert_output ''
}

# --- format -------------------------------------------------------

function format_canonicalizes { # @test
  local f="$BATS_TEST_TMPDIR/f.hyphence"
  printf -- '---\n! md\n# desc\n---\n\nbody\n' > "$f"
  run_hyphence format "$f"
  assert_success
  assert_output "$(printf -- '---\n# desc\n! md\n---\n\nbody')"
}

function format_is_idempotent { # @test
  local f="$BATS_TEST_TMPDIR/f.hyphence"
  printf -- '---\n! md\n# desc\n- tag\n---\n\nbody\n' > "$f"
  local out1 out2
  out1="$("$HYPHENCE_BIN" format "$f")"
  out2="$(printf '%s' "$out1" | "$HYPHENCE_BIN" format -)"
  [[ $out1 == "$out2" ]] || fail "format is not idempotent:\nfirst:  $out1\nsecond: $out2"
}

# --- exit-code policy --------------------------------------------

function bare_invocation_prints_help_nonzero { # @test
  run_hyphence
  refute [ "$status" -eq 0 ]
  assert_output --partial 'hyphence'
}

function unknown_subcommand_fails { # @test
  run_hyphence frobnicate
  refute [ "$status" -eq 0 ]
}
```

**Step 2: Run the bats suite**

Run: `just build`
Run: `just test-bats`

Expected: PASS (all `hyphence.bats` cases green; existing bats files unchanged).

**Step 3: Verify nix-driven bats lane picks up the new file**

The `batsLaneOutputs` block in `go/default.nix` auto-discovers tags from `# bats file_tags=` directives. Our file declares `# bats file_tags=hyphence`, so the build should now emit a `bats-hyphence` derivation.

```bash
nix flake show 2>&1 | grep -i hyphence
```

Expected: `bats-hyphence` package listed.

**Step 4: Commit**

```bash
git add zz-tests_bats/hyphence.bats
git commit -m "test(hyphence-cli): bats integration for all four subcommands

Covers validate happy/sad paths (incl. inline-body-AND-@ rejection),
meta verbatim output, body streaming, format canonicalization +
idempotence, stdin via -, and bare-invocation/unknown-subcommand
exit codes. Auto-picked up as bats-hyphence lane via the file_tags
mechanism in go/default.nix.

:clown:"
```

---

### Task 4.2: Slice 4 verification

**Step 1: Full bats run, all lanes**

Run: `just test-bats`
Expected: PASS.

**Step 2: Race lane**

Run: `nix build .#bats-race`
Expected: PASS (re-runs the suite under `-race`).

**Step 3: Confirm coverage**

Run: `git log --oneline crisp-larch ^crisp-larch~10` (or however many commits this work touches).
Expected: list of incremental commits matching the task structure above.

---

## Final verification

Before declaring the work done:

1. `just test-go` — PASS
2. `just test-go-race` — PASS
3. `just test-bats` — PASS
4. `just build` — PASS, `result/bin/hyphence` exists, all four subcommands present in `result/share/man/man1/`.
5. `git status --short` — clean tree, no stray files.
6. `git log --oneline` shows a clean series of feat/test commits, each landable as its own PR if needed.
7. Manual smoke test against a real artifact:
   ```bash
   # Pick a real capture-receipt or inventory-log file from the user's
   # ~/.local/share/madder/blob_stores/<storeid>/objects/ tree.
   real_doc="$(find ~/.local/log/madder/inventory_log -name '*.hyphence' | head -1)"
   if [[ -n $real_doc ]]; then
     result/bin/hyphence validate "$real_doc"
     result/bin/hyphence meta "$real_doc"
     result/bin/hyphence body "$real_doc" | head -1
     result/bin/hyphence format "$real_doc" | diff - <(result/bin/hyphence format <(result/bin/hyphence format "$real_doc"))
   fi
   ```
   The diff should be empty (idempotence). Validate, meta, body should all exit 0.

If all of these pass, the utility is ready. Open a follow-up tracking ticket if any of #126–#130 turn out to be relevant blockers in practice; otherwise leave them as triage items.
