package hyphence

import (
	"bufio"
	"fmt"
	"io"
	"strings"

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

		if strings.ContainsRune(line, '\r') {
			return n, errors.Errorf("%w: contains carriage return", ErrMalformedMetadataLine)
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

	bw := bufio.NewWriter(e.Out)

	if _, err := io.WriteString(bw, "---\n"); err != nil {
		return 0, errors.Wrap(err)
	}
	for _, ml := range e.Doc.Metadata {
		for _, c := range ml.LeadingComments {
			if _, err := fmt.Fprintf(bw, "%% %s\n", c); err != nil {
				return 0, errors.Wrap(err)
			}
		}
		if _, err := fmt.Fprintf(bw, "%c %s\n", ml.Prefix, ml.Value); err != nil {
			return 0, errors.Wrap(err)
		}
	}
	for _, c := range e.Doc.TrailingComments {
		if _, err := fmt.Fprintf(bw, "%% %s\n", c); err != nil {
			return 0, errors.Wrap(err)
		}
	}
	if _, err := io.WriteString(bw, "---\n"); err != nil {
		return 0, errors.Wrap(err)
	}

	// TODO(slice-3): FormatBodyEmitter is being redesigned in
	// Task 3.4 to peek the body reader and set Doc.HasBody itself.
	// Today the flag is only set by tests; production callers
	// (validate/format subcommands) don't set it.
	if !e.Doc.HasBody {
		// Drain r so the upstream Reader pipeline reaches EOF
		// cleanly even when the document has no body section.
		if err := bw.Flush(); err != nil {
			return 0, errors.Wrap(err)
		}
		_, _ = io.Copy(io.Discard, r)
		return 0, nil
	}

	if _, err := io.WriteString(bw, "\n"); err != nil {
		return 0, errors.Wrap(err)
	}
	if err := bw.Flush(); err != nil {
		return 0, errors.Wrap(err)
	}

	n, err := io.Copy(e.Out, r)
	if err != nil {
		return n, errors.Wrap(err)
	}
	return n, nil
}
