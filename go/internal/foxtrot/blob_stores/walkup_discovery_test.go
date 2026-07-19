//go:build test

package blob_stores

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"code.linenisgreat.com/hyphence/go/hyphence"
	"code.linenisgreat.com/madder/go/internal/0/ids"
	"code.linenisgreat.com/madder/go/internal/bravo/directory_layout"
	"code.linenisgreat.com/madder/go/internal/charlie/blob_store_configs"
	delta_blob_store_configs "code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/madder/go/internal/echo/env_dir"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
)

// writeStoreConfig writes a minimal V3 hash-bucketed config to
// `<madderDir>/local/share/blob_stores/<name>/blob_store-config`.
func writeStoreConfig(t *testing.T, madderDir, name string) {
	t.Helper()

	storeDir := filepath.Join(madderDir, "local", "share", "blob_stores", name)
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(storeDir, directory_layout.FileNameBlobStoreConfig)

	typedBlob := &delta_blob_store_configs.TypedConfig{
		Type: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigV3).TypeStruct,
		Blob: &blob_store_configs.TomlV3{
			HashTypeId:      "sha256",
			HashBuckets:     []int{2},
			CompressionType: "none",
		},
	}

	if err := hyphence.EncodeToFile(
		delta_blob_store_configs.Coder,
		typedBlob,
		configPath,
	); err != nil {
		t.Fatalf("encode %s: %v", configPath, err)
	}
}

// setupAncestorTree builds a temp tree with `.madder/` directories at
// every parent in `ancestorStores` (deepest-first by map iteration is
// fine — the per-ancestor list of names is what matters). Returns
// rootForCeiling, leafCWD, cleanup, and the deepest path used.
//
// `ancestorStores` keys are relative path segments under root, "." for
// root itself, "sub" for `<root>/sub`, "sub/leaf" for `<root>/sub/leaf`,
// etc. Values are the store names to seed under each `.madder/`.
func setupAncestorTree(
	t *testing.T,
	ancestorStores map[string][]string,
	leafSubPath string,
) (rootCeiling, leafCWD string) {
	t.Helper()

	root := t.TempDir()
	rootCeiling = filepath.Dir(root)

	for relPath, stores := range ancestorStores {
		ancestor := root
		if relPath != "." {
			ancestor = filepath.Join(root, relPath)
		}
		madderDir := filepath.Join(ancestor, ".madder")
		if err := os.MkdirAll(madderDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range stores {
			writeStoreConfig(t, madderDir, name)
		}
	}

	leafCWD = filepath.Join(root, leafSubPath)
	if err := os.MkdirAll(leafCWD, 0o755); err != nil {
		t.Fatal(err)
	}

	return rootCeiling, leafCWD
}

// chdirAndMakeEnv chdirs to dir, registers a t.Cleanup to restore, and
// builds a `madder`-scoped env_dir whose XDG walks up from dir.
func chdirAndMakeEnv(t *testing.T, ceiling, dir string) env_dir.Env {
	t.Helper()

	t.Setenv("MADDER_CEILING_DIRECTORIES", ceiling)

	saved, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(saved) })

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %q: %v", dir, err)
	}

	return env_dir.MakeDefault(
		errors.MakeContextDefault(),
		env_dir.Config{},
		"madder",
	)
}

// makeLayoutFor builds a directory_layout.BlobStore for the env's
// blob-store XDG (mirrors blob_store_env.makeBlobStoreEnvBase).
func makeLayoutFor(t *testing.T, env env_dir.Env) directory_layout.BlobStore {
	t.Helper()
	layout, err := directory_layout.MakeBlobStore(env.GetXDGForBlobStores())
	if err != nil {
		t.Fatalf("MakeBlobStore: %v", err)
	}
	return layout
}

