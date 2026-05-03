package zlib

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/bravo/plugins"
)

func TestZlib_RoundTrip(t *testing.T) {
	w, err := plugins.Resolve("madder-codec-zlib-v1@zlib")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	const input = "the quick brown fox jumps over the lazy dog"
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
	if encoded.Len() == 0 {
		t.Fatal("encoded length zero — zlib writer didn't run")
	}
	// zlib's two-byte header at default compression level.
	if !bytes.HasPrefix(encoded.Bytes(), []byte{0x78, 0x9c}) {
		t.Errorf("encoded bytes missing zlib magic: %x", encoded.Bytes()[:2])
	}

	rc, err := w.WrapReader(strings.NewReader(encoded.String()))
	if err != nil {
		t.Fatalf("WrapReader: %v", err)
	}
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != input {
		t.Errorf("decode mismatch:\n got: %q\nwant: %q", got, input)
	}
}
