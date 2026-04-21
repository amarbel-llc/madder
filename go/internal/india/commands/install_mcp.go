package commands

import (
	"github.com/amarbel-llc/madder/go/internal/futility"
	gomcp_command "github.com/amarbel-llc/purse-first/libs/go-mcp/command"
)

func init() {
	utility.AddCmd("install-mcp", &InstallMcp{})
}

type InstallMcp struct{}

var _ futility.CommandWithParams = (*InstallMcp)(nil)

func (cmd *InstallMcp) GetParams() []futility.Param { return nil }

func (cmd InstallMcp) GetDescription() futility.Description {
	return futility.Description{
		Short: "install MCP server configuration",
	}
}

func (cmd InstallMcp) Run(req futility.Request) {
	app := gomcp_command.NewApp("madder", "Blob store MCP server")
	app.MCPArgs = []string{"mcp"}

	if err := app.InstallMCP(); err != nil {
		req.Cancel(err)
	}
}
