// Package cli_main shares the entry-point shape between madder and
// madder-cache (and any future sibling). It invokes the utility's
// RunCLI, renders returned errors via ui.CLIErrorTreeEncoder so the
// full wrapped chain is visible (not just the root, which is what
// ErrorTree.Error() returns by default), and exits the process.
package cli_main

import (
	"context"
	"fmt"
	"os"

	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ui"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/command"
)

// Run invokes utility.RunCLI with os.Args, writes any error to stderr
// as a prefixed tree, and exits 0 on success or 1 on error. name is
// the stderr prefix shown before the tree — typically the binary name.
func Run(utility *command.Utility, name string) {
	err := utility.RunCLI(context.Background(), os.Args[1:], command.StubPrompter{})
	if err == nil {
		return
	}

	fmt.Fprintf(os.Stderr, "%s: ", name)

	// Fallback on encoder failure: a single-line Error() beats silence
	// when stderr itself is the problem.
	if _, encErr := ui.CLIErrorTreeEncoder.EncodeTo(err, os.Stderr); encErr != nil {
		fmt.Fprintln(os.Stderr, err.Error())
	}

	os.Exit(1)
}
