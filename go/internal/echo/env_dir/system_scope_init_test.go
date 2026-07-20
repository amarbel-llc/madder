//go:build test

package env_dir

import (
	"path/filepath"
	"strings"
	"testing"

	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

// TestMakeDefaultAndInitialize_SystemScope_RootsAtSystemRoot is the madder#280
// regression: a //name (LocationTypeXDGSystem) id no longer panics
// Err501NotImplemented — MakeDefaultAndInitialize roots the env's XDG at the
// injected Config.SystemRoot instead of $HOME.
func TestMakeDefaultAndInitialize_SystemScope_RootsAtSystemRoot(t *testing.T) {
	systemRoot := t.TempDir()
	homeRoot := t.TempDir() // distinct from systemRoot so the assertion is meaningful
	t.Setenv("HOME", homeRoot)

	var sysId scoped_id.Id
	if err := sysId.Set("//shared"); err != nil {
		t.Fatal(err)
	}

	env := MakeDefaultAndInitialize(
		errors.MakeContextDefault(),
		Config{SystemRoot: systemRoot},
		"madder",
		sysId,
	)

	// The data path must root under the injected system root — proving the
	// //name branch system-rooted rather than falling back to $HOME.
	dataPath := env.XDG.Data.MakePath().String()
	if !strings.HasPrefix(dataPath, systemRoot) {
		t.Errorf(
			"system id data path %q is not rooted under SystemRoot %q",
			dataPath, systemRoot,
		)
	}
	if strings.HasPrefix(dataPath, homeRoot) {
		t.Errorf(
			"system id data path %q leaked into HOME %q instead of SystemRoot",
			dataPath, homeRoot,
		)
	}
}

// TestMakeDefaultAndInitialize_SystemScope_EmptyRootResolvesLikeUser pins that
// with no Config.SystemRoot injected, a //name id resolves like a user env
// (rootAtSystem no-ops) rather than panicking — consistent with
// GetXDGForBlobStoreId's empty-SystemRoot no-op (madder#230).
func TestMakeDefaultAndInitialize_SystemScope_EmptyRootResolvesLikeUser(t *testing.T) {
	homeRoot := t.TempDir()
	t.Setenv("HOME", homeRoot)

	var sysId scoped_id.Id
	if err := sysId.Set("//shared"); err != nil {
		t.Fatal(err)
	}

	// No SystemRoot — must NOT panic, and must resolve under $HOME.
	env := MakeDefaultAndInitialize(
		errors.MakeContextDefault(),
		Config{},
		"madder",
		sysId,
	)

	dataPath := env.XDG.Data.MakePath().String()
	if !strings.HasPrefix(dataPath, filepath.Clean(homeRoot)) {
		t.Errorf(
			"empty-SystemRoot //name should resolve under HOME %q; got %q",
			homeRoot, dataPath,
		)
	}
}
