package main

import (
	"github.com/amarbel-llc/madder/go/internal/charlie/cli_main"
	"github.com/amarbel-llc/madder/go/internal/india/commands_cache"
)

func main() {
	cli_main.Run(commands_cache.GetUtility(), "madder-cache")
}
