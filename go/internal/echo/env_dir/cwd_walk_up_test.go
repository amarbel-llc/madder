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

	// HOME must point at a writable path because walk-up is about to
	// be disabled; the standard XDG fallback templates resolve to
	// $HOME/.{cache,local/share,...}/madder, and initializeXDG
	// MkdirAll's the cache. Nix sandbox $HOME=/homeless-shelter is
	// read-only, so without this the test fails in `nix build`.
	t.Setenv("HOME", root)

	// Test-scoped env-var name keeps this package independent of
	// madder_env (which would otherwise create a cyclic import).
	const userLocationOnlyEnv = "X_USER_LOCATION_ONLY"
	t.Setenv(userLocationOnlyEnv, "1")

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
		Config{EnvVarNames: EnvVarNames{XDGUserLocationOnly: userLocationOnlyEnv}},
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

// TestMakeDefault_XDGUserLocationOnly_NegativeCases proves that the
// walk-up still fires when the env var is unset, falsy, or when the
// bundle field is empty (so the env-var name is never resolved). Pairs
// with TestMakeDefault_XDGUserLocationOnlyDisablesCwdWalkUp.
func TestMakeDefault_XDGUserLocationOnly_NegativeCases(t *testing.T) {
	const userLocationOnlyEnv = "X_USER_LOCATION_ONLY"

	cases := []struct {
		name      string
		envValue  string // "" means unset
		bundle    EnvVarNames
		setEnvVar bool
	}{
		{
			name:      "empty bundle field, env=1 — env var name never resolved",
			envValue:  "1",
			bundle:    EnvVarNames{},
			setEnvVar: true,
		},
		{
			name:      "bundle field set, env unset — falls through to walk-up",
			envValue:  "",
			bundle:    EnvVarNames{XDGUserLocationOnly: userLocationOnlyEnv},
			setEnvVar: false,
		},
		{
			name:      "bundle field set, env=0 — falsy, walk-up fires",
			envValue:  "0",
			bundle:    EnvVarNames{XDGUserLocationOnly: userLocationOnlyEnv},
			setEnvVar: true,
		},
		{
			name:      "bundle field set, env=garbage — falsy, walk-up fires",
			envValue:  "maybe",
			bundle:    EnvVarNames{XDGUserLocationOnly: userLocationOnlyEnv},
			setEnvVar: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			madderDir := filepath.Join(root, ".madder")
			if err := os.MkdirAll(madderDir, 0o755); err != nil {
				t.Fatalf("mkdir .madder: %v", err)
			}
			subdir := filepath.Join(root, "some-subdir")
			if err := os.MkdirAll(subdir, 0o755); err != nil {
				t.Fatalf("mkdir subdir: %v", err)
			}

			t.Setenv("MADDER_CEILING_DIRECTORIES", filepath.Dir(root))
			if tc.setEnvVar {
				t.Setenv(userLocationOnlyEnv, tc.envValue)
			}

			saved, err := os.Getwd()
			if err != nil {
				t.Fatalf("getwd: %v", err)
			}
			t.Cleanup(func() { _ = os.Chdir(saved) })
			if err := os.Chdir(subdir); err != nil {
				t.Fatalf("chdir: %v", err)
			}

			env := MakeDefault(
				errors.MakeContextDefault(),
				Config{EnvVarNames: tc.bundle},
				"madder",
			)

			if !env.XDG.IsOverridden() {
				t.Errorf(
					"expected walk-up to fire (XDG overridden) for case %q; "+
						"got IsOverridden=false",
					tc.name,
				)
			}
		})
	}
}

// TestMakeDefault_XDGUserLocationOnly_AcceptsParseBoolEnvValues proves
// the truthiness check matches the package's parseBoolEnv helper, not a
// stricter "1"-only comparison. Same accepted set as VerifyOnCollision
// per env_dir convention.
func TestMakeDefault_XDGUserLocationOnly_AcceptsParseBoolEnvValues(t *testing.T) {
	const userLocationOnlyEnv = "X_USER_LOCATION_ONLY"

	for _, value := range []string{"1", "true", "yes", "on", "TRUE", "Yes"} {
		t.Run("value="+value, func(t *testing.T) {
			root := t.TempDir()
			madderDir := filepath.Join(root, ".madder")
			if err := os.MkdirAll(madderDir, 0o755); err != nil {
				t.Fatalf("mkdir .madder: %v", err)
			}
			subdir := filepath.Join(root, "some-subdir")
			if err := os.MkdirAll(subdir, 0o755); err != nil {
				t.Fatalf("mkdir subdir: %v", err)
			}

			t.Setenv("MADDER_CEILING_DIRECTORIES", filepath.Dir(root))
			// Walk-up is about to be disabled — point HOME at a writable
			// path so initializeXDG's MkdirAll on the cache succeeds in
			// sandboxed environments (nix build).
			t.Setenv("HOME", root)
			t.Setenv(userLocationOnlyEnv, value)

			saved, err := os.Getwd()
			if err != nil {
				t.Fatalf("getwd: %v", err)
			}
			t.Cleanup(func() { _ = os.Chdir(saved) })
			if err := os.Chdir(subdir); err != nil {
				t.Fatalf("chdir: %v", err)
			}

			env := MakeDefault(
				errors.MakeContextDefault(),
				Config{EnvVarNames: EnvVarNames{XDGUserLocationOnly: userLocationOnlyEnv}},
				"madder",
			)

			if env.XDG.IsOverridden() {
				t.Errorf(
					"expected walk-up disabled for env value %q; got IsOverridden=true",
					value,
				)
			}
		})
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
