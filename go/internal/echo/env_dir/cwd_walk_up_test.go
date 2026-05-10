//go:build test

package env_dir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// TestMakeDefault_XDGUserLocationOnlyDisablesCwdWalkUp proves that
// setting the env var named by EnvVarNames.XDGUserLocationOnly to "1"
// short-circuits the cwd walk-up that InitializeOverriddenIfNecessary
// would otherwise perform — even when an ancestor `.madder/` exists.
// This is the embedder-friendly knob that lets callers (e.g. dodder
// bats, test harnesses, library users) opt out of walk-up behavior
// without managing a ceiling.
func TestMakeDefault_XDGUserLocationOnlyDisablesCwdWalkUp(t *testing.T) {
	root := t.TempDir()

	madderDir := filepath.Join(root, ".madder")
	if err := os.MkdirAll(madderDir, 0o755); err != nil {
		t.Fatalf("mkdir .madder: %v", err)
	}

	subdir := filepath.Join(root, "some-subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	// Same ceiling shape as the walk-up regression test: parent of
	// root so the walk could reach root in principle. The env var,
	// not the ceiling, is what disables it.
	t.Setenv("MADDER_CEILING_DIRECTORIES", filepath.Dir(root))

	// Use a test-scoped env-var name to keep this package independent
	// of madder_env (which would otherwise create a cyclic import).
	t.Setenv("X_USER_LOCATION_ONLY", "1")

	saved, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(saved) })

	if err := os.Chdir(subdir); err != nil {
		t.Fatalf("chdir subdir: %v", err)
	}

	env := MakeDefault(
		errors.MakeContextDefault(),
		Config{EnvVarNames: EnvVarNames{XDGUserLocationOnly: "X_USER_LOCATION_ONLY"}},
		"madder",
	)

	if env.XDG.IsOverridden() {
		t.Fatalf(
			"expected XDG NOT to be overridden when XDGUserLocationOnly=1; "+
				"walk-up should be short-circuited even with ancestor %q",
			madderDir,
		)
	}

	// Resolved data path must NOT be rooted at the ancestor's
	// `.madder/` (which is what a walk-up resolution would produce).
	dataPath := env.XDG.Data.MakePath().String()
	if strings.HasPrefix(dataPath, madderDir) {
		t.Errorf(
			"data path %q is rooted at the ancestor %q — walk-up was "+
				"NOT disabled by XDGUserLocationOnly",
			dataPath,
			madderDir,
		)
	}
}

// TestMakeDefault_CwdWalkUpFindsAncestorMadder is a regression test for #145.
// `madder list` from a subdir of a `.madder/`-rooted directory should
// resolve XDG paths against the ancestor (the directory containing
// `.madder/`), not against the literal CWD subdir.
func TestMakeDefault_CwdWalkUpFindsAncestorMadder(t *testing.T) {
	root := t.TempDir()

	madderDir := filepath.Join(root, ".madder")
	if err := os.MkdirAll(madderDir, 0o755); err != nil {
		t.Fatalf("mkdir .madder: %v", err)
	}

	subdir := filepath.Join(root, "some-subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	// Ceiling at root's parent so the walk can still check root itself
	// for `.madder/` (a ceiling at root would block that check).
	t.Setenv("MADDER_CEILING_DIRECTORIES", filepath.Dir(root))

	saved, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(saved) })

	if err := os.Chdir(subdir); err != nil {
		t.Fatalf("chdir subdir: %v", err)
	}

	env := MakeDefault(
		errors.MakeContextDefault(),
		Config{},
		"madder",
	)

	if !env.XDG.IsOverridden() {
		t.Fatalf(
			"expected XDG to be overridden after walking up to %q from %q",
			madderDir,
			subdir,
		)
	}

	// The data path is the only externally-visible signal of which
	// ancestor was chosen as the override base: it is rooted at
	// `<base>/.madder/local/share/`. If the walk-up is working, base
	// is `root` and the path does NOT contain the subdir segment.
	dataPath := env.XDG.Data.MakePath().String()
	if strings.Contains(dataPath, "some-subdir") {
		t.Errorf(
			"data path %q is rooted at the subdir; expected the ancestor "+
				"with .madder/ (%q) — walk-up did not happen",
			dataPath,
			root,
		)
	}
}
