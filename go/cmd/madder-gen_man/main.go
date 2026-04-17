package main

import (
	"fmt"
	"os"

	"github.com/amarbel-llc/madder/go/internal/india/commands"
	"github.com/amarbel-llc/madder/go/internal/india/commands_cache"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/command"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: madder-gen_man <output-dir>\n")
		os.Exit(1)
	}

	outputDir := os.Args[1]

	utilities := []*command.Utility{
		commands.GetUtility(),
		commands_cache.GetUtility(),
	}

	for _, utility := range utilities {
		utility.Version = "0.0.1"

		if err := utility.GenerateManpages(outputDir); err != nil {
			fmt.Fprintf(os.Stderr, "madder-gen_man: %s\n", err)
			os.Exit(1)
		}
	}
}
