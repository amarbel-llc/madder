//go:build test

package env_dir

import (
	"path/filepath"
	"strings"
	"testing"

	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
)

// TestSystemScopeXDG_RootsAtSystemRoot is the madder#230 regression test:
// an XDG-system (`//name`) id resolves its blob-store XDG under the injected
// Config.SystemRoot — `<root>/blob_stores` — regardless of the env's
// home/cwd rooting, and a non-system id under the same env does NOT leak
// into the system root. The system root is injected (not hardcoded in
// env_dir) so this test sandboxes it under t.TempDir(); the madder layer
// passes madder_env.DefaultSystemRoot (/var/lib/madder) in production.
func TestSystemScopeXDG_RootsAtSystemRoot(t *testing.T) {
	homeRoot := t.TempDir()   // env construction sandbox (home/cwd override)
	systemRoot := t.TempDir() // the injected //name system root

	env := MakeWithXDGRootOverrideHomeAndInitialize(
		errors.MakeContextDefault(),
		Config{SystemRoot: systemRoot},
		"madder",
		homeRoot,
	)

	var sysId scoped_id.Id
	if err := sysId.Set("//shared"); err != nil {
		t.Fatal(err)
	}

	got := env.GetXDGForBlobStoreId(sysId).Data.MakePath("blob_stores").String()
	want := filepath.Join(systemRoot, "blob_stores")
	if got != want {
		t.Errorf("system blob-store Data path = %q, want %q", got, want)
	}

	// A user id under the same env must resolve elsewhere — never under
	// the system root. Guards against the system rooting leaking into
	// non-system scopes.
	var userId scoped_id.Id
	if err := userId.Set("default"); err != nil {
		t.Fatal(err)
	}

	userPath := env.GetXDGForBlobStoreId(userId).Data.MakePath("blob_stores").String()
	if strings.HasPrefix(userPath, systemRoot) {
		t.Errorf("user blob-store path %q leaked into system root %q", userPath, systemRoot)
	}
}

// TestSystemScopeXDG_EmptyRootIsNoOp pins that an env with no SystemRoot
// injected leaves a system id's XDG un-rerooted (rootAtSystem no-ops),
// so the feature is fully opt-in via Config.SystemRoot.
func TestSystemScopeXDG_EmptyRootIsNoOp(t *testing.T) {
	homeRoot := t.TempDir()

	env := MakeWithXDGRootOverrideHomeAndInitialize(
		errors.MakeContextDefault(),
		Config{}, // no SystemRoot
		"madder",
		homeRoot,
	)

	var sysId scoped_id.Id
	if err := sysId.Set("//shared"); err != nil {
		t.Fatal(err)
	}

	var userId scoped_id.Id
	if err := userId.Set("default"); err != nil {
		t.Fatal(err)
	}

	// With no system root, rootAtSystem no-ops, so a system id resolves to
	// the SAME un-rerooted base as a user id (both via CloneWithoutOverride).
	// Asserted relatively so it holds regardless of $HOME (the non-overridden
	// base resolves against the real home, which a sandbox can't predict).
	sysPath := env.GetXDGForBlobStoreId(sysId).Data.MakePath("blob_stores").String()
	userPath := env.GetXDGForBlobStoreId(userId).Data.MakePath("blob_stores").String()
	if sysPath != userPath {
		t.Errorf(
			"empty SystemRoot: system path %q != user path %q (rootAtSystem should no-op)",
			sysPath, userPath,
		)
	}
}

// TestSystemScopedEnv_TempRootsUnderSystemRoot is the madder#10 regression
// test: a Config.SystemScoped env roots its base XDG — and therefore its
// per-pid TempLocal — under SystemRoot, so a system-store daemon's link(2)
// staging colocates with the store (EXDEV-safe; ProtectSystem-safe). A
// non-system-scoped env (control) keeps its temp under the override root.
// initializeXDG applies the rooting, so every constructor (serve's
// MakeDefault and this override constructor alike) gets it.
func TestSystemScopedEnv_TempRootsUnderSystemRoot(t *testing.T) {
	homeRoot := t.TempDir()   // base override; discarded for categories by rootAtSystem
	systemRoot := t.TempDir() // the injected system root

	sys := MakeWithXDGRootOverrideHomeAndInitialize(
		errors.MakeContextDefault(),
		Config{SystemRoot: systemRoot, SystemScoped: true},
		"madder",
		homeRoot,
	)
	if got := sys.GetTempLocal().BasePath; !strings.HasPrefix(got, systemRoot) {
		t.Errorf("system-scoped TempLocal = %q, want under systemRoot %q", got, systemRoot)
	}

	plain := MakeWithXDGRootOverrideHomeAndInitialize(
		errors.MakeContextDefault(),
		Config{SystemRoot: systemRoot}, // SystemRoot set but NOT SystemScoped
		"madder",
		homeRoot,
	)
	if got := plain.GetTempLocal().BasePath; strings.HasPrefix(got, systemRoot) {
		t.Errorf("non-system-scoped temp %q leaked into systemRoot %q", got, systemRoot)
	}
}
