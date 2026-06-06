//go:build test

package command_components

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/ids"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	_ "github.com/amarbel-llc/madder/go/internal/bravo/plugins/builtins"
	_ "github.com/amarbel-llc/madder/go/internal/charlie/markl_registrations"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/delta/env_ui"
	"github.com/amarbel-llc/madder/go/internal/echo/env_dir"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_store_env"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
)

// makeEnvLocalAt chdirs into dir (restored via t.Cleanup), points
// XDG_DATA_HOME at xdgDataHome, caps the ancestor walk-up at ceiling,
// and builds a madder-scoped env_local whose env_dir XDG resolves from
// dir — so an ancestor `.madder/` override applies when one exists.
func makeEnvLocalAt(
	t *testing.T,
	ceiling, dir, xdgDataHome string,
) env_local.Env {
	t.Helper()

	t.Setenv("MADDER_CEILING_DIRECTORIES", ceiling)
	t.Setenv("XDG_DATA_HOME", xdgDataHome)

	saved, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(saved) })

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %q: %v", dir, err)
	}

	ctx := errors.MakeContextDefault()

	dirEnv := env_dir.MakeDefault(ctx, env_dir.Config{}, "madder")

	return env_local.Make(env_ui.MakeDefault(ctx), &dirEnv)
}

// makeDefaultTypedConfig mirrors the `init` command's registration
// defaults (commands/init.go).
func makeDefaultTypedConfig() *blob_store_configs.TypedConfig {
	return &blob_store_configs.TypedConfig{
		Type: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigVCurrent).TypeStruct,
		Blob: &blob_store_configs.DefaultType{
			HashTypeId:      blob_store_configs.HashTypeDefault,
			HashBuckets:     blob_store_configs.DefaultHashBuckets,
			CompressionType: "zstd",
		},
	}
}

// TestInitBlobStore_UnprefixedIdIgnoresAncestorOverride pins #227: an
// unprefixed blob-store-id selects the XDG user scope (blob-store(7)),
// and `write` resolves it there via discovery's CloneWithoutOverride
// branch — so `init` must create it there too, even when the CWD has
// an ancestor `.madder/` that overrides the env's XDG. Before the fix,
// init silently retargeted the ancestor store root and registered the
// store dot-prefixed, so an init→write round trip failed from inside
// any worktree.
func TestInitBlobStore_UnprefixedIdIgnoresAncestorOverride(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".madder"), 0o755); err != nil {
		t.Fatal(err)
	}
	leaf := filepath.Join(root, "sub", "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	xdgDataHome := t.TempDir()

	envLocal := makeEnvLocalAt(t, filepath.Dir(root), leaf, xdgDataHome)
	envBlobStore := blob_store_env.MakeBlobStoreEnvWithoutStores(envLocal)

	// Sanity: the ancestor `.madder/` must actually override the env's
	// XDG, otherwise this test exercises the trivial path.
	if !envBlobStore.GetXDG().IsOverridden() {
		t.Fatal("test setup: env XDG is not ancestor-overridden")
	}

	var id blob_store_id.Id
	if err := id.Set("xdg-roundtrip"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var cmd Init
	path := cmd.InitBlobStore(
		envLocal,
		envBlobStore,
		id,
		makeDefaultTypedConfig(),
	)

	wantBase := filepath.Join(xdgDataHome, "madder", "blob_stores", "xdg-roundtrip")
	if path.GetBase() != wantBase {
		t.Errorf(
			"init created store at %q, want XDG user scope %q",
			path.GetBase(), wantBase,
		)
	}
	if strings.Contains(path.GetConfig(), ".madder") {
		t.Errorf(
			"init config path %q landed inside an ancestor .madder",
			path.GetConfig(),
		)
	}
	if got := path.GetId().String(); got != "xdg-roundtrip" {
		t.Errorf(
			"init registered id %q, want unprefixed %q",
			got, "xdg-roundtrip",
		)
	}

	// The actual #227 contract: discovery (what `write` uses to
	// resolve store-switch args) must find the store under its
	// unprefixed id.
	envDiscovery := blob_store_env.MakeBlobStoreEnv(envLocal)
	if _, ok := envDiscovery.GetBlobStores()["xdg-roundtrip"]; !ok {
		t.Errorf(
			"discovery cannot resolve %q after init; available: %v",
			"xdg-roundtrip", storeKeys(envDiscovery),
		)
	}
}

// TestInitBlobStore_CwdIdStillLandsInCurrentDir guards the explicit
// `.`-prefix contract while #227 reshapes the non-Cwd path: a
// dot-prefixed id creates under the *current* directory's `.madder/`,
// not the ancestor override and not XDG.
func TestInitBlobStore_CwdIdStillLandsInCurrentDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".madder"), 0o755); err != nil {
		t.Fatal(err)
	}
	leaf := filepath.Join(root, "sub", "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	xdgDataHome := t.TempDir()

	envLocal := makeEnvLocalAt(t, filepath.Dir(root), leaf, xdgDataHome)
	envBlobStore := blob_store_env.MakeBlobStoreEnvWithoutStores(envLocal)

	var id blob_store_id.Id
	if err := id.Set(".cwd-store"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var cmd Init
	path := cmd.InitBlobStore(
		envLocal,
		envBlobStore,
		id,
		makeDefaultTypedConfig(),
	)

	leafResolved, err := filepath.EvalSymlinks(leaf)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	baseResolved := path.GetBase()
	if resolved, err := filepath.EvalSymlinks(filepath.Dir(baseResolved)); err == nil {
		baseResolved = filepath.Join(resolved, filepath.Base(baseResolved))
	}

	if !strings.HasPrefix(baseResolved, leafResolved+string(os.PathSeparator)) {
		t.Errorf(
			"`.`-prefixed init created store at %q, want under current dir %q",
			path.GetBase(), leafResolved,
		)
	}
}

func storeKeys(env blob_store_env.BlobStoreEnv) []string {
	stores := env.GetBlobStores()
	keys := make([]string, 0, len(stores))
	for key := range stores {
		keys = append(keys, key)
	}
	return keys
}
