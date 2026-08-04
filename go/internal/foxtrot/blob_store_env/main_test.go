//go:build test

package blob_store_env

// Unit coverage for the graceful-degrade paths (madder secondary fix):
// a discovered store that fails to build is skipped and stashed with a
// BuildErr, and the failure becomes a HARD error only when the store is
// explicitly addressed or ordered. These tests build a BlobStoreEnv with
// a hand-populated store map (mixed built / degraded entries) so the
// method behaviour is exercised without going through filesystem
// discovery, and use env_local.Cause() to observe the context
// cancellation the hard-fail paths emit (a Cancel outside a Run frame
// also panics, so cancel-driven calls are wrapped in recoverCancel — the
// Cause is the assertion).

import (
	stderrors "errors"
	"path/filepath"
	"strings"
	"testing"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/bravo/directory_layout"
	_ "code.linenisgreat.com/madder/go/internal/charlie/markl_registrations"
	"code.linenisgreat.com/madder/go/internal/delta/env_ui"
	"code.linenisgreat.com/madder/go/internal/echo/env_dir"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_stores"
	"code.linenisgreat.com/madder/go/internal/foxtrot/env_local"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

// --- helpers ---------------------------------------------------------

// makeEnvLocal builds a real env_local.Env with a MakeContextDefault
// context (so Cancel/Cause work) and HOME + the XDG set sandboxed under a
// temp dir, so env_dir construction never touches the real $HOME
// (unwritable in the nix build sandbox, polluted otherwise). No chdir /
// ceiling is needed: these tests hand-populate the store map rather than
// discovering, so the env's cwd is irrelevant.
func makeEnvLocal(t *testing.T) env_local.Env {
	t.Helper()

	sandbox := t.TempDir()
	t.Setenv("HOME", filepath.Join(sandbox, "home"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(sandbox, "cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(sandbox, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(sandbox, "state"))
	t.Setenv("XDG_RUNTIME_HOME", filepath.Join(sandbox, "runtime"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(sandbox, "data"))

	ctx := errors.MakeContextDefault()
	dirEnv := env_dir.MakeDefault(ctx, env_dir.Config{}, "madder")

	return env_local.Make(env_ui.MakeDefault(ctx), &dirEnv)
}

// envWith builds a BlobStoreEnv over a real env_local (for cancellation
// observation) with the given hand-populated store map and default id.
func envWith(
	t *testing.T,
	defaultId string,
	stores blob_stores.BlobStoreMap,
) (BlobStoreEnv, env_local.Env) {
	t.Helper()

	envLocal := makeEnvLocal(t)
	env := BlobStoreEnv{Env: envLocal}
	env.blobStores = stores
	env.defaultBlobStoreIdString = defaultId

	return env, envLocal
}

// recoverCancel runs f and returns the recovered panic value (or nil).
// The hard-fail paths cancel the env context, which panics outside a Run
// frame; the durable assertion is env_local.Cause(), not the panic.
func recoverCancel(f func()) (r any) {
	defer func() { r = recover() }()
	f()
	return r
}

// causeChain flattens an error's Unwrap chain into one searchable string.
// A BadRequest-wrapped cause renders only its HTTP status via Error(); the
// human message lives further down the chain (matching init_test.go).
func causeChain(err error) string {
	var texts []string
	for e := err; e != nil; e = stderrors.Unwrap(e) {
		texts = append(texts, e.Error())
	}
	return strings.Join(texts, "\n")
}

// fakeBlobStore is a domain_interfaces.BlobStore whose only live methods
// are the two the read paths call. The embedded nil interface satisfies
// the (large) BlobStore surface; any other method would nil-panic, which
// no test here triggers.
type fakeBlobStore struct {
	domain_interfaces.BlobStore
	has bool
}

func (f fakeBlobStore) HasBlob(domain_interfaces.MarklId) bool { return f.has }

func (f fakeBlobStore) MakeBlobReader(
	domain_interfaces.MarklId,
) (domain_interfaces.BlobReader, error) {
	return nil, nil
}

// builtEntry is a discovered store that built: non-nil backend, no
// BuildErr. has controls whether its backend reports the probed blob.
func builtEntry(name string, has bool) blob_stores.BlobStoreInitialized {
	var bs blob_stores.BlobStoreInitialized
	bs.BlobStore = fakeBlobStore{has: has}
	bs.ConfigNamed.Path = directory_layout.MakeBlobStorePath(
		scoped_id.Make(name), "", "",
	)
	return bs
}

// degradedEntry is a discovered store that failed to build: nil backend,
// BuildErr stashed.
func degradedEntry(name string, err error) blob_stores.BlobStoreInitialized {
	var bs blob_stores.BlobStoreInitialized
	bs.BuildErr = err
	bs.ConfigNamed.Path = directory_layout.MakeBlobStorePath(
		scoped_id.Make(name), "", "",
	)
	return bs
}

func testMarklId(t *testing.T) domain_interfaces.MarklId {
	t.Helper()
	var d markl.Id
	if err := d.SetMarklId(markl.FormatIdHashBlake2b256, make([]byte, 32)); err != nil {
		t.Fatalf("SetMarklId: %v", err)
	}
	return &d
}

// --- selectDefaultBlobStoreId ---------------------------------------

func TestSelectDefaultBlobStoreId(t *testing.T) {
	boom := errors.Errorf("boom")

	cases := []struct {
		name   string
		stores blob_stores.BlobStoreMap
		want   string
	}{
		{
			name:   "empty map",
			stores: blob_stores.BlobStoreMap{},
			want:   "",
		},
		{
			name:   "single built",
			stores: blob_stores.BlobStoreMap{"only": builtEntry("only", false)},
			want:   "only",
		},
		{
			// "aaa" sorts first but is degraded; the default must skip it
			// and pick the next built store.
			name: "degraded first is skipped",
			stores: blob_stores.BlobStoreMap{
				"aaa": degradedEntry("aaa", boom),
				"bbb": builtEntry("bbb", false),
			},
			want: "bbb",
		},
		{
			name: "all degraded yields empty",
			stores: blob_stores.BlobStoreMap{
				"aaa": degradedEntry("aaa", boom),
				"bbb": degradedEntry("bbb", boom),
			},
			want: "",
		},
		{
			name: "built first wins",
			stores: blob_stores.BlobStoreMap{
				"aaa": builtEntry("aaa", false),
				"bbb": degradedEntry("bbb", boom),
			},
			want: "aaa",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := selectDefaultBlobStoreId(tc.stores); got != tc.want {
				t.Errorf("selectDefaultBlobStoreId = %q, want %q", got, tc.want)
			}
		})
	}
}

// --- GetBlobStore ----------------------------------------------------

func TestGetBlobStore_DegradedSurfacesErrorOnAddress(t *testing.T) {
	boom := errors.Errorf("underlying build failure xyz")
	env, envLocal := envWith(t, "good", blob_stores.BlobStoreMap{
		"good": builtEntry("good", false),
		"bad":  degradedEntry("bad", boom),
	})

	recoverCancel(func() { env.GetBlobStore(scoped_id.Make("bad")) })

	cause := envLocal.Cause()
	if cause == nil {
		t.Fatal("addressing a degraded store did not cancel the context")
	}
	// dewey's Wrapf annotation ("failed to build during discovery") is
	// carried for the CLI error-tree renderer, not surfaced via Error();
	// the durable, user-visible signal is the stashed BuildErr itself,
	// which must appear in the cancellation cause.
	if chain := causeChain(cause); !strings.Contains(chain, "underlying build failure xyz") {
		t.Errorf("cause chain missing the stashed BuildErr: %q", chain)
	}
}

func TestGetBlobStore_BuiltReturnsWithoutCancel(t *testing.T) {
	env, envLocal := envWith(t, "good", blob_stores.BlobStoreMap{
		"good": builtEntry("good", false),
	})

	got := env.GetBlobStore(scoped_id.Make("good"))

	if got.BlobStore == nil {
		t.Error("GetBlobStore returned an entry with a nil backend for a built store")
	}
	if cause := envLocal.Cause(); cause != nil {
		t.Errorf("GetBlobStore of a built store cancelled the context: %v", cause)
	}
}

func TestGetBlobStore_NotFoundCancels(t *testing.T) {
	env, envLocal := envWith(t, "good", blob_stores.BlobStoreMap{
		"good": builtEntry("good", false),
	})

	recoverCancel(func() { env.GetBlobStore(scoped_id.Make("missing")) })

	cause := envLocal.Cause()
	if cause == nil {
		t.Fatal("addressing a missing store did not cancel the context")
	}
	if chain := causeChain(cause); !strings.Contains(chain, "not found") {
		t.Errorf("cause chain missing not-found message: %q", chain)
	}
}

// --- GetDefaultBlobStore --------------------------------------------

func TestGetDefaultBlobStore_ReturnsBuiltDefault(t *testing.T) {
	env, _ := envWith(t, "good", blob_stores.BlobStoreMap{
		"good": builtEntry("good", false),
	})

	if r := recoverCancel(func() {
		if env.GetDefaultBlobStore().BlobStore == nil {
			t.Error("default store has a nil backend")
		}
	}); r != nil {
		t.Fatalf("GetDefaultBlobStore panicked for a usable default: %v", r)
	}
}

func TestGetDefaultBlobStore_PanicsWhenNoUsableDefault(t *testing.T) {
	boom := errors.Errorf("boom")

	// Non-empty map, but every store degraded, so setupStores would have
	// left the default id "". GetDefaultBlobStore must panic rather than
	// return a zero-value entry with a nil backend.
	env, _ := envWith(t, "", blob_stores.BlobStoreMap{
		"aaa": degradedEntry("aaa", boom),
	})

	if r := recoverCancel(func() { env.GetDefaultBlobStore() }); r == nil {
		t.Error("GetDefaultBlobStore did not panic with no usable default")
	}

	// The empty-map case must panic too.
	envEmpty, _ := envWith(t, "", blob_stores.BlobStoreMap{})
	if r := recoverCancel(func() { envEmpty.GetDefaultBlobStore() }); r == nil {
		t.Error("GetDefaultBlobStore did not panic on an empty map")
	}
}

// --- SetBlobStoreOrder ----------------------------------------------

func TestSetBlobStoreOrder_DegradedExplicitNameFailsHard(t *testing.T) {
	boom := errors.Errorf("explicit-order build failure")
	env, envLocal := envWith(t, "good", blob_stores.BlobStoreMap{
		"good": builtEntry("good", false),
		"bad":  degradedEntry("bad", boom),
	})

	recoverCancel(func() {
		env.SetBlobStoreOrder([]scoped_id.Id{scoped_id.Make("bad")})
	})

	cause := envLocal.Cause()
	if cause == nil {
		t.Fatal("ordering a degraded store did not cancel the context")
	}
	if chain := causeChain(cause); !strings.Contains(chain, "explicit-order build failure") {
		t.Errorf("cause chain missing the underlying BuildErr: %q", chain)
	}
	if env.orderedBlobStoreIds != nil {
		t.Error("order was set despite the degraded explicit name")
	}
}

func TestSetBlobStoreOrder_AllBuiltSetsOrder(t *testing.T) {
	env, envLocal := envWith(t, "", blob_stores.BlobStoreMap{
		"a": builtEntry("a", false),
		"b": builtEntry("b", false),
	})

	env.SetBlobStoreOrder([]scoped_id.Id{
		scoped_id.Make("b"), scoped_id.Make("a"),
	})

	if cause := envLocal.Cause(); cause != nil {
		t.Fatalf("ordering all-built stores cancelled the context: %v", cause)
	}
	if got := env.defaultBlobStoreIdString; got != "b" {
		t.Errorf("default = %q, want first ordered id %q", got, "b")
	}
	want := []string{"b", "a"}
	if len(env.orderedBlobStoreIds) != len(want) {
		t.Fatalf("order = %v, want %v", env.orderedBlobStoreIds, want)
	}
	for i := range want {
		if env.orderedBlobStoreIds[i] != want[i] {
			t.Errorf("order[%d] = %q, want %q", i, env.orderedBlobStoreIds[i], want[i])
		}
	}
}

// --- read-path nil-backend skipping ---------------------------------

func TestStoreHasBlob_NilBackendSkipped(t *testing.T) {
	id := testMarklId(t)

	if storeHasBlob(blob_stores.BlobStoreInitialized{}, id) {
		t.Error("storeHasBlob reported true for a nil-backend store")
	}
	if storeHasBlob(degradedEntry("bad", errors.Errorf("boom")), id) {
		t.Error("storeHasBlob reported true for a degraded store")
	}
	if !storeHasBlob(builtEntry("good", true), id) {
		t.Error("storeHasBlob reported false for a built store that has the blob")
	}
}

func TestTryOpenInStore_NilBackendSkipped(t *testing.T) {
	id := testMarklId(t)

	if _, ok, _ := tryOpenInStore(blob_stores.BlobStoreInitialized{}, id); ok {
		t.Error("tryOpenInStore reported ok for a nil-backend store")
	}
	if _, ok, _ := tryOpenInStore(builtEntry("good", true), id); !ok {
		t.Error("tryOpenInStore reported not-ok for a built store that has the blob")
	}
}

func TestHasBlobInAnyStore_SkipsDegradedFindsBuilt(t *testing.T) {
	id := testMarklId(t)

	// default present-but-without-the-blob; remaining has a degraded
	// (nil-backend) store and a built store that DOES have it.
	env, _ := envWith(t, "aaa", blob_stores.BlobStoreMap{
		"aaa": builtEntry("aaa", false),
		"bbb": degradedEntry("bbb", errors.Errorf("boom")),
		"ccc": builtEntry("ccc", true),
	})

	if !env.HasBlobInAnyStore(id) {
		t.Error("HasBlobInAnyStore skipped the built store holding the blob")
	}

	// No store holds it: default without it, remaining only degraded.
	envMiss, _ := envWith(t, "aaa", blob_stores.BlobStoreMap{
		"aaa": builtEntry("aaa", false),
		"bbb": degradedEntry("bbb", errors.Errorf("boom")),
	})
	if envMiss.HasBlobInAnyStore(id) {
		t.Error("HasBlobInAnyStore reported found when no built store holds the blob")
	}
}
