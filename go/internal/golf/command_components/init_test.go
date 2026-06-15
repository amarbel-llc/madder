//go:build test

package command_components

import (
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/ids"
	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
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
//
// HOME and the remaining XDG vars are sandboxed under a temp dir:
// when no ancestor override applies, env_dir falls back to
// HOME-derived defaults and initializeXDG creates its tmp dir under
// the cache home — which must not touch the real $HOME (unwritable in
// the nix build sandbox, polluted otherwise).
func makeEnvLocalAt(
	t *testing.T,
	ceiling, dir, xdgDataHome string,
) env_local.Env {
	t.Helper()

	sandbox := t.TempDir()
	t.Setenv("HOME", filepath.Join(sandbox, "home"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(sandbox, "cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(sandbox, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(sandbox, "state"))
	t.Setenv("XDG_RUNTIME_HOME", filepath.Join(sandbox, "runtime"))

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

	var id scoped_id.Id
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

	var id scoped_id.Id
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

// recoverInitPanic runs f and returns the panic value, or nil if f did
// not panic. InitBlobStore rejects via envBlobStore.Cancel, which
// panics up through ContextContinueOrPanic outside a Run frame.
func recoverInitPanic(f func()) (r any) {
	defer func() { r = recover() }()
	f()
	return r
}

// TestInitBlobStore_RejectsScopesTheLayoutCannotRepresent pins the #230
// decision: id scopes the utility's store layout cannot represent are
// rejected with a clear error instead of silently retargeted. Under
// the `madder` (user-data) layout that means '%' (XDG cache — owned by
// madder-cache), '/' (XDG system — unimplemented), and '_' (Unknown —
// root comes from configuration, not a name). Pre-#230 these were
// quietly created in the user-data layout and registered under a
// DIFFERENT scope than the user named.
func TestInitBlobStore_RejectsScopesTheLayoutCannotRepresent(t *testing.T) {
	for _, input := range []string{"%scratch", "/system", "_custom"} {
		t.Run(input, func(t *testing.T) {
			// Fresh env per case: a cancelled context keeps its first
			// cause, so sharing one env would cross-contaminate the
			// per-case error assertions.
			root := t.TempDir()
			leaf := filepath.Join(root, "leaf")
			if err := os.MkdirAll(leaf, 0o755); err != nil {
				t.Fatal(err)
			}

			envLocal := makeEnvLocalAt(
				t, filepath.Dir(root), leaf, t.TempDir(),
			)
			envBlobStore := blob_store_env.MakeBlobStoreEnvWithoutStores(
				envLocal,
			)

			var id scoped_id.Id
			if err := id.Set(input); err != nil {
				t.Fatalf("Set(%q): %v", input, err)
			}

			var cmd Init
			r := recoverInitPanic(func() {
				cmd.InitBlobStore(
					envLocal,
					envBlobStore,
					id,
					makeDefaultTypedConfig(),
				)
			})

			if r == nil {
				t.Fatalf(
					"InitBlobStore(%q) did not reject the unsupported scope",
					input,
				)
			}

			// Cancel outside a Run frame panics with dewey's
			// state sentinel, not the error — the rejection error
			// is observable as the context cause. The BadRequest
			// wrapper's Error() is just the HTTP status; the
			// message lives in the unwrap chain (the CLI error-tree
			// encoder renders it for users).
			cause := envLocal.Cause()
			if cause == nil {
				t.Fatal("context cause not set by the rejection")
			}

			var texts []string
			for err := cause; err != nil; err = stderrors.Unwrap(err) {
				texts = append(texts, err.Error())
			}
			chain := strings.Join(texts, "\n")

			scopeName := fmt.Sprint(id.GetLocationType())
			if !strings.Contains(chain, scopeName) {
				t.Errorf(
					"rejection error chain %q does not name the offending scope %q",
					chain, scopeName,
				)
			}
		})
	}
}
