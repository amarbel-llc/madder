package plugins_test

import (
	"errors"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/bravo/plugins"
	_ "github.com/amarbel-llc/madder/go/internal/bravo/plugins/builtins"
)

func TestLegacyCompressionRef(t *testing.T) {
	cases := []struct {
		legacy string
		want   string
	}{
		{"", "madder-codec-none-v1@none"},
		{"none", "madder-codec-none-v1@none"},
		{"gzip", "madder-codec-gzip-v1@gzip"},
		{"zlib", "madder-codec-zlib-v1@zlib"},
		{"zstd", "madder-codec-zstd-v1@zstd"},
	}
	for _, tc := range cases {
		got, err := plugins.LegacyCompressionRef(tc.legacy)
		if err != nil {
			t.Errorf("legacy %q: unexpected error %v", tc.legacy, err)
			continue
		}
		if got != tc.want {
			t.Errorf("legacy %q: got %q, want %q", tc.legacy, got, tc.want)
		}
	}
}

func TestLegacyCompressionRef_Unknown(t *testing.T) {
	_, err := plugins.LegacyCompressionRef("brotli")
	if !errors.Is(err, plugins.ErrUnknownLegacyCompression) {
		t.Errorf("expected ErrUnknownLegacyCompression, got %v", err)
	}
}

func TestLegacyCompression_ResolvesViaDefault(t *testing.T) {
	for _, legacy := range []string{"", "none", "gzip", "zlib", "zstd"} {
		ref, err := plugins.LegacyCompressionRef(legacy)
		if err != nil {
			t.Fatalf("ref %q: %v", legacy, err)
		}
		if _, err := plugins.Resolve(ref); err != nil {
			t.Errorf("legacy %q -> %q failed Resolve: %v", legacy, ref, err)
		}
	}
}
