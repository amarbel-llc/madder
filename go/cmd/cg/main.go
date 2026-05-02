package main

import (
	"github.com/amarbel-llc/madder/go/internal/0/buildinfo"
	"github.com/amarbel-llc/madder/go/internal/charlie/cli_main"
	_ "github.com/amarbel-llc/madder/go/internal/charlie/markl_registrations"
	"github.com/amarbel-llc/madder/go/internal/india/commands_cutting_garden"
)

// Populated at link time via `-X main.version` / `-X main.commit`.
var (
	version = "dev"
	commit  = "unknown"
)

func init() {
	buildinfo.Set(version, commit)
}

// cg is the short-name alias of cutting-garden. It runs the same
// utility; only the error prefix passed to cli_main.Run differs. The
// help banner / man pages come from utility.Name ("cutting-garden")
// today — see TODO note about banner unification.
func main() {
	cli_main.Run(commands_cutting_garden.GetUtility(), "cg")
}
