package main

import (
	"fmt"
	"os"

	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/india/commands"
	"github.com/amarbel-llc/madder/go/internal/india/commands_cache"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: madder-gen_man <output-dir>\n")
		os.Exit(1)
	}

	outputDir := os.Args[1]

	utilities := []*futility.Utility{
		commands.GetUtility(),
		commands_cache.GetUtility(),
	}

	for _, utility := range utilities {
		if err := utility.GenerateManpages(outputDir); err != nil {
			fmt.Fprintf(os.Stderr, "madder-gen_man: %s\n", err)
			os.Exit(1)
		}
	}
}
