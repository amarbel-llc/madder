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

	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	_, peekErr := br.Peek(1)
	e.Doc.HasBody = peekErr == nil

	bw := bufio.NewWriter(e.Out)
	var n int64

	write := func(s string) error {
		nn, err := io.WriteString(bw, s)
		n += int64(nn)
		return err
	}
	writef := func(format string, args ...any) error {
		nn, err := fmt.Fprintf(bw, format, args...)
		n += int64(nn)
		return err
	}

	if err := write("---\n"); err != nil {
		return n, errors.Wrap(err)
	}
	for _, ml := range e.Doc.Metadata {
		for _, c := range ml.LeadingComments {
			if err := writef("%% %s\n", c); err != nil {
				return n, errors.Wrap(err)
			}
		}
		if err := writef("%c %s\n", ml.Prefix, ml.Value); err != nil {
			return n, errors.Wrap(err)
		}
	}
	for _, c := range e.Doc.TrailingComments {
		if err := writef("%% %s\n", c); err != nil {
			return n, errors.Wrap(err)
		}
	}
	if err := write("---\n"); err != nil {
		return n, errors.Wrap(err)
	}

	if !e.Doc.HasBody {
		if err := bw.Flush(); err != nil {
			return n, errors.Wrap(err)
		}
		return n, nil
	}

	if err := write("\n"); err != nil {
		return n, errors.Wrap(err)
	}
	if err := bw.Flush(); err != nil {
		return n, errors.Wrap(err)
	}

	bodyN, err := io.Copy(e.Out, br)
	n += bodyN
	if err != nil {
		return n, errors.Wrap(err)
	}
	return n, nil
}

// CountingDiscardReaderFrom is the Blob consumer for validate, meta,
// and any subcommand that wants to drain the body section without
// preserving it. SawBody is true after ReadFrom if at least one byte
// followed the body separator.
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

// BodyStreamer is the Blob consumer for `hyphence body` and any
// caller that wants to stream the body section verbatim to a
// writer. Mirrors MetadataStreamer for the metadata side.
type BodyStreamer struct {
	W io.Writer
}

func (b *BodyStreamer) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(b.W, r)
}
