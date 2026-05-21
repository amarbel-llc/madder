package env_dir

import (
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/debug"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/xdg"
)

type beforeXDG struct {
	xdgInitArgs xdg.InitArgs

	envVarNames EnvVarNames

	dryRun       bool
	debugOptions debug.Options
}

// initialize wires up xdgInitArgs and stashes debug/dry-run state.
// The binary path stays on env.xdgInitArgs.ExecPath; subprocess
// plumbing publishes it via MakeCommonEnv / AddToEnvVars and passes
// it through exec.Cmd.Env explicitly rather than via process-env
// inheritance.
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

	return err
}
