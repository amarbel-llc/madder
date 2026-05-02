package env_dir

// EnvVarNames bundles the env var names env_dir reads from and writes to.
// Exists so consumers (madder, dodder, future utilities) can supply their
// own prefix without forking env_dir; the only consumer-visible difference
// between callers is cosmetic. See issue #105.
type EnvVarNames struct {
	// Binary is the env var written via AddToEnvVars / MakeCommonEnv /
	// beforeXDG.initialize so subprocesses can find the binary that
	// spawned them.
	Binary string

	// XDGUtilityOverride is read by the dewey XDG stack to override the
	// utilityName the call site passes in.
	XDGUtilityOverride string

	// VerifyOnCollision is read at env construction time; truthy values
	// flip GetVerifyOnCollisionOverride to true. See issue #31 and
	// ADR 0003 for rationale, #38 for the eventual flag migration.
	VerifyOnCollision string
}

const (
	// EnvBin is the env var name madder exports to subprocesses so they
	// can find the madder binary that spawned them. Set in
	// beforeXDG.initialize and surfaced via MakeCommonEnv / AddToEnvVars
	// when DefaultEnvVarNames is in effect.
	EnvBin = "BIN_MADDER"

	// OverrideEnvVarName is the env var a user sets to override the XDG
	// utility name dewey resolves at startup, when DefaultEnvVarNames is
	// in effect. When set, its value wins over whatever utilityName the
	// call site passes into env_dir.MakeDefault.
	OverrideEnvVarName = "MADDER_XDG_UTILITY_OVERRIDE"

	// EnvVerifyOnCollision is the env var read at env construction time
	// to opt into byte-by-byte verification on hash collisions, when
	// DefaultEnvVarNames is in effect.
	EnvVerifyOnCollision = "MADDER_VERIFY_ON_COLLISION"
)

// DefaultEnvVarNames preserves madder's original env var contract
// (BIN_MADDER / MADDER_XDG_UTILITY_OVERRIDE / MADDER_VERIFY_ON_COLLISION).
// MakeDefault and friends use this when no WithEnvVarNames option is
// supplied. Other consumers pass their own EnvVarNames via WithEnvVarNames.
var DefaultEnvVarNames = EnvVarNames{
	Binary:             EnvBin,
	XDGUtilityOverride: OverrideEnvVarName,
	VerifyOnCollision:  EnvVerifyOnCollision,
}

// Option configures env construction. Pass instances to MakeDefault,
// MakeDefaultNoInit, MakeDefaultAndInitialize, MakeWithDefaultHome,
// MakeWithXDGRootOverrideHomeAndInitialize, MakeWithHomeAndInitialize,
// MakeWithXDG, or MakeFromXDGDotenvPath.
type Option func(*makeOpts)

type makeOpts struct {
	envVarNames EnvVarNames
}

func defaultMakeOpts() makeOpts {
	return makeOpts{envVarNames: DefaultEnvVarNames}
}

func applyOptions(opts []Option) makeOpts {
	resolved := defaultMakeOpts()
	for _, opt := range opts {
		opt(&resolved)
	}
	return resolved
}

// WithEnvVarNames overrides the env var names env_dir reads from / writes
// to. When unset, DefaultEnvVarNames applies (madder's BIN_MADDER /
// MADDER_* contract).
func WithEnvVarNames(names EnvVarNames) Option {
	return func(o *makeOpts) {
		o.envVarNames = names
	}
}