// keysOf returns the sorted keys of a BlobStoreMap so test diagnostics
// are deterministic.
func keysOf(m BlobStoreMap) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestMakeAncestorOverrideStores_DiscoversAllAncestors pins #145: each
// `.madder/` ancestor on the walk-up chain contributes its CWD-prefix
// stores. Same-name stores at shallower ancestors get extra `.`
// prefixes (`..rsync_dot_net` for depth-1).
func TestMakeAncestorOverrideStores_DiscoversAllAncestors(t *testing.T) {
	ceiling, leaf := setupAncestorTree(
		t,
		map[string][]string{
			".":   {"default", "rsync_dot_net"},
			"sub": {"default"},
		},
		filepath.Join("sub", "leaf"),
	)

	env := chdirAndMakeEnv(t, ceiling, leaf)
	layout := makeLayoutFor(t, env)

	stores := makeAncestorOverrideStores(
		errors.MakeContextDefault(),
		env,
		layout,
	)

	got := keysOf(stores)
	want := []string{".default", "..default", ".rsync_dot_net"}
	sort.Strings(want)

	if !equalStringSlices(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
}

// TestMakeAncestorOverrideStores_UniqueNamesGetSingleDot pins the
// "minimal disambiguation" rule: a name that exists at only one
// ancestor — even a shallow one — uses a single dot.
func TestMakeAncestorOverrideStores_UniqueNamesGetSingleDot(t *testing.T) {
	ceiling, leaf := setupAncestorTree(
		t,
		map[string][]string{
			".":   {"home_only"},
			"sub": {"sub_only"},
		},
		filepath.Join("sub", "leaf"),
	)

	env := chdirAndMakeEnv(t, ceiling, leaf)
	layout := makeLayoutFor(t, env)

	stores := makeAncestorOverrideStores(
		errors.MakeContextDefault(),
		env,
		layout,
	)

	got := keysOf(stores)
	want := []string{".home_only", ".sub_only"}
	sort.Strings(want)

	if !equalStringSlices(got, want) {
		t.Errorf("keys = %v, want %v (unique names get single dot regardless of depth)",
			got, want)
	}
}

// TestMakeAncestorOverrideStores_RespectsCeiling pins git-style
// ceiling semantics (matching the post-purse-first#75 dewey): the
// ceiling dir itself IS in the walk, but anything strictly above is
// not. With ceiling = mid, the walk visits mid (at-ceiling) and leaf
// (below) but never aboveCeiling (strictly above).
func TestMakeAncestorOverrideStores_RespectsCeiling(t *testing.T) {
	aboveCeiling := t.TempDir()
	ceiling := filepath.Join(aboveCeiling, "ceiling")
	mid := filepath.Join(ceiling, "mid")
	leaf := filepath.Join(mid, "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}

	aboveMadder := filepath.Join(aboveCeiling, ".madder")
	if err := os.MkdirAll(aboveMadder, 0o755); err != nil {
		t.Fatal(err)
	}
	writeStoreConfig(t, aboveMadder, "above_ceiling")

	ceilingMadder := filepath.Join(ceiling, ".madder")
	if err := os.MkdirAll(ceilingMadder, 0o755); err != nil {
		t.Fatal(err)
	}
	writeStoreConfig(t, ceilingMadder, "at_ceiling")

	midMadder := filepath.Join(mid, ".madder")
	if err := os.MkdirAll(midMadder, 0o755); err != nil {
		t.Fatal(err)
	}
	writeStoreConfig(t, midMadder, "below_ceiling")

	env := chdirAndMakeEnv(t, ceiling, leaf)
	layout := makeLayoutFor(t, env)

	stores := makeAncestorOverrideStores(
		errors.MakeContextDefault(),
		env,
		layout,
	)

	got := keysOf(stores)
	want := []string{".at_ceiling", ".below_ceiling"}

	if !equalStringSlices(got, want) {
		t.Errorf("keys = %v, want %v (ceiling dir IS in the walk, strictly-above is not)",
			got, want)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
