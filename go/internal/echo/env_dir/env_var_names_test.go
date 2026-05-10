//go:build test

package env_dir

import "testing"

// TestConfig_EnvVarNamesPassedThrough proves Config.EnvVarNames is
// taken as-is — no fallback, no merge with library defaults. env_dir
// is utility-agnostic; the application layer (e.g. madder_env) owns
// its own EnvVarNames bundle.
func TestConfig_EnvVarNamesPassedThrough(t *testing.T) {
	custom := EnvVarNames{
		Binary:              "X_BIN",
		XDGUtilityOverride:  "X_OVERRIDE",
		VerifyOnCollision:   "X_VERIFY",
		XDGUserLocationOnly: "X_USER_LOCATION_ONLY",
	}
	cfg := Config{EnvVarNames: custom}

	if cfg.EnvVarNames != custom {
		t.Errorf("Config.EnvVarNames = %+v, want %+v",
			cfg.EnvVarNames, custom)
	}
}

// TestConfig_ZeroEnvVarNames_TolerableForSubprocessPublish proves the
// zero-value EnvVarNames opts out of subprocess publishing instead of
// emitting under an empty key (which would be invalid).
//
// Background: an env_dir built from Config{} has EnvVarNames.Binary
// == "". AddToEnvVars and MakeCommonEnv MUST NOT emit a key-value pair
// in that state — an empty env-var name is invalid syntax for
// `exec.Cmd.Env` and would silently corrupt subprocess environments.
func TestConfig_ZeroEnvVarNames_TolerableForSubprocessPublish(t *testing.T) {
	env := env{}
	env.envVarNames = EnvVarNames{}

	if got := env.MakeCommonEnv(); got != nil {
		t.Errorf("MakeCommonEnv with empty EnvVarNames = %+v, want nil",
			got)
	}

	envVars := map[string]string{}
	env.AddToEnvVars(envVars)
	if len(envVars) != 0 {
		t.Errorf("AddToEnvVars with empty EnvVarNames populated %d entries: %+v",
			len(envVars), envVars)
	}
}
