package command_components

import (
	"github.com/amarbel-llc/madder/go/internal/delta/env_ui"
	"github.com/amarbel-llc/madder/go/internal/echo/env_dir"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/debug"
	"github.com/amarbel-llc/purse-first/libs/dewey/foxtrot/config_cli"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/command"
)

// DefaultConfig is the CLI config used by madder commands.
// Set by commands.init before any command runs.
var DefaultConfig = config_cli.Default()

type EnvBlobStore struct{}

func (cmd EnvBlobStore) MakeEnvBlobStore(
	req command.Request,
) BlobStoreEnv {
	config := DefaultConfig

	var debugOptions debug.Options
	var envOptions env_ui.Options

	if config != nil {
		debugOptions = config.Debug
		envOptions.CustomOut = config.CustomOut
		envOptions.CustomErr = config.CustomErr
	}

	dir := env_dir.MakeDefault(
		req,
		req.Utility.GetName(),
		debugOptions,
	)

	envUI := env_ui.Make(
		req,
		config,
		debugOptions,
		envOptions,
	)

	envLocal := env_local.Make(envUI, dir)

	return MakeBlobStoreEnv(envLocal)
}
