package commands

import (
	"fmt"
	"os"

	tap "github.com/amarbel-llc/bob/packages/tap-dancer/go"
	"github.com/amarbel-llc/madder/go/internal/bravo/directory_layout"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/command"
)

func init() {
	utility.AddCmd("migrate-legacy-configs", &MigrateLegacyConfigs{})
}

type MigrateLegacyConfigs struct {
	command_components.EnvBlobStore
}

var _ command.CommandWithParams = (*MigrateLegacyConfigs)(nil)

func (cmd *MigrateLegacyConfigs) GetParams() []command.Param { return nil }

func (cmd MigrateLegacyConfigs) GetDescription() command.Description {
	return command.Description{
		Short: "rename legacy blob store config files",
		Long: "Rename any on-disk blob store config files that still use " +
			"the legacy filename (dodder-blob_store-config) to the current " +
			"filename (blob_store-config). Run this once after upgrading " +
			"madder; subsequent commands fail loudly when legacy files " +
			"remain.",
	}
}

func (cmd *MigrateLegacyConfigs) SetFlagDefinitions(
	flagDefinitions interfaces.CLIFlagDefinitions,
) {
}

func (cmd *MigrateLegacyConfigs) Run(req command.Request) {
	req.AssertNoMoreArgs()

	tw := tap.NewWriter(os.Stdout)

	envBlobStore := cmd.MakeEnvBlobStoreWithoutStores(req)

	legacyPaths := directory_layout.GetLegacyBlobStoreConfigPaths(
		req,
		envBlobStore.BlobStore,
	)

	if len(legacyPaths) == 0 {
		tw.Ok("migrate-legacy-configs (no legacy files found)")
		tw.Plan()
		return
	}

	for _, legacyPath := range legacyPaths {
		newPath, err := directory_layout.RenameLegacyBlobStoreConfig(legacyPath)
		if err != nil {
			tw.NotOk(
				fmt.Sprintf("migrate-legacy-configs %s", legacyPath),
				map[string]string{
					"severity": "fail",
					"message":  err.Error(),
				},
			)
			tw.Plan()
			envBlobStore.Cancel(err)
			return
		}

		if err := os.Rename(legacyPath, newPath); err != nil {
			wrapped := errors.Wrapf(err, "renaming %q to %q", legacyPath, newPath)
			tw.NotOk(
				fmt.Sprintf("migrate-legacy-configs %s", legacyPath),
				map[string]string{
					"severity": "fail",
					"message":  wrapped.Error(),
				},
			)
			tw.Plan()
			envBlobStore.Cancel(wrapped)
			return
		}

		tw.Ok(fmt.Sprintf("migrate-legacy-configs %s -> %s", legacyPath, newPath))
	}

	tw.Plan()
}
