package commands_cache

import (
	"os"
	"path/filepath"

	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
)

func init() {
	utility.AddCmd("list", &List{})
}

type List struct {
	command_components.EnvBlobStore
}

var (
	_ interfaces.CommandComponentWriter = (*List)(nil)
	_ futility.CommandWithParams        = (*List)(nil)
)

func (cmd *List) GetParams() []futility.Param { return nil }

func (cmd List) GetDescription() futility.Description {
	return futility.Description{
		Short: "list configured cache blob stores",
		Long: "List all cache blob stores configured for the current " +
			"environment, showing each store's ID, description, and the " +
			"location of its on-disk config file. Each line ends with " +
			"'# path: <rel>' where <rel> is the config-file path expressed " +
			"relative to the current working directory (falling back to " +
			"absolute when the relative form cannot be computed).",
	}
}

func (cmd *List) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
}

func (cmd List) Run(req futility.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	blobStores := envBlobStore.GetBlobStores()

	cwd, _ := os.Getwd()

	for _, blobStore := range blobStores {
		envBlobStore.GetUI().Printf(
			"%s: %s # path: %s",
			blobStore.Path.GetId(),
			blobStore.GetBlobStoreDescription(),
			relOrAbs(cwd, blobStore.Path.GetConfig()),
		)
	}
}

// relOrAbs returns abs expressed relative to cwd, or abs unchanged if
// cwd is empty or filepath.Rel rejects the inputs.
func relOrAbs(cwd, abs string) string {
	if cwd == "" {
		return abs
	}

	rel, err := filepath.Rel(cwd, abs)
	if err != nil {
		return abs
	}

	return rel
}
