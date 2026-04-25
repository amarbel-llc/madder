// Package main is the test-only SFTP server described in RFC 0001.
// Normally invoked by bats helpers via MADDER_PLUGIN_COOKIE; refuses
// to start without the envelope so accidental direct invocation on a
// shared machine fails loudly.
package main

import (
	"fmt"
	"os"
)

const programName = "madder-test-sftp-server"

func main() {
	if os.Getenv("MADDER_PLUGIN_COOKIE") == "" {
		fmt.Fprintf(os.Stderr, "[%s] magic cookie mismatch\n", programName)
		os.Exit(1)
	}

	// Remainder lands in later tasks. For now an empty cookie check
	// is enough to satisfy TestCookieMismatchExitsOne.
	os.Exit(0)
}
