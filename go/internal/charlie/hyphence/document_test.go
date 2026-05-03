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
