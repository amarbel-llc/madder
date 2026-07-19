package main

import (
	"code.linenisgreat.com/madder/go/internal/0/buildinfo"
	"code.linenisgreat.com/madder/go/internal/charlie/cli_main"
	_ "code.linenisgreat.com/madder/go/internal/charlie/markl_registrations"
	"code.linenisgreat.com/madder/go/internal/india/commands_mcp"
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
	cli_main.Run(commands_mcp.GetUtility(), "madder-mcp")
}
