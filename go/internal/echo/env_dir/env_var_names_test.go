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

// TestDefaultEnvVarNames pins the bundle Config falls back to when its
// EnvVarNames field is the zero value.
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

// TestConfig_ZeroValueDefaultsEnvVarNames proves the zero-value Config
// resolves to DefaultEnvVarNames. Callers that don't need to override
// the env var prefix can pass Config{} (or Config{DebugOptions: ...})
// and get madder's BIN_MADDER / MADDER_* contract automatically.
func TestConfig_ZeroValueDefaultsEnvVarNames(t *testing.T) {
	cfg := Config{}

	if got := cfg.envVarNamesOrDefault(); got != DefaultEnvVarNames {
		t.Errorf("Config{}.envVarNamesOrDefault() = %+v, want %+v",
			got, DefaultEnvVarNames)
	}
}

// TestConfig_ExplicitEnvVarNamesPreserved proves that any non-zero
// EnvVarNames field defeats defaulting: the entire bundle the caller
// supplied is taken as-is. Matches the prior WithEnvVarNames semantics
// (whole-bundle replacement; no partial-field merge with defaults).
func TestConfig_ExplicitEnvVarNamesPreserved(t *testing.T) {
	custom := EnvVarNames{
		Binary:             "X_BIN",
		XDGUtilityOverride: "X_OVERRIDE",
		VerifyOnCollision:  "X_VERIFY",
	}
	cfg := Config{EnvVarNames: custom}

	if got := cfg.envVarNamesOrDefault(); got != custom {
		t.Errorf("envVarNamesOrDefault() = %+v, want %+v", got, custom)
	}
}
