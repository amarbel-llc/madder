package main

import (
	"fmt"
	"os"

	_ "github.com/amarbel-llc/madder/go/internal/charlie/markl_registrations"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/india/commands"
	"github.com/amarbel-llc/madder/go/internal/india/commands_cache"
	"github.com/amarbel-llc/madder/go/internal/india/commands_cutting_garden"
)

// Declared so the shared -X main.version / -X main.commit ldflags have
// matching symbols in this binary. madder-gen_man is a build-time tool
// and doesn't surface either value, but the ldflags list is shared
// across all subPackages.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	_ = version
	_ = commit

	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: madder-gen_man <output-dir>\n")
		os.Exit(1)
	}

	outputDir := os.Args[1]

	utilities := []*futility.Utility{
		commands.GetUtility(),
		commands_cache.GetUtility(),
		commands_cutting_garden.GetUtility(),
	}

	for _, utility := range utilities {
		if err := utility.GenerateManpages(outputDir); err != nil {
			fmt.Fprintf(os.Stderr, "madder-gen_man: %s\n", err)
			os.Exit(1)
		}
	}
}
