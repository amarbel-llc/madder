//go:build test

package env_dir

import (
	"strings"
	"testing"

	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
)

// TestMakeDefault_DistinctScopesAreIndependent is the multi-scope tracer:
// the same Config value, applied to two MakeWithXDGRootOverrideHomeAndInitialize
// calls with different xdgScope strings, must produce two env_dirs whose
// XDG paths are disjoint and whose UtilityName fields reflect the distinct
// scope arg each was constructed with.
//
// This is the architectural prerequisite for the multi-scope env_dir
// composition described in #123 (a wrapper utility holding both its own
// scope and a wrapped utility's scope at once). The full end-to-end
// pinning test — capture writing into madder's blob store AND a cg-scoped
// log under disjoint XDG roots — lives with Step 5 of the env_dir
// multi-scope plan.
//
// Sandboxed under t.TempDir() so the test never touches the host's real
// XDG paths and never mutates process env.
func TestMakeDefault_DistinctScopesAreIndependent(t *testing.T) {
	cfg := Config{}

	root := t.TempDir()

	madderEnv := MakeWithXDGRootOverrideHomeAndInitialize(
		errors.MakeContextDefault(),
		cfg,
		"madder",
		root,
	)
	cgEnv := MakeWithXDGRootOverrideHomeAndInitialize(
		errors.MakeContextDefault(),
		cfg,
		"cutting-garden",
		root,
	)

	if got, want := madderEnv.XDG.UtilityName, "madder"; got != want {
		t.Errorf("madderEnv.XDG.UtilityName = %q, want %q", got, want)
	}
	if got, want := cgEnv.XDG.UtilityName, "cutting-garden"; got != want {
		t.Errorf("cgEnv.XDG.UtilityName = %q, want %q", got, want)
	}

	// Disjoint XDG paths: each env's data path must, when made relative
	// to the shared root, contain its own scope name and not the other's.
	// Substring checks on the absolute path are unsafe — the test runs
	// inside the madder repo, so the path "/madder/" appears in the
	// fixture prefix and would false-positive a "leaked madder" assertion.
	// XDG-override builds dotfile-style segments under the override root
	// (e.g. "<root>/.cutting-garden/local/share").
	madderData := madderEnv.XDG.Data.MakePath().String()
	cgData := cgEnv.XDG.Data.MakePath().String()

	madderRel := strings.TrimPrefix(madderData, root)
	cgRel := strings.TrimPrefix(cgData, root)

	if !strings.Contains(madderRel, "madder") {
		t.Errorf("madder data rel-path %q missing madder segment", madderRel)
	}
	if !strings.Contains(cgRel, "cutting-garden") {
		t.Errorf("cg data rel-path %q missing cutting-garden segment", cgRel)
	}
	if madderRel == cgRel {
		t.Errorf("scopes share rel-path %q under root; expected disjoint", madderRel)
	}
	if strings.Contains(madderRel, "cutting-garden") {
		t.Errorf("madder rel-path leaked cg segment: %q", madderRel)
	}
	if strings.Contains(cgRel, "madder") {
		t.Errorf("cg rel-path leaked madder segment: %q", cgRel)
	}
}
