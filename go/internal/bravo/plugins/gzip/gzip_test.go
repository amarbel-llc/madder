package gzip

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"code.linenisgreat.com/madder/go/internal/bravo/plugins"
)

func TestGzip_RoundTrip(t *testing.T) {
	w, err := plugins.Resolve("madder-codec-gzip-v1@gzip")
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
		t.Fatal("encoded length zero — gzip writer didn't run")
	}
	// gzip magic bytes
	if !bytes.HasPrefix(encoded.Bytes(), []byte{0x1f, 0x8b}) {
		t.Errorf("encoded bytes missing gzip magic: %x", encoded.Bytes()[:2])
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
