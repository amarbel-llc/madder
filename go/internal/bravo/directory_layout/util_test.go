//go:build test

package directory_layout

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFindAllCwdOverridePaths_NoAncestors(t *testing.T) {
	root := t.TempDir()
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
	root := t.TempDir()
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
	root := t.TempDir()
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
	root := t.TempDir()
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
	root := t.TempDir()
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
	root := t.TempDir()
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
