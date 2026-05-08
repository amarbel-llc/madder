// Package main is the test-only fixture binary used by bats to
// materialize legacy-shaped blob bytes deterministically. It
// takes a -compression name, optional -encryption (none|age) and
// -recipient public-key path, and writes the encoded bytes to
// -out. Invoked by the bats suite for sftp-analyze-and-suggest-
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
		recip   = fs.String("recipient", "", "age recipient pubkey path if -encryption=age")
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
// encryption. Phase B.1 is passthrough; Phase B.2 wires the
// codec layers.
func run(comp, enc, recip, contentPath, outPath string, stdin io.Reader) error {
	var src io.Reader = stdin
	if contentPath != "-" {
		f, err := os.Open(contentPath)
		if err != nil {
			return err
		}
		defer f.Close()
		src = f
	}

	dst, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	// Phase B.2 will replace this passthrough with a
	// compression+encryption pipeline.
	_, err = io.Copy(dst, src)
	if err != nil {
		return err
	}

	_ = comp
	_ = enc
	_ = recip
	return nil
}
