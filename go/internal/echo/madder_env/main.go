// Package madder_env houses the madder-application-layer
// `env_dir.EnvVarNames` bundle and the env-var name constants the
// madder CLI publishes and consults.
//
// env_dir itself is utility-agnostic: it accepts any `EnvVarNames`
// bundle (or none, which opts out of the env-var features). madder
// commands wire env_dir with `DefaultEnvVarNames` from this package.
// Other utilities that import `pkgs/env_dir` define their own
// equivalent at their application layer (e.g. dodder defines a
// dodder-prefixed bundle in its own package).
//
// This package's public face is the dagnabit-generated
// `pkgs/madder_env` facade, so external consumers (dodder
// today; other wrapper utilities tomorrow) can opt into madder's
// env-var contract when they want to interoperate with madder's
// blob-store ops at madder's scope.
//
// What madder DOES NOT define: an XDG-utility-override env-var
// name. The `env_dir.EnvVarNames.XDGUtilityOverride` field exists
// for utilities that want runtime XDG-scope override semantics, but
// madder commands do not honor any such override — `madder` always
// resolves to `$XDG_*_HOME/madder/`. (Historical: a vestigial
// `MADDER_XDG_UTILITY_OVERRIDE` was inherited from the dodder→madder
// rename in #42; never documented, never tested. Removed under #123
// because madder commands sharing it caused a per-scope-isolation
// gap when a single process held multiple env_dirs at different
// scopes — see the env_dir multi-scope plan.) Wrapper utilities that
// want override semantics define their own bundle (e.g. dodder uses
// `DODDER_XDG_UTILITY_OVERRIDE`).
package madder_env

//go:generate dagnabit export

import "github.com/amarbel-llc/madder/go/internal/echo/env_dir"

// EnvBin is the env-var name madder publishes to subprocesses so
// they can locate the binary that spawned them. Read by external
// scripts that wrap madder; never read by madder itself.
const EnvBin = "BIN_MADDER"

// EnvVerifyOnCollision is the env-var name that, when truthy, flips
// `env_dir.Env.GetVerifyOnCollisionOverride()` to true. See ADR
// 0003 for rationale and #38 for the eventual migration to a CLI
// flag.
const EnvVerifyOnCollision = "MADDER_VERIFY_ON_COLLISION"

// EnvXDGUserLocationOnly is the env-var name that, when set to "1",
// disables the cwd walk-up env_dir.MakeDefault would otherwise perform
// to find an ancestor `.madder/` directory. With this set, env_dir
// uses standard XDG resolution only — honoring $XDG_DATA_HOME etc.
// directly. Useful for embedders and test harnesses that exec madder
// from a cwd whose path branch a MADDER_CEILING_DIRECTORIES entry
// can't gate.
const EnvXDGUserLocationOnly = "MADDER_XDG_USER_LOCATION_ONLY"

// DefaultEnvVarNames is madder's env-var contract bundled for
// passing into `env_dir.Config.EnvVarNames`. Wrapper utilities that
// want to honor madder's env-var contract (e.g. when constructing
// an env_dir at madder's scope to operate against madder's blob
// stores) pass this directly:
//
//	cfg := env_dir.Config{EnvVarNames: madder_env.DefaultEnvVarNames}
//	madderScopedEnv := env_dir.MakeDefault(ctx, cfg, "madder")
//
// Wrapper utilities that want their OWN env-var contract for their
// OWN scope define their own `EnvVarNames` bundle in their own
// application layer.
//
// XDGUtilityOverride is intentionally empty — see the package
// doc-comment for why madder doesn't honor any such env var.
var DefaultEnvVarNames = env_dir.EnvVarNames{
	Binary:              EnvBin,
	VerifyOnCollision:   EnvVerifyOnCollision,
	XDGUserLocationOnly: EnvXDGUserLocationOnly,
}
