//go:build test

package directory_layout

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveNthAncestorMatch_RanksMatchesDeepestFirst pins the dodder#281
// operate-path resolver: depth indexes the deepest-first matching ancestors,
// and overflow errors (does not clamp).
func TestResolveNthAncestorMatch_RanksMatchesDeepestFirst(t *testing.T) {
	root := t.TempDir()
	mid := filepath.Join(root, "mid")
	leaf := filepath.Join(mid, "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{root, mid, leaf} {
		if err := os.MkdirAll(filepath.Join(dir, ".madder"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	ceilings := []string{filepath.Dir(root)}
	matchAll := func(string) bool { return true }

	// FindAllCwdOverridePaths(leaf) = [leaf, mid, root]; depth indexes it.
	cases := []struct {
		depth uint
		want  string
	}{
		{0, leaf},
		{1, mid},
		{2, root},
	}
	for _, tc := range cases {
		got, err := ResolveNthAncestorMatch(leaf, "madder", tc.depth, ceilings, matchAll)
		if err != nil {
			t.Fatalf("depth %d: unexpected error: %v", tc.depth, err)
		}
		if got != tc.want {
			t.Errorf("depth %d: got %q, want %q", tc.depth, got, tc.want)
		}
	}

	// Only 3 matching ancestors — depth 3 errors rather than clamping.
	if _, err := ResolveNthAncestorMatch(leaf, "madder", 3, ceilings, matchAll); err == nil {
		t.Fatal("expected overflow error at depth 3, got nil")
	}
}

// TestResolveNthAncestorMatch_SkipsNonMatchingAncestors proves the
// "Nth SAME-NAMED ancestor" semantics: only ancestors the caller's matches
// predicate accepts are ranked, so a non-matching deeper ancestor is skipped.
func TestResolveNthAncestorMatch_SkipsNonMatchingAncestors(t *testing.T) {
	root := t.TempDir()
	mid := filepath.Join(root, "mid")
	leaf := filepath.Join(mid, "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{root, mid, leaf} {
		if err := os.MkdirAll(filepath.Join(dir, ".madder"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	ceilings := []string{filepath.Dir(root)}
	// matches only mid and root — the deepest ancestor (leaf) is skipped.
	matchesMidRoot := func(p string) bool { return p == mid || p == root }

	got, err := ResolveNthAncestorMatch(leaf, "madder", 0, ceilings, matchesMidRoot)
	if err != nil {
		t.Fatalf("depth 0: unexpected error: %v", err)
	}
	if got != mid {
		t.Errorf("depth 0 (leaf skipped): got %q, want %q", got, mid)
	}

	got, err = ResolveNthAncestorMatch(leaf, "madder", 1, ceilings, matchesMidRoot)
	if err != nil {
		t.Fatalf("depth 1: unexpected error: %v", err)
	}
	if got != root {
		t.Errorf("depth 1: got %q, want %q", got, root)
	}

	// Only 2 matches → depth 2 errors.
	if _, err := ResolveNthAncestorMatch(leaf, "madder", 2, ceilings, matchesMidRoot); err == nil {
		t.Fatal("expected overflow error at depth 2 (only 2 matches), got nil")
	}
}
