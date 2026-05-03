package commands_hyphence

import (
	"errors"
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
)

func TestValidate_HappyPath(t *testing.T) {
	const input = "---\n! md\n---\n\nhello\n"
	if err := runValidate(strings.NewReader(input)); err != nil {
		t.Errorf("expected no error on valid input, got %v", err)
	}
}

func TestValidate_NoBody(t *testing.T) {
	const input = "---\n! md\n---\n"
	if err := runValidate(strings.NewReader(input)); err != nil {
		t.Errorf("expected no error on no-body document, got %v", err)
	}
}

func TestValidate_RejectsInlineBodyWithAt(t *testing.T) {
	const input = "---\n@ blake2b256-abc\n! md\n---\n\ninline\n"
	err := runValidate(strings.NewReader(input))
	if !errors.Is(err, hyphence.ErrInlineBodyWithAtReference) {
		t.Errorf("expected ErrInlineBodyWithAtReference, got %v", err)
	}
}

func TestValidate_RejectsInvalidPrefix(t *testing.T) {
	const input = "---\n! md\nX bad\n---\n"
	err := runValidate(strings.NewReader(input))
	if !errors.Is(err, hyphence.ErrInvalidPrefix) {
		t.Errorf("expected ErrInvalidPrefix, got %v", err)
	}
}

func TestValidate_RejectsMissingBodySeparator(t *testing.T) {
	const input = "---\n! md\n---\nhello\n" // no blank line after closing ---
	err := runValidate(strings.NewReader(input))
	if err == nil {
		t.Errorf("expected error for missing body separator, got nil")
	}
}

// runValidate exercises the same Reader/consumer wiring as Validate.Run
// but takes a concrete reader and returns the error directly. The CLI
// wrapper (Validate.Run) handles printing the diagnostic and the
// futility-level cancellation; the validation logic itself is what we
// test here.
func runValidate(in *strings.Reader) error {
	v := &hyphence.MetadataValidator{}
	body := &CountingDiscardReaderFrom{}
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
