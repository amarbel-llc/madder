//go:build test

package madder_env

import (
	"testing"

	"github.com/amarbel-llc/madder/go/internal/echo/env_dir"
)

// TestEnvBin pins the env-var-name constant that madder publishes to
// subprocesses. User-visible contract — external scripts wrap madder
// and read $BIN_MADDER.
func TestEnvBin(t *testing.T) {
	if EnvBin != "BIN_MADDER" {
		t.Errorf("EnvBin = %q, want %q", EnvBin, "BIN_MADDER")
	}
}

// TestOverrideEnvVarName pins the override env-var name. User-visible:
// `MADDER_XDG_UTILITY_OVERRIDE=alt madder ...` redirects madder's
// XDG scope.
func TestOverrideEnvVarName(t *testing.T) {
	if OverrideEnvVarName != "MADDER_XDG_UTILITY_OVERRIDE" {
		t.Errorf("OverrideEnvVarName = %q, want %q",
			OverrideEnvVarName, "MADDER_XDG_UTILITY_OVERRIDE")
	}
}

// TestEnvVerifyOnCollision pins the verify-on-collision env-var name.
// See ADR 0003 / #38.
func TestEnvVerifyOnCollision(t *testing.T) {
	if EnvVerifyOnCollision != "MADDER_VERIFY_ON_COLLISION" {
		t.Errorf("EnvVerifyOnCollision = %q, want %q",
			EnvVerifyOnCollision, "MADDER_VERIFY_ON_COLLISION")
	}
}

// TestDefaultEnvVarNames pins the bundle madder commands pass into
// env_dir.Config.EnvVarNames. The constants alone are easy to mis-
// wire (assigning OverrideEnvVarName to Binary, etc.); this test
// confirms the bundle's three fields point at the right constants.
func TestDefaultEnvVarNames(t *testing.T) {
	want := env_dir.EnvVarNames{
		Binary:             EnvBin,
		XDGUtilityOverride: OverrideEnvVarName,
		VerifyOnCollision:  EnvVerifyOnCollision,
	}

	if DefaultEnvVarNames != want {
		t.Errorf("DefaultEnvVarNames = %+v, want %+v",
			DefaultEnvVarNames, want)
	}
}
