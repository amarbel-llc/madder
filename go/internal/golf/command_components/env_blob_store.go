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
	return MakeBlobStoreEnv(cmd.makeEnvLocal(req))
}

// MakeEnvBlobStoreWithoutStores returns a BlobStoreEnv with the directory
// layout wired up but no blob stores discovered or initialized. Use this from
// commands that must operate before discovery would succeed, such as the
// legacy-config migration command.
func (cmd EnvBlobStore) MakeEnvBlobStoreWithoutStores(
	req command.Request,
) BlobStoreEnv {
	return MakeBlobStoreEnvWithoutStores(cmd.makeEnvLocal(req))
}

func (cmd EnvBlobStore) makeEnvLocal(
	req command.Request,
) env_local.Env {
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

	return env_local.Make(envUI, dir)
}
