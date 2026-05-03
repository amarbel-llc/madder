package env_dir

import "github.com/amarbel-llc/purse-first/libs/dewey/echo/debug"

// EnvVarNames bundles the env var names env_dir reads from and writes to,
// so consumers can supply their own prefix without forking.
type EnvVarNames struct {
	// Binary names the env var written for subprocesses so they can find
	// the binary that spawned them.
	Binary string

	// XDGUtilityOverride names the env var the dewey XDG stack consults
	// to override the utilityName the call site passes in.
	XDGUtilityOverride string

	// VerifyOnCollision names the env var that, when truthy, flips
	// GetVerifyOnCollisionOverride to true. See ADR 0003 for rationale,
	// #38 for the eventual migration to a CLI flag.
	VerifyOnCollision string
}

const (
	EnvBin               = "BIN_MADDER"
	OverrideEnvVarName   = "MADDER_XDG_UTILITY_OVERRIDE"
	EnvVerifyOnCollision = "MADDER_VERIFY_ON_COLLISION"
)

// DefaultEnvVarNames is the bundle Make* uses when Config.EnvVarNames is
// the zero value.
var DefaultEnvVarNames = EnvVarNames{
	Binary:             EnvBin,
	XDGUtilityOverride: OverrideEnvVarName,
	VerifyOnCollision:  EnvVerifyOnCollision,
}

// Config bundles the construction inputs that are reusable across XDG
// scopes. The XDG scope itself is NOT here — it is passed as its own arg
// to each Make* constructor (or implied by an externally-supplied
// xdg.XDG / dotenv path) so a single Config value can be applied to
// multiple env_dir instances with disjoint scopes:
//
//	cfg := env_dir.Config{DebugOptions: dbg}
//	madderEnv := env_dir.MakeDefault(ctx, cfg, "madder")
//	cgEnv     := env_dir.MakeDefault(ctx, cfg, "cutting-garden")
//
// EnvVarNames defaults to DefaultEnvVarNames when zero. Partial-override
// is not supported: if any field is set, the entire bundle is taken
// as-is (matches the prior WithEnvVarNames semantics).
type Config struct {
	EnvVarNames  EnvVarNames
	DebugOptions debug.Options
}

// envVarNamesOrDefault returns cfg.EnvVarNames when any field is non-zero,
// otherwise DefaultEnvVarNames.
func (cfg Config) envVarNamesOrDefault() EnvVarNames {
	if cfg.EnvVarNames == (EnvVarNames{}) {
		return DefaultEnvVarNames
	}
	return cfg.EnvVarNames
}
