package commands

import (
	"github.com/amarbel-llc/madder/go/internal/0/mcp"
	"github.com/amarbel-llc/madder/go/internal/futility"
)

func init() {
	utility.AddCmd("mcp", &Mcp{})
}

type Mcp struct{}

var _ futility.CommandWithParams = (*Mcp)(nil)

func (cmd *Mcp) GetParams() []futility.Param { return nil }

func (cmd Mcp) GetDescription() futility.Description {
	return futility.Description{
		Short: "start the MCP server",
	}
}

func (cmd Mcp) Run(req futility.Request) {
	if err := mcp.RunServer(req.Utility); err != nil {
		req.Cancel(err)
	}
}
