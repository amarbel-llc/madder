package main

import (
	"context"
	"fmt"
	"os"

	"github.com/amarbel-llc/madder/go/internal/india/commands"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ui"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/command"
)

func main() {
	utility := commands.GetUtility()

	if err := utility.RunCLI(
		context.Background(),
		os.Args[1:],
		command.StubPrompter{},
	); err != nil {
		fmt.Fprint(os.Stderr, "madder: ")

		if _, encErr := ui.CLIErrorTreeEncoder.EncodeTo(err, os.Stderr); encErr != nil {
			// Encoder should never fail writing to a real stderr, but
			// if it does we still need *some* signal — drop back to the
			// root Error() so the user isn't staring at silence.
			fmt.Fprintln(os.Stderr, err.Error())
		}

		os.Exit(1)
	}
}
