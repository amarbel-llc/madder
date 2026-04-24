package main

import (
	"github.com/amarbel-llc/madder/go/internal/0/buildinfo"
	"github.com/amarbel-llc/madder/go/internal/charlie/cli_main"
	"github.com/amarbel-llc/madder/go/internal/india/commands"
)

// Populated at link time via `-X main.version` / `-X main.commit`.
var (
	version = "dev"
	commit  = "unknown"
)

func init() {
	buildinfo.Set(version, commit)
}

func main() {
	cli_main.Run(commands.GetUtility(), "madder")
}
