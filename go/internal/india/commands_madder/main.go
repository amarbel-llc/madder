package commands_madder

import (
	"github.com/amarbel-llc/madder/go/internal/golf/command"
	"github.com/amarbel-llc/purse-first/libs/dewey/foxtrot/config_cli"
)

var utility = command.MakeUtility("madder", config_cli.Default())

func GetUtility() command.Utility {
	return utility
}
