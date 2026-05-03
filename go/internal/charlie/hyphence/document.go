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
