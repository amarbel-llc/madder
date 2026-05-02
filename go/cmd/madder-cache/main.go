package main

import (
	"github.com/amarbel-llc/madder/go/internal/0/buildinfo"
	"github.com/amarbel-llc/madder/go/internal/charlie/cli_main"
	_ "github.com/amarbel-llc/madder/go/internal/charlie/markl_registrations"
	"github.com/amarbel-llc/madder/go/internal/india/commands_cache"
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
	cli_main.Run(commands_cache.GetUtility(), "madder-cache")
}
