package commands

import (
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/command"
)

var utility = command.NewUtility("madder", "content-addressable blob store operations")

func init() {
	utility.EnvVars = append(utility.EnvVars, command.EnvVar{
		Name: "MADDER_CEILING_DIRECTORIES",
		Description: "Colon-separated list of absolute directories above which " +
			"madder will not walk when searching the current working directory " +
			"upward for a .madder override directory. Mirrors " +
			"GIT_CEILING_DIRECTORIES; useful for isolating test runs so madder " +
			"does not inherit configuration from ancestor directories.",
	})
}

func GetUtility() *command.Utility {
	return utility
}
