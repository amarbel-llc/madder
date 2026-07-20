//go:build test

package env_dir

import (
	"strings"
	"testing"

	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
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

// TestMetadataXDG_NestsUnderRepoName is the madder#241 regression test:
// a named repo nests its base (metadata) XDG — the tree dodder's
// config-seed / object-index / inventory-list / lock live under — beneath
// repos/<name>/ via GetXDG(), mirroring the blob-store accessors that #240
// already nest. An unnamed env stays flat. This convergence is what lets
// dodder delete its own NestUnderRepoName and delegate the whole layout to
// madder (FDR-0019 Phase 2 Option 2). All five categories nest.
//
// Same named-vs-unnamed comparison strategy as the #240 test above (the
// test runs inside .../repos/madder, so a bare "repos/" Contains check
// would false-positive); sandboxed under t.TempDir() via root override.
func TestMetadataXDG_NestsUnderRepoName(t *testing.T) {
	root := t.TempDir()

	makeEnv := func(repoName string) env {
		return MakeWithXDGRootOverrideHomeAndInitialize(
			errors.MakeContextDefault(),
			Config{RepoName: repoName},
			"madder",
			root,
		)
	}

	named := makeEnv("foo")
	unnamed := makeEnv("")

	categories := []struct {
		name string
		path func(e env) string
	}{
		{"Data", func(e env) string { return e.GetXDG().Data.MakePath("seed").String() }},
		{"Config", func(e env) string { return e.GetXDG().Config.MakePath("seed").String() }},
		{"State", func(e env) string { return e.GetXDG().State.MakePath("seed").String() }},
		{"Cache", func(e env) string { return e.GetXDG().Cache.MakePath("seed").String() }},
		{"Runtime", func(e env) string { return e.GetXDG().Runtime.MakePath("seed").String() }},
	}

	for _, c := range categories {
		namedPath := c.path(named)
		unnamedPath := c.path(unnamed)

		// Named path is exactly the unnamed path with repos/foo inserted
		// before the trailing seed segment.
		want := strings.TrimSuffix(unnamedPath, "seed") + "repos/foo/seed"
		if namedPath != want {
			t.Errorf("GetXDG().%s: named = %q, want %q (unnamed base: %q)",
				c.name, namedPath, want, unnamedPath)
		}

		if strings.Contains(unnamedPath, "repos/foo") {
			t.Errorf("GetXDG().%s: unnamed leaked repos/foo: %q", c.name, unnamedPath)
		}
	}
}
