//go:build test

package mmap_blob

import (
	"errors"
	"testing"
)

func TestErrMmapUnsupported_IsSentinel(t *testing.T) {
	if ErrMmapUnsupported == nil {
		t.Fatal("ErrMmapUnsupported is nil")
	}
	wrapped := errors.Join(ErrMmapUnsupported, errors.New("ctx"))
	if !errors.Is(wrapped, ErrMmapUnsupported) {
		t.Fatal("errors.Is should match")
	}
}

func TestErrDigestMismatch_IsSentinel(t *testing.T) {
	if ErrDigestMismatch == nil {
		t.Fatal("ErrDigestMismatch is nil")
	}
}
