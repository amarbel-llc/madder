package main

import (
	"context"
	"fmt"
	"os"

	"github.com/amarbel-llc/madder/go/internal/india/commands_madder"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/command"
)

func main() {
	utility := commands_madder.GetUtility()

	if err := utility.RunCLI(context.Background(), os.Args[1:], command.StubPrompter{}); err != nil {
		fmt.Fprintf(os.Stderr, "madder: %s\n", err)
		os.Exit(1)
	}
}
