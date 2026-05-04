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
// wire; this test confirms the bundle's fields point at the right
// constants AND that XDGUtilityOverride is intentionally empty
// (madder does not honor any XDG-scope-override env var; see
// package doc-comment).
func TestDefaultEnvVarNames(t *testing.T) {
	want := env_dir.EnvVarNames{
		Binary:            EnvBin,
		VerifyOnCollision: EnvVerifyOnCollision,
		// XDGUtilityOverride: "" — madder defines no override env var
	}

	if DefaultEnvVarNames != want {
		t.Errorf("DefaultEnvVarNames = %+v, want %+v",
			DefaultEnvVarNames, want)
	}

	if DefaultEnvVarNames.XDGUtilityOverride != "" {
		t.Errorf("madder must not define an XDG-utility-override env var; "+
			"got XDGUtilityOverride = %q", DefaultEnvVarNames.XDGUtilityOverride)
	}
}
