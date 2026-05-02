package env_dir

import (
	"os"

	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/debug"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/xdg"
)

// TODO separate read-only from write

// utilityName names the XDG scope this env reads/writes — the `<scope>`
// segment in `$XDG_*_HOME/<scope>/`. It is decoupled from any CLI /
// process-identity notion (which lives in cli_main / futility.Utility);
// multiple env_dir instances with different utility names can coexist
// in the same process and address disjoint XDG scopes that don't
// affect each other. See command_components.EnvBlobStore.BlobStoreXDGScope
// for one such use: cutting-garden (CLI identity = "cutting-garden")
// constructs an env_dir with utilityName="madder" so it operates on
// madder's blob store paths. The deeper struct-state /
// multi-scope-composition refactor is tracked separately.

func MakeDefault(
	context errors.Context,
	utilityName string,
	debugOptions debug.Options,
	opts ...Option,
) env {
	return MakeWithDefaultHome(
		context,
		utilityName,
		debugOptions,
		true,
		true,
		opts...,
	)
}

func MakeDefaultNoInit(
	context errors.Context,
	utilityName string,
	debugOptions debug.Options,
	opts ...Option,
) env {
	return MakeWithDefaultHome(
		context,
		utilityName,
		debugOptions,
		true,
		false,
		opts...,
	)
}

func MakeFromXDGDotenvPath(
	context errors.Context,
	debugOptions debug.Options,
	xdgDotenvPath string,
	opts ...Option,
) env {
	dotenv := xdg.Dotenv{
		XDG: &xdg.XDG{},
	}

	var file *os.File

	{
		var err error

		if file, err = os.Open(xdgDotenvPath); err != nil {
			context.Cancel(err)
		}
	}

	if _, err := dotenv.ReadFrom(file); err != nil {
		context.Cancel(err)
	}

	if err := file.Close(); err != nil {
		context.Cancel(err)
	}

	return MakeWithXDG(
		context,
		debugOptions,
		*dotenv.XDG,
		opts...,
	)
}

func MakeDefaultAndInitialize(
	context errors.Context,
	utilityName string,
	do debug.Options,
	repoId RepoId,
	opts ...Option,
) env {
	if repoId.IsSystem() {
		panic(errors.WithoutStack(errors.Err501NotImplemented))
	}

	if repoId.IsCwd() {
		var cwd string

		{
			var err error

			if cwd, err = os.Getwd(); err != nil {
				context.Cancel(err)
			}
		}

		return MakeWithXDGRootOverrideHomeAndInitialize(
			context,
			cwd,
			utilityName,
			do,
			opts...,
		)
	}

	var home string

	{
		var err error

		if home, err = os.UserHomeDir(); err != nil {
			context.Cancel(err)
		}
	}

	return MakeWithHomeAndInitialize(
		context,
		utilityName,
		home,
		do,
		opts...,
	)
}

func MakeWithDefaultHome(
	context errors.Context,
	utilityName string,
	debugOptions debug.Options,
	permitCwdXDGOverride bool,
	initialize bool,
	opts ...Option,
) (env env) {
	resolved := applyOptions(opts)
	env.Context = context
	env.envVarNames = resolved.envVarNames

	if err := env.beforeXDG.initialize(debugOptions, utilityName); err != nil {
		env.Cancel(err)
		return env
	}

	if !initialize {
		return env
	}

	if permitCwdXDGOverride {
		if err := env.XDG.InitializeOverriddenIfNecessary(env.xdgInitArgs); err != nil {
			env.Cancel(err)
			return env
		}
	} else {
		if err := env.XDG.InitializeStandardFromEnv(env.xdgInitArgs); err != nil {
			env.Cancel(err)
			return env
		}
	}

	if err := env.initializeXDG(); err != nil {
		env.Cancel(err)
		return env
	}

	env.After(env.resetTempOnExit)

	return env
}

func MakeWithXDGRootOverrideHomeAndInitialize(
	context errors.Context,
	xdgRootOverride string,
	utilityName string,
	debugOptions debug.Options,
	opts ...Option,
) (env env) {
	resolved := applyOptions(opts)
	env.Context = context
	env.envVarNames = resolved.envVarNames
	env.xdgInitArgs.Cwd = xdgRootOverride

	if err := env.beforeXDG.initialize(debugOptions, utilityName); err != nil {
		env.Cancel(err)
		return env
	}

	if err := env.XDG.InitializeOverridden(
		env.xdgInitArgs,
		xdgRootOverride,
	); err != nil {
		env.Cancel(err)
		return env
	}

	if err := env.initializeXDG(); err != nil {
		env.Cancel(err)
		return env
	}

	env.After(env.resetTempOnExit)

	return env
}

func MakeWithHomeAndInitialize(
	context errors.Context,
	utilityName string,
	home string,
	debugOptions debug.Options,
	opts ...Option,
) (env env) {
	resolved := applyOptions(opts)
	env.Context = context
	env.envVarNames = resolved.envVarNames

	if err := env.beforeXDG.initialize(debugOptions, utilityName); err != nil {
		env.Cancel(err)
	}

	if err := env.XDG.InitializeStandardFromEnv(env.xdgInitArgs); err != nil {
		env.Cancel(err)
		return env
	}

	if err := env.initializeXDG(); err != nil {
		env.Cancel(err)
		return env
	}

	env.After(env.resetTempOnExit)

	return env
}

func MakeWithXDG(
	context errors.Context,
	debugOptions debug.Options,
	xdg xdg.XDG,
	opts ...Option,
) (env env) {
	resolved := applyOptions(opts)
	env.Context = context
	env.envVarNames = resolved.envVarNames
	env.XDG = xdg

	if err := env.beforeXDG.initialize(debugOptions, xdg.UtilityName); err != nil {
		env.Cancel(err)
		return env
	}

	if err := env.initializeXDG(); err != nil {
		env.Cancel(err)
		return env
	}

	return env
}
