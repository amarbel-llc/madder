//go:build test

package directory_layout

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// canonicalTempDir wraps t.TempDir() with filepath.EvalSymlinks so test
// expectations match the canonicalized form FindAllCwdOverridePaths now
// returns. On macOS the default $TMPDIR is /var/folders/... but the
// canonical path is /private/var/folders/...; without canonicalizing
// the fixture root, expected and actual strings disagree.
func canonicalTempDir(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval symlinks on tempdir: %v", err)
	}
	return resolved
}

func TestFindAllCwdOverridePaths_NoAncestors(t *testing.T) {
	root := canonicalTempDir(t)
	cwd := filepath.Join(root, "deep", "down")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	got := FindAllCwdOverridePaths(cwd, "madder", []string{filepath.Dir(root)})
	if len(got) != 0 {
		t.Errorf("expected no ancestors, got %v", got)
	}
}

func TestFindAllCwdOverridePaths_OneAncestor(t *testing.T) {
	root := canonicalTempDir(t)
	if err := os.MkdirAll(filepath.Join(root, ".madder"), 0o755); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(root, "sub", "leaf")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	got := FindAllCwdOverridePaths(cwd, "madder", []string{filepath.Dir(root)})
	want := []string{root}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFindAllCwdOverridePaths_MultipleAncestorsDeepestFirst(t *testing.T) {
	root := canonicalTempDir(t)
	mid := filepath.Join(root, "mid")
	leaf := filepath.Join(mid, "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{root, mid} {
		if err := os.MkdirAll(filepath.Join(dir, ".madder"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got := FindAllCwdOverridePaths(leaf, "madder", []string{filepath.Dir(root)})
	want := []string{mid, root}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v (expected deepest-first)", got, want)
	}
}

func TestFindAllCwdOverridePaths_AncestorWithMarkerIsItselfReturned(t *testing.T) {
	// CWD == an ancestor with `.madder/` itself; that ancestor must
	// appear in the list (depth 0).
	root := canonicalTempDir(t)
	if err := os.MkdirAll(filepath.Join(root, ".madder"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := FindAllCwdOverridePaths(root, "madder", []string{filepath.Dir(root)})
	want := []string{root}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFindAllCwdOverridePaths_CeilingCutsOffAncestor(t *testing.T) {
	// `.madder/` lives ABOVE the ceiling and must not be discovered.
	root := canonicalTempDir(t)
	mid := filepath.Join(root, "mid")
	leaf := filepath.Join(mid, "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".madder"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Ceiling at `mid` blocks any walk into `root`.
	got := FindAllCwdOverridePaths(leaf, "madder", []string{mid})
	if len(got) != 0 {
		t.Errorf("expected ceiling to block ancestor, got %v", got)
	}
}

func TestFindAllCwdOverridePaths_FileMarker(t *testing.T) {
	// `.<utility>` is a file rather than a directory — still counts.
	root := canonicalTempDir(t)
	leaf := filepath.Join(root, "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(root, ".madder"))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	got := FindAllCwdOverridePaths(leaf, "madder", []string{filepath.Dir(root)})
	want := []string{root}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestFindAllCwdOverridePaths_CeilingThroughSymlink pins git-style
// symlink resolution on ceiling entries. Without it the macOS default
// $TMPDIR (`/var/folders/...` → `/private/var/folders/...`) leaves
// ceiling and cwd in incompatible string forms and the walk runs past
// the ceiling unchallenged. Matches git's documented behavior for
// GIT_CEILING_DIRECTORIES.
func TestFindAllCwdOverridePaths_CeilingThroughSymlink(t *testing.T) {
	root := canonicalTempDir(t)
	canonicalCeiling := filepath.Join(root, "ceiling")
	if err := os.MkdirAll(canonicalCeiling, 0o755); err != nil {
		t.Fatal(err)
	}

	// Caller-side ceiling reaches the same dir through a symlink, so the
	// raw ceiling string differs from the canonical form embedded in cwd.
	symlinkedCeiling := filepath.Join(root, "ceiling_via_symlink")
	if err := os.Symlink(canonicalCeiling, symlinkedCeiling); err != nil {
		t.Skipf("symlink creation not supported: %v", err)
	}

	// `.madder` sits *above* the ceiling — must NOT be discovered when
	// the ceiling is honored.
	if err := os.MkdirAll(filepath.Join(root, ".madder"), 0o755); err != nil {
		t.Fatal(err)
	}

	mid := filepath.Join(canonicalCeiling, "mid")
	leaf := filepath.Join(mid, "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(mid, ".madder"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := FindAllCwdOverridePaths(leaf, "madder", []string{symlinkedCeiling})
	want := []string{mid}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v (ceiling-via-symlink must stop the walk)",
			got, want)
	}
}

