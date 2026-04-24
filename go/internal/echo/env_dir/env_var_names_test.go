//go:build test

package env_dir

import "testing"

// TestEnvVarNames pins the DODDER → MADDER env var rename from issue #42.
// EnvBin is set via os.Setenv and exposed to subprocesses through
// MakeCommonEnv; OverrideEnvVarName is read from the user's shell by the
// dewey XDG stack. Both are user-visible contracts.
func TestEnvVarNames(t *testing.T) {
	if EnvBin != "BIN_MADDER" {
		t.Errorf("EnvBin = %q, want %q", EnvBin, "BIN_MADDER")
	}

	if OverrideEnvVarName != "MADDER_XDG_UTILITY_OVERRIDE" {
		t.Errorf("OverrideEnvVarName = %q, want %q",
			OverrideEnvVarName, "MADDER_XDG_UTILITY_OVERRIDE")
	}
}
