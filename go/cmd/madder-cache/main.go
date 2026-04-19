package main

import (
	"context"
	"fmt"
	"os"

	"github.com/amarbel-llc/madder/go/internal/india/commands_cache"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ui"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/command"
)

func main() {
	utility := commands_cache.GetUtility()

	if err := utility.RunCLI(
		context.Background(),
		os.Args[1:],
		command.StubPrompter{},
	); err != nil {
		fmt.Fprint(os.Stderr, "madder-cache: ")

		if _, encErr := ui.CLIErrorTreeEncoder.EncodeTo(err, os.Stderr); encErr != nil {
			fmt.Fprintln(os.Stderr, err.Error())
		}

		os.Exit(1)
	}
}
