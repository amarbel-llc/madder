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
