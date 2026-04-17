package main

import (
	"fmt"
	"os"

	"github.com/amarbel-llc/madder/go/internal/india/commands"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: madder-gen_man <output-dir>\n")
		os.Exit(1)
	}

	outputDir := os.Args[1]

	utility := commands.GetUtility()
	utility.Version = "0.0.1"

	if err := utility.GenerateManpages(outputDir); err != nil {
		fmt.Fprintf(os.Stderr, "madder-gen_man: %s\n", err)
		os.Exit(1)
	}
}
