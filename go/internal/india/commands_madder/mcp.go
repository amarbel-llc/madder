package commands_madder

import (
	"github.com/amarbel-llc/madder/go/internal/hotel/mcp_madder"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/command"
)

func init() {
	utility.AddCmd("mcp", &Mcp{})
}

type Mcp struct{}

var _ command.CommandWithParams = (*Mcp)(nil)

func (cmd *Mcp) GetParams() []command.Param { return nil }

func (cmd Mcp) GetDescription() command.Description {
	return command.Description{
		Short: "start the MCP server",
	}
}

func (cmd Mcp) Run(req command.Request) {
	if err := mcp_madder.RunServer(req.Utility); err != nil {
		req.Cancel(err)
	}
}
