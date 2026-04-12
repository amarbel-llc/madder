package command_components_madder

import (
	"fmt"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/charlie/repo_config_cli"
	"github.com/amarbel-llc/madder/go/internal/delta/env_ui"
	"github.com/amarbel-llc/madder/go/internal/echo/env_dir"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/golf/command"
	"github.com/amarbel-llc/madder/go/internal/golf/env_repo"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/debug"
	"github.com/amarbel-llc/purse-first/libs/dewey/foxtrot/config_cli"
)

type EnvBlobStore struct{}

func (cmd EnvBlobStore) MakeEnvBlobStore(
	req command.Request,
) env_repo.BlobStoreEnv {
	configAny := req.Utility.GetConfigAny()

	var debugOptions debug.Options
	var cliConfig domain_interfaces.CLIConfigProvider
	var envOptions env_ui.Options

	switch c := configAny.(type) {
	case *config_cli.Config:
		debugOptions = c.Debug
		cliConfig = c
		envOptions.CustomOut = c.CustomOut
		envOptions.CustomErr = c.CustomErr
	case *repo_config_cli.Config:
		debugOptions = c.Debug
		cliConfig = c
	default:
		panic(fmt.Sprintf("unsupported config type: %T", configAny))
	}

	dir := env_dir.MakeDefault(
		req,
		req.Utility.GetName(),
		debugOptions,
	)

	envUI := env_ui.Make(
		req,
		cliConfig,
		debugOptions,
		envOptions,
	)

	envLocal := env_local.Make(envUI, dir)

	return env_repo.MakeBlobStoreEnv(envLocal)
}
