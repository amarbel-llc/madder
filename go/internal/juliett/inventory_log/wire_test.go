package inventory_log

import (
	"testing"

	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// TestWireDefault_EnvDisableReturnsNop pins the disable contract:
// MADDER_INVENTORY_LOG=0 (exact "0", trim-space tolerant) makes
// WireDefault return a NopObserver. Mirrors the CLI side of the
// disable flow — both paths must agree so importers and madder users
// share semantics.
func TestWireDefault_EnvDisableReturnsNop(t *testing.T) {
	t.Setenv(envVarDisable, "0")

	ctx := errors.MakeContextDefault()

	obs := WireDefault(ctx)

	if _, ok := obs.(NopObserver); !ok {
		t.Errorf("expected NopObserver under MADDER_INVENTORY_LOG=0, got %T", obs)
	}
}

// TestWireDefault_EnvDisableTrimsWhitespace covers a leading/trailing
// space in the env var — strings.TrimSpace is documented in the
// disable contract.
func TestWireDefault_EnvDisableTrimsWhitespace(t *testing.T) {
	t.Setenv(envVarDisable, "  0  ")

	ctx := errors.MakeContextDefault()

	obs := WireDefault(ctx)

	if _, ok := obs.(NopObserver); !ok {
		t.Errorf("expected NopObserver with whitespace-padded \"0\", got %T", obs)
	}
}

// TestWireDefault_EnvOtherValueDoesNotDisable confirms that any value
// other than "0" leaves the observer enabled. "false" / "off" /
// "disable" should NOT disable the log — the contract is exact "0",
// for parity with the CLI.
func TestWireDefault_EnvOtherValueDoesNotDisable(t *testing.T) {
	for _, v := range []string{"", "1", "false", "off"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(envVarDisable, v)

			ctx := errors.MakeContextDefault()

			obs := WireDefault(ctx)

			if _, ok := obs.(NopObserver); ok {
				t.Errorf("expected non-Nop observer for env=%q, got NopObserver", v)
			}
		})
	}
}

// TestWireWithCleanup_EnvDisableReturnsNopAndNoOpCleanup pins the
// equivalent contract for the cleanup-style entry point: NopObserver
// plus a cleanup that returns nil so callers' `defer cleanup()` is
// safe even on the disabled path.
func TestWireWithCleanup_EnvDisableReturnsNopAndNoOpCleanup(t *testing.T) {
	t.Setenv(envVarDisable, "0")

	obs, cleanup := WireWithCleanup()

	if _, ok := obs.(NopObserver); !ok {
		t.Errorf("expected NopObserver under MADDER_INVENTORY_LOG=0, got %T", obs)
	}
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup func")
	}
	if err := cleanup(); err != nil {
		t.Errorf("expected nil from no-op cleanup, got %v", err)
	}
}

// TestWireWithCleanup_EnabledReturnsFileObserver guards the happy
// path: a real FileObserver is returned and its Close (cleanup) does
// not error on a clean shutdown.
func TestWireWithCleanup_EnabledReturnsFileObserver(t *testing.T) {
	t.Setenv(envVarDisable, "")
	t.Setenv("XDG_LOG_HOME", t.TempDir())

	obs, cleanup := WireWithCleanup()
	defer cleanup() //nolint:errcheck // covered below

	if _, ok := obs.(*FileObserver); !ok {
		t.Errorf("expected *FileObserver, got %T", obs)
	}
	if err := cleanup(); err != nil {
		t.Errorf("cleanup: %v", err)
	}
}
