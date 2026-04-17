package commands

import (
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/command"
)

var utility = command.NewUtility("madder", "content-addressable blob store operations")

func GetUtility() *command.Utility {
	return utility
}
