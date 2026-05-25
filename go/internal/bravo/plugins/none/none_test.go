package none

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/bravo/plugins"
)

func TestNone_RoundTrip(t *testing.T) {
	w, err := plugins.Resolve("madder-codec-none-v1@none")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if w == nil {
		t.Fatal("plugin instance is nil")
	}

	const input = "hello world"
	var encoded bytes.Buffer
	wc, err := w.WrapWriter(&encoded)
	if err != nil {
		t.Fatalf("WrapWriter: %v", err)
	}
	if _, err := io.WriteString(wc, input); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if got := encoded.String(); got != input {
		t.Errorf("none should be identity; encoded = %q, want %q", got, input)
	}

	rc, err := w.WrapReader(strings.NewReader(input))
	if err != nil {
		t.Fatalf("WrapReader: %v", err)
	}
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != input {
		t.Errorf("none should be identity; decoded = %q, want %q", got, input)
	}
}

func TestNone_IsIdentity_MatchesResolved(t *testing.T) {
	// IsIdentity is the contract Slice 5 will use to replace
	// env_dir's compression_type type-assertion. Pin that the
	// instance returned via the registry IS recognized as the
	// identity wrapper.
	w, err := plugins.Resolve("madder-codec-none-v1@none")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !IsIdentity(w) {
		t.Errorf("resolved plugin should be the identity wrapper")
	}
}

func TestNone_IsIdentity_RejectsForeign(t *testing.T) {
	// A non-none wrapper should not match. Use the existing factory
	// to produce a wrapper of a different type via a stub.
	other := stubNonIdentity{}
	if IsIdentity(other) {
		t.Errorf("foreign wrapper should not be identity")
	}
}

type stubNonIdentity struct{}

func (stubNonIdentity) WrapWriter(w io.Writer) (io.WriteCloser, error) {
	return nil, nil
}

func (stubNonIdentity) WrapReader(r io.Reader) (io.ReadCloser, error) {
	return nil, nil
}
