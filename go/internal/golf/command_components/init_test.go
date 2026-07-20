//go:build test

package command_components

import (
	"bytes"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code.linenisgreat.com/madder/go/internal/0/ids"
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	_ "code.linenisgreat.com/madder/go/internal/bravo/plugins/builtins"
	_ "code.linenisgreat.com/madder/go/internal/charlie/markl_registrations"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/madder/go/internal/delta/env_ui"
	"code.linenisgreat.com/madder/go/internal/echo/env_dir"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_store_env"
	"code.linenisgreat.com/madder/go/internal/foxtrot/env_local"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
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
	return makeEnvLocalAtWithSystemRoot(t, ceiling, dir, xdgDataHome, "")
}

// makeEnvLocalAtWithSystemRoot is makeEnvLocalAt plus an injected
// env_dir.Config.SystemRoot, so XDG-system (`//name`) init tests can
// sandbox the system root under t.TempDir() rather than /var/lib/madder
// (madder#230).
func makeEnvLocalAtWithSystemRoot(
	t *testing.T,
	ceiling, dir, xdgDataHome, systemRoot string,
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

	dirEnv := env_dir.MakeDefault(ctx, env_dir.Config{SystemRoot: systemRoot}, "madder")

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
// madder-cache) and '_' (Unknown — root comes from configuration, not a
// name). ('/'/'//' XDG system is now implemented as of madder#230 — it
// resolves via the v3System layout rather than being rejected — so it is
// no longer in this list; see TestInitBlobStore_SystemScopeSucceeds.)
// Pre-#230 these were quietly created in the user-data layout and
// registered under a DIFFERENT scope than the user named.
func TestInitBlobStore_RejectsScopesTheLayoutCannotRepresent(t *testing.T) {
	for _, input := range []string{"%scratch", "_custom"} {
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

// TestInitBlobStore_SystemScopeSucceeds pins madder#230: an XDG-system
// (`//name`) id is now accepted by init (it resolves via the v3System
// layout instead of being rejected) and creates the store under the
// injected system root — <root>/blob_stores/<name>. The system root is
// sandboxed under t.TempDir(); production uses /var/lib/madder via
// madder_env.DefaultSystemRoot.
func TestInitBlobStore_SystemScopeSucceeds(t *testing.T) {
	root := t.TempDir()
	leaf := filepath.Join(root, "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	systemRoot := t.TempDir()

	envLocal := makeEnvLocalAtWithSystemRoot(
		t, filepath.Dir(root), leaf, t.TempDir(), systemRoot,
	)
	envBlobStore := blob_store_env.MakeBlobStoreEnvWithoutStores(envLocal)

	var id scoped_id.Id
	if err := id.Set("//shared"); err != nil {
		t.Fatalf("Set(//shared): %v", err)
	}

	var cmd Init
	var basePath string
	r := recoverInitPanic(func() {
		basePath = cmd.InitBlobStore(
			envLocal,
			envBlobStore,
			id,
			makeDefaultTypedConfig(),
		).GetBase()
	})

	if r != nil {
		t.Fatalf("InitBlobStore(//shared) rejected the system scope: %v", r)
	}

	want := filepath.Join(systemRoot, "blob_stores", "shared")
	if !strings.HasPrefix(basePath, want) {
		t.Errorf("system store base = %q, want under %q", basePath, want)
	}
}

// TestMakeBlobStoreByScopedId_SystemStore pins madder#10's single-store
// open-by-id: after `madder init //shared` creates the store + on-disk
// config under the (sandboxed) system root, MakeBlobStoreByScopedId opens
// it WITHOUT discovery — the resolution `madder serve --store //shared`
// relies on (it reads the on-disk config directly, so it works even where
// discovery doesn't run). See TestMakeBlobStores_DiscoversSystemStore for
// the discovery side (#230 inc-2).
func TestMakeBlobStoreByScopedId_SystemStore(t *testing.T) {
	root := t.TempDir()
	leaf := filepath.Join(root, "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	systemRoot := t.TempDir()

	envLocal := makeEnvLocalAtWithSystemRoot(
		t, filepath.Dir(root), leaf, t.TempDir(), systemRoot,
	)
	envBlobStore := blob_store_env.MakeBlobStoreEnvWithoutStores(envLocal)

	var id scoped_id.Id
	if err := id.Set("//shared"); err != nil {
		t.Fatalf("Set(//shared): %v", err)
	}

	var initCmd Init
	if r := recoverInitPanic(func() {
		initCmd.InitBlobStore(envLocal, envBlobStore, id, makeDefaultTypedConfig())
	}); r != nil {
		t.Fatalf("init //shared: %v", r)
	}

	var (
		bsCmd       BlobStore
		storeOpened bool
		storeBase   string
	)
	if r := recoverInitPanic(func() {
		s := bsCmd.MakeBlobStoreByScopedId(envBlobStore, id)
		storeOpened = s.GetBlobStore() != nil
		storeBase = s.Path.GetBase()
	}); r != nil {
		t.Fatalf("MakeBlobStoreByScopedId(//shared): %v", r)
	}

	if !storeOpened {
		t.Fatal("MakeBlobStoreByScopedId returned an uninitialized store")
	}

	want := filepath.Join(systemRoot, "blob_stores", "shared")
	if !strings.HasPrefix(storeBase, want) {
		t.Errorf("opened store base = %q, want under %q", storeBase, want)
	}
}

// TestEnsureBlobStoreVerbatim_WriteIdempotentDrift pins the two-stage init
// primitive: writing a digest-pinned config-gen artifact is verbatim and
// idempotent-by-digest. (a) absent → bytes written byte-identical; (b)
// re-run same artifact → no-op success; (c) a different config at the same
// id → drift error. This is the engine behind `init-from <id>@<digest>`.
func TestEnsureBlobStoreVerbatim_WriteIdempotentDrift(t *testing.T) {
	root := t.TempDir()
	leaf := filepath.Join(root, "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}

	envLocal := makeEnvLocalAt(t, filepath.Dir(root), leaf, t.TempDir())
	envBlobStore := blob_store_env.MakeBlobStoreEnvWithoutStores(envLocal)

	var id scoped_id.Id
	if err := id.Set(".verbatim"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Stage 1: a digest-stamped artifact (EncodeWithDigest populates
	// BlobDigest). Copy the bytes — buf is reused below.
	tc := makeDefaultTypedConfig()
	var buf bytes.Buffer
	if _, err := blob_store_configs.EncodeWithDigest(tc, &buf); err != nil {
		t.Fatalf("EncodeWithDigest: %v", err)
	}
	raw := append([]byte(nil), buf.Bytes()...)

	var cmd Init

	// (a) absent → writes verbatim.
	var configPath string
	if r := recoverInitPanic(func() {
		configPath = cmd.EnsureBlobStoreVerbatim(
			envLocal, envBlobStore, id, raw, tc.BlobDigest,
		).GetConfig()
	}); r != nil {
		t.Fatalf("first EnsureBlobStoreVerbatim rejected: %v", r)
	}

	onDisk, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read on-disk config: %v", err)
	}
	if !bytes.Equal(onDisk, raw) {
		t.Fatal("on-disk config is not byte-verbatim with the artifact")
	}

	// (b) present + same digest → idempotent no-op.
	if r := recoverInitPanic(func() {
		cmd.EnsureBlobStoreVerbatim(envLocal, envBlobStore, id, raw, tc.BlobDigest)
	}); r != nil {
		t.Fatalf("idempotent re-run rejected: %v", r)
	}

	// (c) present + different config → drift error.
	tcOther := makeDefaultTypedConfig()
	tcOther.Blob.(*blob_store_configs.DefaultType).CompressionType = "none"
	var bufOther bytes.Buffer
	if _, err := blob_store_configs.EncodeWithDigest(tcOther, &bufOther); err != nil {
		t.Fatalf("EncodeWithDigest (other): %v", err)
	}
	if tcOther.BlobDigest.String() == tc.BlobDigest.String() {
		t.Fatal("test setup: expected differing digests for zstd vs none")
	}

	if r := recoverInitPanic(func() {
		cmd.EnsureBlobStoreVerbatim(
			envLocal, envBlobStore, id, bufOther.Bytes(), tcOther.BlobDigest,
		)
	}); r == nil {
		t.Fatal("a different config at the same id did not error (drift undetected)")
	}
}

// TestMakeBlobStores_DiscoversSystemStore pins madder#230 increment 2:
// after `madder init //shared` creates a system store under the (sandboxed)
// system root, full discovery (MakeBlobStores via MakeBlobStoreEnv) surfaces
// it in the BlobStoreMap under its `//shared` key — alongside, and disjoint
// from, the unprefixed user store created in the same env. This is what
// `madder list` and store-switch arg resolution see.
func TestMakeBlobStores_DiscoversSystemStore(t *testing.T) {
	root := t.TempDir()
	leaf := filepath.Join(root, "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	systemRoot := t.TempDir()
	xdgDataHome := t.TempDir()

	envLocal := makeEnvLocalAtWithSystemRoot(
		t, filepath.Dir(root), leaf, xdgDataHome, systemRoot,
	)
	envInit := blob_store_env.MakeBlobStoreEnvWithoutStores(envLocal)

	var initCmd Init
	for _, name := range []string{"//shared", "user-store"} {
		var id scoped_id.Id
		if err := id.Set(name); err != nil {
			t.Fatalf("Set(%q): %v", name, err)
		}
		if r := recoverInitPanic(func() {
			initCmd.InitBlobStore(envLocal, envInit, id, makeDefaultTypedConfig())
		}); r != nil {
			t.Fatalf("init %q: %v", name, r)
		}
	}

	envDiscovery := blob_store_env.MakeBlobStoreEnv(envLocal)
	stores := envDiscovery.GetBlobStores()

	if _, ok := stores["//shared"]; !ok {
		t.Errorf(
			"discovery did not surface the system store under %q; available: %v",
			"//shared", storeKeys(envDiscovery),
		)
	}

	// The user store must still be discovered under its unprefixed key —
	// the system pass merges in, it does not replace the user/cwd entries.
	if _, ok := stores["user-store"]; !ok {
		t.Errorf(
			"discovery dropped the user store %q after the system pass; available: %v",
			"user-store", storeKeys(envDiscovery),
		)
	}
}
