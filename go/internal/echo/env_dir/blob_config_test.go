//go:build test

package env_dir

import (
	"testing"

	"github.com/amarbel-llc/purse-first/libs/dewey/delta/compression_type"
)

func TestHasIdentityWrappers_Default(t *testing.T) {
	if !DefaultConfig.HasIdentityWrappers() {
		t.Fatal("DefaultConfig should have identity wrappers")
	}
}

func TestHasIdentityWrappers_Zstd(t *testing.T) {
	zstd := compression_type.CompressionTypeZstd
	cfg := DefaultConfig
	cfg.compression = &zstd
	if cfg.HasIdentityWrappers() {
		t.Fatal("zstd compression must not be identity")
	}
}

func TestHasIdentityWrappers_Gzip(t *testing.T) {
	gzip := compression_type.CompressionTypeGzip
	cfg := DefaultConfig
	cfg.compression = &gzip
	if cfg.HasIdentityWrappers() {
		t.Fatal("gzip compression must not be identity")
	}
}
