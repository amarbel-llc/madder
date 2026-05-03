package hyphence

import (
	"bufio"
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
