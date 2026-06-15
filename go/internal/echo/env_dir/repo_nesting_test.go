//go:build test

package env_dir

import (
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
)

// TestBlobStoreXDG_NestsUnderRepoName is the madder#240 regression test:
// a named repo (Config.RepoName) nests its blob-store XDG under
// repos/<name>/ so it gets an isolated blob pool, and an unnamed env does
// NOT nest. It exercises every blob-XDG boundary that re-applies the nest
// after its own XDG re-derivation: GetXDGForBlobStores, the without-
// override and with-override-path accessors (discovery's user/ancestor
// paths), and GetXDGForBlobStoreId (user + cache scopes).
//
// Paths are compared named-vs-unnamed rather than by substring: the test
// runs inside the madder repo, whose path literally contains "/repos/",
// so a bare Contains check would false-positive (see the same caveat in
// multi_scope_test.go). Sandboxed under t.TempDir() via root override.
func TestBlobStoreXDG_NestsUnderRepoName(t *testing.T) {
	root := t.TempDir()

	makeEnv := func(repoName string) env {
		return MakeWithXDGRootOverrideHomeAndInitialize(
			errors.MakeContextDefault(),
			Config{RepoName: repoName},
			"madder",
			root,
		)
	}

	var userId, cacheId scoped_id.Id
	if err := userId.Set("default"); err != nil {
		t.Fatal(err)
	}
	if err := cacheId.Set("%scratch"); err != nil {
		t.Fatal(err)
	}

	named := makeEnv("foo")
	unnamed := makeEnv("")

	accessors := []struct {
		name string
		path func(e env) string
	}{
		{"GetXDGForBlobStores", func(e env) string {
			return e.GetXDGForBlobStores().Data.MakePath("blob_stores").String()
		}},
		{"GetXDGForBlobStoresWithoutOverride", func(e env) string {
			return e.GetXDGForBlobStoresWithoutOverride().Data.MakePath("blob_stores").String()
		}},
		{"GetXDGForBlobStoresWithOverridePath", func(e env) string {
			return e.GetXDGForBlobStoresWithOverridePath(root).Data.MakePath("blob_stores").String()
		}},
		{"GetXDGForBlobStoreId(user)", func(e env) string {
			return e.GetXDGForBlobStoreId(userId).Data.MakePath("blob_stores").String()
		}},
		{"GetXDGForBlobStoreId(cache)", func(e env) string {
			return e.GetXDGForBlobStoreId(cacheId).Cache.MakePath("blob_stores").String()
		}},
	}

	for _, a := range accessors {
		namedPath := a.path(named)
		unnamedPath := a.path(unnamed)

		// The named path is exactly the unnamed path with repos/foo
		// inserted before the trailing blob_stores segment.
		want := strings.TrimSuffix(unnamedPath, "blob_stores") + "repos/foo/blob_stores"
		if namedPath != want {
			t.Errorf("%s: named = %q, want %q (unnamed base: %q)",
				a.name, namedPath, want, unnamedPath)
		}

		// "repos/foo" is unambiguous (the fixture prefix is repos/madder),
		// so this guards that the unnamed env did not nest.
		if strings.Contains(unnamedPath, "repos/foo") {
			t.Errorf("%s: unnamed leaked repos/foo: %q", a.name, unnamedPath)
		}
	}
}
