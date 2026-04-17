package commands

import (
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/command"
)

func init() {
	utility.AddCmd("list", &List{})
}

type List struct {
	command_components.EnvBlobStore
}

var (
	_ interfaces.CommandComponentWriter = (*List)(nil)
	_ command.CommandWithParams         = (*List)(nil)
)

func (cmd *List) GetParams() []command.Param { return nil }

func (cmd List) GetDescription() command.Description {
	return command.Description{
		Short: "list configured blob stores",
		Long: "List all blob stores configured for the current repository, " +
			"showing each store's ID and description. Store IDs use prefixes " +
			"to indicate scope: unprefixed for XDG user stores, '.' for " +
			"CWD-relative stores, and '/' for XDG system stores.",
	}
}

func (cmd *List) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
}

func (cmd List) Run(req command.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	blobStores := envBlobStore.GetBlobStores()

	for _, blobStore := range blobStores {
		envBlobStore.GetUI().Printf(
			"%s: %s",
			blobStore.Path.GetId(),
			blobStore.GetBlobStoreDescription(),
		)
	}
}
