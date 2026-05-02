//go:build test

package env_dir

import "testing"

// TestEnvVarNames pins the DODDER → MADDER env var rename from #42. All
// three constants are user-visible contracts.
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
// WithEnvVarNames option is supplied.
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

// TestApplyOptions_NoOpts must yield DefaultEnvVarNames so callers that
// pass no options keep madder's BIN_MADDER / MADDER_* contract.
func TestApplyOptions_NoOpts(t *testing.T) {
	got := applyOptions(nil)

	if got.envVarNames != DefaultEnvVarNames {
		t.Errorf("applyOptions(nil).envVarNames = %+v, want %+v",
			got.envVarNames, DefaultEnvVarNames)
	}
}

// TestWithEnvVarNames_Overrides proves the consumer-facing seam: a
// custom EnvVarNames bundle passed via WithEnvVarNames replaces the
// defaults inside the resolved makeOpts. The Make* constructors copy
// resolved.envVarNames onto the env struct, which subsequent reads
// (AddToEnvVars, MakeCommonEnv, beforeXDG.initialize, initializeXDG)
// consult — so this test guards the contract end-to-end without
// touching the filesystem.
func TestWithEnvVarNames_Overrides(t *testing.T) {
	custom := EnvVarNames{
		Binary:             "X_BIN",
		XDGUtilityOverride: "X_OVERRIDE",
		VerifyOnCollision:  "X_VERIFY",
	}

	got := applyOptions([]Option{WithEnvVarNames(custom)})

	if got.envVarNames != custom {
		t.Errorf("applyOptions(WithEnvVarNames(%+v)).envVarNames = %+v, want %+v",
			custom, got.envVarNames, custom)
	}
}
