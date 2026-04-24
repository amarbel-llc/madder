package env_dir

import (
	"os"

	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/debug"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/xdg"
)

// OverrideEnvVarName is the env var a user sets to override the XDG
// utility name dewey resolves at startup. When set, its value wins over
// whatever utilityName the call site passes into env_dir.MakeDefault.
const OverrideEnvVarName = "MADDER_XDG_UTILITY_OVERRIDE"

type beforeXDG struct {
	xdgInitArgs xdg.InitArgs

	dryRun       bool
	debugOptions debug.Options
}

func (env *beforeXDG) initialize(
	debugOptions debug.Options,
	utilityName string,
) (err error) {
	env.debugOptions = debugOptions
	env.xdgInitArgs.OverrideEnvVarName = OverrideEnvVarName

	if err = env.xdgInitArgs.Initialize(utilityName); err != nil {
		err = errors.Wrap(err)
		return err
	}

	env.dryRun = debugOptions.DryRun

	// TODO switch to useing MakeCommonEnv()
	{
		if err = os.Setenv(EnvBin, env.xdgInitArgs.ExecPath); err != nil {
			err = errors.Wrap(err)
			return err
		}
	}

	return err
}
