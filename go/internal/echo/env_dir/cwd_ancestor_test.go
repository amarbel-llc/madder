//go:build test

package env_dir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

// TestResolveCwdAncestorOrError_LiteralWalkUp pins madder#153's model (A):
// each dot beyond the first walks up exactly one literal parent directory,
// with no `.<scope>/` store-existence check. depth 0 returns cwd unchanged.
func TestResolveCwdAncestorOrError_LiteralWalkUp(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	bravo := filepath.Join(alpha, "bravo")
	charlie := filepath.Join(bravo, "charlie")
	if err := os.MkdirAll(charlie, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		depth uint
		want  string
	}{
		{0, charlie},
		{1, bravo},
		{2, alpha},
		{3, root},
	}

	for _, tc := range cases {
		got, err := resolveCwdAncestorOrError(charlie, tc.depth, nil)
		if err != nil {
			t.Fatalf("depth %d: unexpected error: %v", tc.depth, err)
		}
		if got != tc.want {
			t.Errorf("depth %d: got %q, want %q", tc.depth, got, tc.want)
		}
	}
}

// TestResolveCwdAncestorOrError_OverflowRootErrors pins the (error)-not-
// (clamp) ruling: a depth exceeding the parents available before the
// filesystem root errors rather than silently clamping at root. The walk
// is a pure string operation when ceilings is nil, so a synthetic shallow
// path makes the overflow deterministic.
func TestResolveCwdAncestorOrError_OverflowRootErrors(t *testing.T) {
	if _, err := resolveCwdAncestorOrError("/alpha/bravo", 10, nil); err == nil {
		t.Fatal("expected overflow error walking 10 parents up from /alpha/bravo, got nil")
	}
}

// TestResolveCwdAncestorOrError_CeilingBounds pins that the walk-up may
// reach the ceiling directory itself but errors when a depth would carry
// it above the ceiling (matching FindAllCwdOverridePaths' git-style
// GIT_CEILING_DIRECTORIES boundary).
func TestResolveCwdAncestorOrError_CeilingBounds(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	bravo := filepath.Join(alpha, "bravo")
	charlie := filepath.Join(bravo, "charlie")
	if err := os.MkdirAll(charlie, 0o755); err != nil {
		t.Fatal(err)
	}

	ceilings := []string{root}

	// depth 3 reaches exactly root (the ceiling) — allowed.
	got, err := resolveCwdAncestorOrError(charlie, 3, ceilings)
	if err != nil {
		t.Fatalf("depth 3 to the ceiling itself should succeed, got: %v", err)
	}
	if got != root {
		t.Errorf("depth 3: got %q, want ceiling %q", got, root)
	}

	// depth 4 would step above the ceiling — error.
	if _, err := resolveCwdAncestorOrError(charlie, 4, ceilings); err == nil {
		t.Fatal("expected error walking above the ceiling at depth 4, got nil")
	}
}

// TestMakeDefaultAndInitialize_MultiDotResolvesAncestors is the madder#153
// integration test: a multi-dot Cwd id resolves MakeDefaultAndInitialize's
// XDG override to the literal Nth-parent of the working directory.
func TestMakeDefaultAndInitialize_MultiDotResolvesAncestors(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "dir_alpha")
	bravo := filepath.Join(alpha, "dir_bravo")
	charlie := filepath.Join(bravo, "dir_charlie")
	if err := os.MkdirAll(charlie, 0o755); err != nil {
		t.Fatal(err)
	}

	// Neutralize any ambient ceiling so the literal walk-up is bounded
	// only by the filesystem root (never reached at these depths).
	t.Setenv("MADDER_CEILING_DIRECTORIES", "")

	saved, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(saved) })
	if err := os.Chdir(charlie); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		id      string
		present string // ancestor segment the override must root at
		absent  string // a deeper segment that must NOT appear
	}{
		{"single dot roots at cwd", ".myrepo", "dir_charlie", ""},
		{"double dot roots at parent", "..myrepo", "dir_bravo", "dir_charlie"},
		{"triple dot roots at grandparent", "...myrepo", "dir_alpha", "dir_bravo"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var id scoped_id.Id
			if err := id.Set(tc.id); err != nil {
				t.Fatalf("Set(%q): %v", tc.id, err)
			}

			env := MakeDefaultAndInitialize(
				errors.MakeContextDefault(),
				Config{},
				"madder",
				id,
			)

			if !env.XDG.IsOverridden() {
				t.Fatalf("expected XDG overridden for id %q", tc.id)
			}

			dataPath := env.XDG.Data.MakePath().String()
			if !strings.Contains(dataPath, tc.present) {
				t.Errorf("data path %q does not root at expected ancestor %q",
					dataPath, tc.present)
			}
			if tc.absent != "" && strings.Contains(dataPath, tc.absent) {
				t.Errorf("data path %q still contains deeper segment %q — walk-up did not happen",
					dataPath, tc.absent)
			}
		})
	}
}
