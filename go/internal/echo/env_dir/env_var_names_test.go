//go:build test

package env_dir

import "testing"

// TestEnvVarNames pins the DODDER → MADDER env var rename from issue #42.
// EnvBin is set via os.Setenv and exposed to subprocesses through
// MakeCommonEnv; OverrideEnvVarName is read from the user's shell by the
// dewey XDG stack; EnvVerifyOnCollision is read at env construction time
// to OR with the per-store config field. All three are user-visible
// contracts.
func TestEnvVarNames(t *testing.T) {
	if EnvBin != "BIN_MADDER" {
		t.Errorf("EnvBin = %q, want %q", EnvBin, "BIN_MADDER")
	}

	if OverrideEnvVarName != "MADDER_XDG_UTILITY_OVERRIDE" {
		t.Errorf("OverrideEnvVarName = %q, want %q",
			OverrideEnvVarName, "MADDER_XDG_UTILITY_OVERRIDE")
	}

	if EnvVerifyOnCollision != "MADDER_VERIFY_ON_COLLISION" {
		t.Errorf("EnvVerifyOnCollision = %q, want %q",
			EnvVerifyOnCollision, "MADDER_VERIFY_ON_COLLISION")
	}
}

// TestDefaultEnvVarNames pins the bundle MakeDefault* uses when no
// WithEnvVarNames option is supplied. Issue #105 — consumers reusing
// pkgs/env_dir need DefaultEnvVarNames to keep producing today's
// BIN_MADDER / MADDER_* contract for madder's own callers.
func TestDefaultEnvVarNames(t *testing.T) {
	if got, want := DefaultEnvVarNames.Binary, EnvBin; got != want {
		t.Errorf("DefaultEnvVarNames.Binary = %q, want %q", got, want)
	}

	if got, want := DefaultEnvVarNames.XDGUtilityOverride, OverrideEnvVarName; got != want {
		t.Errorf("DefaultEnvVarNames.XDGUtilityOverride = %q, want %q", got, want)
	}

	if got, want := DefaultEnvVarNames.VerifyOnCollision, EnvVerifyOnCollision; got != want {
		t.Errorf("DefaultEnvVarNames.VerifyOnCollision = %q, want %q", got, want)
	}
}
