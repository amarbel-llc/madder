package env_dir

import (
	"os"

	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/xdg"
)

// TODO separate read-only from write

// xdgScope names the XDG scope this env reads/writes — the `<scope>`
// segment in `$XDG_*_HOME/<scope>/`. It is decoupled from any CLI /
// process-identity notion (which lives in cli_main / futility.Utility);
// multiple env_dir instances with different scopes can coexist in the
// same process and address disjoint XDG paths that don't affect one
// another. cutting-garden (CLI identity = "cutting-garden") constructs
// an env_dir with xdgScope="madder" so it operates on madder's blob
// store paths; if it also wants its own state, it constructs a SECOND
// env_dir with xdgScope="cutting-garden" using the same Config bundle.
//
// MakeWithXDG and MakeFromXDGDotenvPath take an externally-supplied
// xdg.XDG (or a dotenv that builds one) instead of an xdgScope string;
// in those constructors the scope is read from xdg.UtilityName, which
// is single-source-of-truth.

func MakeDefault(
	context errors.Context,
	cfg Config,
	xdgScope string,
) env {
	return MakeWithDefaultHome(context, cfg, xdgScope, true, true)
}

func MakeDefaultNoInit(
	context errors.Context,
	cfg Config,
	xdgScope string,
) env {
	return MakeWithDefaultHome(context, cfg, xdgScope, true, false)
}

func MakeFromXDGDotenvPath(
	context errors.Context,
	cfg Config,
	xdgDotenvPath string,
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

	return MakeWithXDG(context, cfg, *dotenv.XDG)
}

func MakeDefaultAndInitialize(
	context errors.Context,
	cfg Config,
	xdgScope string,
	repoId RepoId,
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
			cfg,
			xdgScope,
			cwd,
		)
	}

	var home string

	{
		var err error

		if home, err = os.UserHomeDir(); err != nil {
			context.Cancel(err)
		}
	}

	return MakeWithHomeAndInitialize(context, cfg, xdgScope, home)
}

func MakeWithDefaultHome(
	context errors.Context,
	cfg Config,
	xdgScope string,
	permitCwdXDGOverride bool,
	initialize bool,
) (env env) {
	env.Context = context
	env.envVarNames = cfg.envVarNamesOrDefault()

	if err := env.beforeXDG.initialize(cfg.DebugOptions, xdgScope); err != nil {
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
	cfg Config,
	xdgScope string,
	xdgRootOverride string,
) (env env) {
	env.Context = context
	env.envVarNames = cfg.envVarNamesOrDefault()
	env.xdgInitArgs.Cwd = xdgRootOverride

	if err := env.beforeXDG.initialize(cfg.DebugOptions, xdgScope); err != nil {
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
	cfg Config,
	xdgScope string,
	home string,
) (env env) {
	env.Context = context
	env.envVarNames = cfg.envVarNamesOrDefault()

	if err := env.beforeXDG.initialize(cfg.DebugOptions, xdgScope); err != nil {
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

// MakeWithXDG accepts an externally-supplied xdg.XDG; the scope is read
// from xdg.UtilityName. cfg carries only EnvVarNames and DebugOptions.
func MakeWithXDG(
	context errors.Context,
	cfg Config,
	xdg xdg.XDG,
) (env env) {
	env.Context = context
	env.envVarNames = cfg.envVarNamesOrDefault()
	env.XDG = xdg

	if err := env.beforeXDG.initialize(cfg.DebugOptions, xdg.UtilityName); err != nil {
		env.Cancel(err)
		return env
	}

	if err := env.initializeXDG(); err != nil {
		env.Cancel(err)
		return env
	}

	return env
}
