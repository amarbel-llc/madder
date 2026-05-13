package commands

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
		Short: "list configured blob stores",
		Long: "List all blob stores configured for the current repository, " +
			"showing each store's ID, description, and the location of its " +
			"on-disk config file. Each line ends with '# path: <rel>' " +
			"where <rel> is the config-file path expressed relative to " +
			"the current working directory (falling back to absolute when " +
			"the relative form cannot be computed). Store IDs use prefixes " +
			"to indicate scope: unprefixed for XDG user stores, '.' for " +
			"CWD-relative stores, and '/' for XDG system stores.",
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
// cwd is empty or filepath.Rel rejects the inputs (e.g. they live on
// different volumes on Windows; not a concern on Unix but
// filepath.Rel can still error). The "relative" form is the friendly
// rendering for humans running list inside a repo; the absolute
// fallback keeps the output usable in pathological setups.
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
