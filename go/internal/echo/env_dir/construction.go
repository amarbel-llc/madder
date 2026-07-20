package env_dir

import (
	"os"

	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/xdg"
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
	repoId scoped_id.Id,
) env {
	// madder#240: a named repo nests its blob-store XDG under
	// repos/<name>/. Empty name (the default/legacy id) leaves the
	// shared layout untouched.
	cfg.RepoName = repoId.GetName()

	switch repoId.GetLocationType() {
	case scoped_id.LocationTypeXDGSystem:
		// madder#280: a //name (system) id roots its XDG at the injected
		// system root (Config.SystemRoot, e.g. /var/lib/madder) instead of
		// the user $HOME. Force SystemScoped so initializeXDG re-roots the
		// base XDG at SystemRoot — the same rootAtSystem path
		// GetXDGForBlobStoreId applies per store (madder#230). When
		// SystemRoot is unset the re-root no-ops and the env resolves like a
		// user env, consistent with GetXDGForBlobStoreId's empty-SystemRoot
		// no-op. (Previously this branch panicked Err501NotImplemented; the
		// remote-first `/name` marker stays a dodder concern — madder has no
		// remote transport and resolves both `/name` and `//name` to the
		// system scope.)
		cfg.SystemScoped = true

		var home string

		{
			var err error

			if home, err = os.UserHomeDir(); err != nil {
				context.Cancel(err)
			}
		}

		return MakeWithHomeAndInitialize(context, cfg, xdgScope, home)

	case scoped_id.LocationTypeCwd:
		var cwd string

		{
			var err error

			if cwd, err = os.Getwd(); err != nil {
				context.Cancel(err)
			}
		}

		// madder#153: resolve a multi-dot id (`..name`, `...name`) by
		// walking up cwdDepth literal parent directories from cwd before
		// rooting the XDG. depth 0 (`.name`) stays at cwd, preserving the
		// constructor's prior single-dot behavior. Errors (not clamps)
		// when the depth overruns the available ancestors or a ceiling.
		ceilings := xdg.ParseCeilingDirectories(
			os.Getenv(xdg.CeilingEnvVarName(xdgScope)),
		)

		root, err := resolveCwdAncestorOrError(
			cwd,
			repoId.GetCwdDepth(),
			ceilings,
		)
		if err != nil {
			context.Cancel(err)
		}

		return MakeWithXDGRootOverrideHomeAndInitialize(
			context,
			cfg,
			xdgScope,
			root,
		)
	}

	// XDGUser, unknown, and the zero value all resolve against $HOME.
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
	env.envVarNames = cfg.EnvVarNames
	env.repoName = cfg.RepoName
	env.systemRoot = cfg.SystemRoot
	env.systemScoped = cfg.SystemScoped

	if err := env.beforeXDG.initialize(cfg.DebugOptions, xdgScope); err != nil {
		env.Cancel(err)
		return env
	}

	if !initialize {
		return env
	}

	if permitCwdXDGOverride {
		if name := cfg.EnvVarNames.XDGUserLocationOnly; name != "" && parseBoolEnv(os.Getenv(name)) {
			permitCwdXDGOverride = false
		}
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
) env {
	return MakeWithXDGRootOverrideHome(context, cfg, xdgScope, xdgRootOverride, true)
}

// MakeWithXDGRootOverrideHomeNoInit mirrors
// MakeWithXDGRootOverrideHomeAndInitialize but omits initializeXDG's mkdir,
// pairing with MakeDefaultNoInit the same way MakeWithXDGRootOverrideHomeAndInitialize
// pairs with MakeDefault. It lets callers read GetXDG().Data.ActualValue (and
// the other category dirs) for a root that may not exist yet — e.g. a
// pre-existence check — without the side effect of creating it.
func MakeWithXDGRootOverrideHomeNoInit(
	context errors.Context,
	cfg Config,
	xdgScope string,
	xdgRootOverride string,
) env {
	return MakeWithXDGRootOverrideHome(context, cfg, xdgScope, xdgRootOverride, false)
}

// MakeWithXDGRootOverrideHome is the shared body behind
// MakeWithXDGRootOverrideHomeAndInitialize / MakeWithXDGRootOverrideHomeNoInit,
// mirroring the initialize-bool parameterization MakeWithDefaultHome already
// uses for the MakeDefault / MakeDefaultNoInit pair.
func MakeWithXDGRootOverrideHome(
	context errors.Context,
	cfg Config,
	xdgScope string,
	xdgRootOverride string,
	initialize bool,
) (env env) {
	env.Context = context
	env.envVarNames = cfg.EnvVarNames
	env.repoName = cfg.RepoName
	env.systemRoot = cfg.SystemRoot
	env.systemScoped = cfg.SystemScoped
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

	if !initialize {
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
	env.envVarNames = cfg.EnvVarNames
	env.repoName = cfg.RepoName
	env.systemRoot = cfg.SystemRoot
	env.systemScoped = cfg.SystemScoped

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
	env.envVarNames = cfg.EnvVarNames
	env.repoName = cfg.RepoName
	env.systemRoot = cfg.SystemRoot
	env.systemScoped = cfg.SystemScoped
	env.XDG = xdg

	if err := env.beforeXDG.initialize(cfg.DebugOptions, xdg.UtilityName); err != nil {
		env.Cancel(err)
		return env
	}

	if err := env.initializeXDG(); err != nil {
		env.Cancel(err)
		return env
	}

	// madder#239: register temp-dir cleanup like the sibling constructors
	// (MakeWithDefaultHome, MakeWith...RootOverride..., MakeWithHome...).
	// Without this an env built from an externally-supplied xdg.XDG (e.g.
	// via MakeFromXDGDotenvPath) leaks its per-pid temp dir on exit.
	env.After(env.resetTempOnExit)

	return env
}
