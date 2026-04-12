package commands_madder

import (
	"fmt"
	"os"

	tap "github.com/amarbel-llc/bob/packages/tap-dancer/go"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/charlie/fd"
	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/golf/command"
	"github.com/amarbel-llc/madder/go/internal/hotel/command_components_madder"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

func init() {
	utility.AddCmd("init-from", &InitFrom{})
}

type InitFrom struct {
	command_components_madder.EnvBlobStore
	command_components_madder.Init
}

var (
	_ interfaces.CommandComponentWriter = (*InitFrom)(nil)
	_ command.CommandWithArgs           = (*InitFrom)(nil)
)

func (cmd *InitFrom) GetArgs() []command.ArgGroup {
	return []command.ArgGroup{{
		Args: []command.Arg{
			{
				Name:        "store-name",
				Description: "name for the new blob store",
				Required:    true,
			},
			{
				Name:        "config-path",
				Description: "path to the blob store configuration file",
				Required:    true,
			},
		},
	}}
}

func (cmd InitFrom) GetDescription() command.Description {
	return command.Description{
		Short: "initialize a blob store from a configuration file",
		Long: "Create a new blob store by reading its type and settings " +
			"from a hyphence-encoded configuration file. The config is " +
			"automatically upgraded to the current version if an older " +
			"format is detected. Requires a store name and the path to " +
			"the configuration file.",
	}
}

func (cmd *InitFrom) SetFlagDefinitions(
	flagDefinitions interfaces.CLIFlagDefinitions,
) {
}

func (cmd InitFrom) Complete(
	req command.Request,
	envLocal env_local.Env,
	commandLine command.CommandLineInput,
) {
	// TODO support completion for config path
}

func (cmd *InitFrom) Run(req command.Request) {
	var blobStoreId blob_store_id.Id

	if err := blobStoreId.Set(req.PopArg("blob store name")); err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
	}

	var configPathFD fd.FD

	if err := configPathFD.Set(req.PopArg("blob store config path")); err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
	}

	req.AssertNoMoreArgs()

	tw := tap.NewWriter(os.Stdout)

	envBlobStore := cmd.MakeEnvBlobStore(req)

	var typedConfig blob_store_configs.TypedConfig

	{
		var err error

		if typedConfig, err = hyphence.DecodeFromFile(
			blob_store_configs.Coder,
			configPathFD.String(),
		); err != nil {
			tw.NotOk(
				fmt.Sprintf("init-from %s", configPathFD.String()),
				map[string]string{
					"severity": "fail",
					"message":  err.Error(),
				},
			)
			tw.Plan()
			envBlobStore.Cancel(err)
			return
		}
	}

	for {
		configUpgraded, ok := typedConfig.Blob.(blob_store_configs.ConfigUpgradeable)

		if !ok {
			break
		}

		typedConfig.Blob, typedConfig.Type = configUpgraded.Upgrade()
	}

	pathConfig := cmd.InitBlobStore(
		req,
		envBlobStore,
		blobStoreId,
		&typedConfig,
	)

	tw.Ok(fmt.Sprintf("init-from %s", pathConfig.GetConfig()))
	tw.Plan()
}
