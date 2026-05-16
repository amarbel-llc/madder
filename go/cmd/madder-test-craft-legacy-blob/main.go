// Package main is the test-only fixture binary used by bats to
// materialize legacy-shaped blob bytes deterministically. It
// takes a -compression name, optional -encryption (none|age) and
// -recipient key path, and writes the encoded bytes to -out.
// Invoked by the bats suite for sftp-analyze-and-suggest-
// configs to construct stores that look like the
// pre-blob_store-config era.
//
// Not shipped to end users.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/bravo/plugins"
	_ "github.com/amarbel-llc/madder/go/internal/bravo/plugins/builtins"
	_ "github.com/amarbel-llc/madder/go/internal/charlie/markl_registrations"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_io"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/delta/files"
)

func main() {
	if err := runMain(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// runMain parses argv-style arguments and dispatches to run.
// Split out so unit tests can exercise the parsing + crafting
// path without re-execing the binary.
func runMain(argv []string) error {
	fs := flag.NewFlagSet("madder-test-craft-legacy-blob", flag.ContinueOnError)
	var (
		comp    = fs.String("compression", "none", "none|gzip|zlib|zstd")
		enc     = fs.String("encryption", "none", "none|age")
		recip   = fs.String("recipient", "", "path to age private key (used as encryption identity)")
		content = fs.String("content", "-", "source file or '-' for stdin")
		out     = fs.String("out", "", "destination path (required)")
	)
	if err := fs.Parse(argv); err != nil {
		return err
	}

	if *out == "" {
		return fmt.Errorf("must pass -out <path>")
	}

	return run(*comp, *enc, *recip, *content, *out, os.Stdin)
}

// run encodes content from contentPath (or `-` for stdin) into
// outPath, applying the named compression and (optional) age
// encryption.
//
// The recipient key, when -encryption=age, is loaded as a markl
// private key (the markl wrapper handles both encrypt and decrypt
// directions through the same Id). Tests share one keypair
// between this binary and the verifier in sftp_probe; the
// production sftp-analyze command takes -key flags pointing at
// the same kind of file.
func run(comp, enc, recip, contentPath, outPath string, stdin io.Reader) (err error) {
	var src io.Reader = stdin
	if contentPath != "-" {
		var f *os.File
		if f, err = os.Open(contentPath); err != nil {
			return err
		}
		defer files.CloseReadOnly(f)
		src = f
	}

	var dst *os.File
	if dst, err = os.Create(outPath); err != nil {
		return err
	}
	defer errors.DeferredCloser(&err, dst)

	var cfg blob_io.Config
	if cfg, err = makeIOConfig(comp, enc, recip); err != nil {
		return err
	}

	w, werr := blob_io.NewWriter(cfg, dst)
	if werr != nil {
		return werr
	}
	if _, err = io.Copy(w, src); err != nil {
		return err
	}
	// Assign w.Close()'s result to the named return so the deferred
	// errors.DeferredCloser on dst joins (rather than masks) it.
	// `return w.Close()` would be functionally equivalent (Go's
	// spec orders return-expression assignment before defers, and
	// DeferredCloser uses errors.Join), but the explicit form keeps
	// the join visible to the reader.
	err = w.Close()
	return err
}

// makeIOConfig translates the CLI flags into a blob_io.Config that
// matches the encoding the legacy bats fixtures need.
func makeIOConfig(comp, enc, recip string) (blob_io.Config, error) {
	ref, err := plugins.LegacyCompressionRef(comp)
	if err != nil {
		return blob_io.Config{}, fmt.Errorf("LegacyCompressionRef(%q): %w", comp, err)
	}
	wrapper, err := plugins.Resolve(ref)
	if err != nil {
		return blob_io.Config{}, fmt.Errorf("plugins.Resolve(%q): %w", ref, err)
	}

	var encId domain_interfaces.MarklId
	switch enc {
	case "none", "":
		// nothing
	case "age":
		if recip == "" {
			return blob_io.Config{}, fmt.Errorf("-encryption=age requires -recipient")
		}
		var key markl.Id
		if err := markl.SetFromPath(&key, recip); err != nil {
			return blob_io.Config{}, fmt.Errorf("loading recipient %q: %w", recip, err)
		}
		encId = &key
	default:
		return blob_io.Config{}, fmt.Errorf("unsupported -encryption %q", enc)
	}

	return blob_io.MakeConfig(
		blob_store_configs.DefaultHashType,
		nil,
		wrapper,
		encId,
	), nil
}
