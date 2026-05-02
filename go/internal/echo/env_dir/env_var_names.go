package env_dir

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

// DefaultEnvVarNames is the bundle MakeDefault* uses when no
// WithEnvVarNames option is supplied.
var DefaultEnvVarNames = EnvVarNames{
	Binary:             EnvBin,
	XDGUtilityOverride: OverrideEnvVarName,
	VerifyOnCollision:  EnvVerifyOnCollision,
}

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
