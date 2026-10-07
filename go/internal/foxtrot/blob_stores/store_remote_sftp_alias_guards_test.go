//go:build test

package blob_stores

import (
	"os"
	"path/filepath"
	"testing"

	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

// A single-hash sftp store has nowhere to put an alias. It must say so, so
// that sync asks before a cross-hash copy into it and then rehashes without
// trying to alias, as it did before sftp stores could record aliases at
// all. Type-asserting the adder interface alone would say yes.
func TestSftpSupportsForeignDigestAliases_OnlyWhenMultiHash(t *testing.T) {
	h := newSFTPBenchHarness(t, 0, concurrentClientOptions()...)
	defer h.Close()

	multi := newSftpHashTestStore(
		t, h.client, h.remotePath, true, markl.FormatHashBlake2b256,
	)

	if !SupportsForeignDigestAliases(multi) {
		t.Errorf("a multi-hash sftp store reports no alias support")
	}

	single := newSftpHashTestStore(
		t, h.client, t.TempDir(), false, markl.FormatHashBlake2b256,
	)

	if SupportsForeignDigestAliases(single) {
		t.Errorf("a single-hash sftp store reports alias support")
	}
}

// A remote root is usually RELATIVE to the sftp login directory
// ("Library/store"). Aliases must be created, read back and resolved there
// too. Every other sftp alias test uses an absolute temp dir, which is how
// a bug in exactly this case reached a real remote: the alias was created,
// its read-back failed to parse, and madder removed it again.
func TestSftpForeignDigestAlias_RelativeRemoteRoot(t *testing.T) {
	// The harness server resolves relative paths against the process's
	// working directory.
	t.Chdir(t.TempDir())

	h := newSFTPBenchHarness(t, 0, concurrentClientOptions()...)
	defer h.Close()

	const (
		root    = "Library/store"
		payload = "under a relative root"
	)

	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	store := newSftpAliasTestStore(t, h, root)
	native := writeSftpTestBlob(t, store, payload)
	foreign := sha256IdOf(t, payload)

	if err := store.AddForeignBlobDigestForNativeDigest(foreign, native); err != nil {
		t.Fatalf("AddForeignBlobDigestForNativeDigest: %v", err)
	}

	if _, links := countRegularFiles(t, root); links != 1 {
		t.Fatalf("store holds %d links, want 1", links)
	}

	fresh := newSftpAliasTestStore(t, h, root)

	resolved, ok, err := fresh.ResolveForeignBlobDigest(foreign)
	if err != nil || !ok {
		t.Fatalf("ResolveForeignBlobDigest: ok=%t err=%v", ok, err)
	}

	if !markl.Equals(resolved, native) {
		t.Errorf("alias resolved to %s, want %s", resolved, native)
	}

	primed := newSftpAliasTestStore(t, h, root)

	if err = primed.PrimeBlobPresence(); err != nil {
		t.Fatalf("PrimeBlobPresence: %v", err)
	}

	if resolved, ok, err = primed.ResolveForeignBlobDigest(foreign); err != nil || !ok ||
		!markl.Equals(resolved, native) {
		t.Errorf("primed resolve: %v ok=%t err=%v, want %s", resolved, ok, err, native)
	}

	// And a whole alias-carrying copy between two relative roots.
	const dstRoot = "Library/onward"

	if err = os.MkdirAll(dstRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	dst := newSftpAliasTestStore(t, h, dstRoot)

	result := CopyBlobIfNecessary(errors.MakeContextDefault(), dst, store, foreign, nil, nil)
	if err = result.GetError(); err != nil {
		t.Fatalf("copying the alias: %v", err)
	}

	if regular, links := countRegularFiles(t, dstRoot); regular != 1 || links != 1 {
		t.Errorf("destination holds %d files and %d links, want 1 and 1", regular, links)
	}
}

// An alias in the source that points at itself must end in an error, not
// in unbounded recursion.
func TestCopyBlobIfNecessary_SelfReferencingAliasDoesNotRecurse(t *testing.T) {
	h := newSFTPBenchHarness(t, 0, concurrentClientOptions()...)
	defer h.Close()

	src := newSftpAliasTestStore(t, h, h.remotePath)
	dst := newSftpAliasTestStore(t, h, t.TempDir())

	looped := sha256IdOf(t, "points at itself")
	loopedPath := src.remotePathForMerkleId(looped)

	if err := os.MkdirAll(filepath.Dir(loopedPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := os.Symlink(filepath.Base(loopedPath), loopedPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	result := CopyBlobIfNecessary(errors.MakeContextDefault(), dst, src, looped, nil, nil)

	if state := result; state.GetError() == nil && !state.IsMissing() {
		if written, _ := state.GetBytesWrittenAndState(); written >= 0 {
			t.Errorf("a self-referencing alias was copied: %s", result)
		}
	}

	if _, links := countRegularFiles(t, dst.config.GetRemotePath()); links != 0 {
		t.Errorf("a self-referencing alias was reproduced at the destination")
	}
}
