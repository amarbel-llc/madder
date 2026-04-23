package main

import (
	"github.com/amarbel-llc/madder/go/internal/0/buildinfo"
	"github.com/amarbel-llc/madder/go/internal/charlie/cli_main"
	"github.com/amarbel-llc/madder/go/internal/india/commands_cache"
)

var version = "dev"
var commit  = "unknown"

func init() {
	buildinfo.Set(version, commit)
}

func main() {
	cli_main.Run(commands_cache.GetUtility(), "madder-cache")
}
