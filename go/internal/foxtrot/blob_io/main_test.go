//go:build test

package blob_io

import (
	"testing"

	"github.com/amarbel-llc/madder/go/internal/bravo/plugins"
	_ "github.com/amarbel-llc/madder/go/internal/bravo/plugins/builtins"
)

func TestHasIdentityWrappers_Default(t *testing.T) {
	if !DefaultConfig.HasIdentityWrappers() {
		t.Fatal("DefaultConfig should have identity wrappers")
	}
}

func TestHasIdentityWrappers_Zstd(t *testing.T) {
	zstd, err := plugins.Resolve("madder-codec-zstd-v1@zstd")
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig
	cfg.compression = zstd
	if cfg.HasIdentityWrappers() {
		t.Fatal("zstd compression must not be identity")
	}
}

func TestHasIdentityWrappers_Gzip(t *testing.T) {
	gzip, err := plugins.Resolve("madder-codec-gzip-v1@gzip")
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig
	cfg.compression = gzip
	if cfg.HasIdentityWrappers() {
		t.Fatal("gzip compression must not be identity")
	}
}
