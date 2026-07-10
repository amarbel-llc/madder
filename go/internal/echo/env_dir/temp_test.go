//go:build test

package env_dir

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
)

// TestMakeWithXDG_RegistersTempCleanup is the regression test for
// madder#239. MakeWithXDG must register the resetTempOnExit After hook
// like its sibling constructors (MakeWithDefaultHome,
// MakeWithXDGRootOverrideHomeAndInitialize, MakeWithHomeAndInitialize);
// otherwise an env built from an externally-supplied xdg.XDG (e.g. via
// MakeFromXDGDotenvPath) leaks its per-pid temp dir on context exit.
//
// Sandboxed under t.TempDir() via the root-override constructor, so the
// test never touches the host's real XDG paths.
func TestMakeWithXDG_RegistersTempCleanup(t *testing.T) {
	root := t.TempDir()

	// Obtain an initialized xdg.XDG via a sibling constructor, then hand
	// it to MakeWithXDG — the constructor under test.
	seed := MakeWithXDGRootOverrideHomeAndInitialize(
		errors.MakeContextDefault(),
		Config{},
		"madder",
		root,
	)

	ctx := errors.MakeContextDefault()
	env := MakeWithXDG(ctx, Config{}, seed.GetXDG())

	tempDir := env.GetTempLocal().BasePath
	if tempDir == "" {
		t.Fatal("MakeWithXDG produced an empty temp dir path")
	}
	if _, err := os.Stat(tempDir); err != nil {
		t.Fatalf("temp dir not created by MakeWithXDG: %v", err)
	}

	// Completing the context fires the After hooks, including
	// resetTempOnExit. Without the fix no hook is registered, so the
	// temp dir survives.
	if err := ctx.Run(func(errors.Context) {}); err != nil {
		t.Fatalf("ctx.Run: %v", err)
	}

	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Errorf(
			"temp dir %q still present after context completion; "+
				"MakeWithXDG did not register resetTempOnExit",
			tempDir,
		)
	}
}

// TestMakeWithXDGRootOverrideHomeNoInit_NoMkdir is the madder#260
// regression test: the no-init counterpart to
// MakeWithXDGRootOverrideHomeAndInitialize must compute the XDG paths
// (GetXDG().Data.ActualValue etc.) for a possibly-nonexistent root
// WITHOUT creating any directories — mirroring the MakeDefault /
// MakeDefaultNoInit pairing (initialize=false skips initializeXDG's
// mkdir). Root is a path under a t.TempDir() that is never created, so
// any mkdir side effect would be observable via os.Stat.
func TestMakeWithXDGRootOverrideHomeNoInit_NoMkdir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "does-not-exist")

	env := MakeWithXDGRootOverrideHomeNoInit(
		errors.MakeContextDefault(),
		Config{},
		"madder",
		root,
	)

	dataDir := env.GetXDG().Data.ActualValue
	if dataDir == "" {
		t.Fatal("GetXDG().Data.ActualValue is empty")
	}

	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("root %q was created; expected no mkdir side effect", root)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Errorf("data dir %q was created; expected no mkdir side effect", dataDir)
	}
}
