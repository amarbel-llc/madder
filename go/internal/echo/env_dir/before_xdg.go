package env_dir

import (
	"os"

	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/debug"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/xdg"
)

type beforeXDG struct {
	xdgInitArgs xdg.InitArgs

	envVarNames EnvVarNames

	dryRun       bool
	debugOptions debug.Options
}

func (env *beforeXDG) initialize(
	debugOptions debug.Options,
	utilityName string,
) (err error) {
	env.debugOptions = debugOptions
	env.xdgInitArgs.OverrideEnvVarName = env.envVarNames.XDGUtilityOverride

	if err = env.xdgInitArgs.Initialize(utilityName); err != nil {
		err = errors.Wrap(err)
		return err
	}

	env.dryRun = debugOptions.DryRun

	// TODO switch to useing MakeCommonEnv()
	{
		if err = os.Setenv(env.envVarNames.Binary, env.xdgInitArgs.ExecPath); err != nil {
			err = errors.Wrap(err)
			return err
		}
	}

	return err
}
