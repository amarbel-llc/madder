package env_dir

import "github.com/amarbel-llc/purse-first/libs/dewey/echo/debug"

// EnvVarNames bundles the env var names env_dir reads from and writes to.
// Consumers supply their own bundle via Config.EnvVarNames; env_dir does
// not provide application-specific defaults — that belongs at the
// caller's application layer (e.g. madder commands pass
// `madder_env.DefaultEnvVarNames`).
//
// Empty fields opt the corresponding env-var feature OUT entirely:
//
//   - Empty `Binary`: env_dir does not publish the binary path to
//     subprocesses (AddToEnvVars and MakeCommonEnv emit nothing).
//   - Empty `XDGUtilityOverride`: env_dir does not honor any env-var
//     override of the XDG scope (the dewey XDG stack receives an
//     empty override-env-var-name and therefore reads no override).
//   - Empty `VerifyOnCollision`: env_dir does not honor any env-var
//     override of the verify-on-collision toggle
//     (`os.Getenv("")` returns "", which parses as false).
//
// This makes `Config{}` (zero-value) a viable construction shape for
// callers that want a pure XDG-only env_dir with no env-var-driven
// behavior.
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

// Config bundles the construction inputs that are reusable across XDG
// scopes. The XDG scope itself is NOT here — it is passed as its own arg
// to each Make* constructor (or implied by an externally-supplied
// xdg.XDG / dotenv path) so a single Config value can be applied to
// multiple env_dir instances with disjoint scopes:
//
//	cfg := env_dir.Config{
//	    EnvVarNames:  madder_env.DefaultEnvVarNames,
//	    DebugOptions: dbg,
//	}
//	madderEnv := env_dir.MakeDefault(ctx, cfg, "madder")
//	cgEnv     := env_dir.MakeDefault(ctx, cfg, "cutting-garden")
//
// EnvVarNames is taken as-is. env_dir does NOT supply defaults — see
// the EnvVarNames doc-comment for what empty fields mean.
type Config struct {
	EnvVarNames  EnvVarNames
	DebugOptions debug.Options
}
