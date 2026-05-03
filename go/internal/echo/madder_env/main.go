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
package madder_env

//go:generate dagnabit export

import "github.com/amarbel-llc/madder/go/internal/echo/env_dir"

// EnvBin is the env-var name madder publishes to subprocesses so
// they can locate the binary that spawned them. Read by external
// scripts that wrap madder; never read by madder itself.
const EnvBin = "BIN_MADDER"

// OverrideEnvVarName is the env-var name the dewey XDG stack
// consults to override the utilityName a call site passed in.
// Setting this redirects every env_dir built with the madder
// EnvVarNames bundle to a different XDG scope. (Note: today this
// affects every env_dir that uses the madder bundle, including
// cutting-garden's; full per-scope isolation requires per-scope
// EnvVarNames bundles — tracked in #123.)
const OverrideEnvVarName = "MADDER_XDG_UTILITY_OVERRIDE"

// EnvVerifyOnCollision is the env-var name that, when truthy, flips
// `env_dir.Env.GetVerifyOnCollisionOverride()` to true. See ADR
// 0003 for rationale and #38 for the eventual migration to a CLI
// flag.
const EnvVerifyOnCollision = "MADDER_VERIFY_ON_COLLISION"

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
var DefaultEnvVarNames = env_dir.EnvVarNames{
	Binary:             EnvBin,
	XDGUtilityOverride: OverrideEnvVarName,
	VerifyOnCollision:  EnvVerifyOnCollision,
}
