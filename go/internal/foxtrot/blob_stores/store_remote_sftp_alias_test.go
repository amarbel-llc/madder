//go:build test

package blob_stores

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

func sha256IdOf(t *testing.T, payload string) domain_interfaces.MarklId {
	t.Helper()

	hash, _ := markl.FormatHashSha256.Get() //repool:owned

	if _, err := hash.Write([]byte(payload)); err != nil {
		t.Fatalf("hashing: %v", err)
	}

	id, _ := hash.GetMarklId()   //repool:owned
	cloned, _ := markl.Clone(id) //repool:owned

	return cloned
}

func newSftpAliasTestStore(t *testing.T, h *sftpBenchHarness, remotePath string) *remoteSftp {
	t.Helper()

	return newSftpHashTestStore(
		t, h.client, remotePath, true, markl.FormatHashBlake2b256,
	)
}

func countRegularFiles(t *testing.T, root string) (regular, links int) {
	t.Helper()

	if err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		switch {
		case info.Mode()&os.ModeSymlink != 0:
			links++
		case info.Mode().IsRegular():
			regular++
		}

		return nil
	}); err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}

	return regular, links
}

// An alias on an sftp store is a relative symlink into the native hash
// type's tree: it reads as the blob, verifies under its own hash type,
// resolves back to the native digest, and stores no second copy.
func TestSftpForeignDigestAlias_RoundTrip(t *testing.T) {
	h := newSFTPBenchHarness(t, 0, concurrentClientOptions()...)
	defer h.Close()

	const payload = "one blob, two names"

	store := newSftpAliasTestStore(t, h, h.remotePath)
	native := writeSftpTestBlob(t, store, payload)
	foreign := sha256IdOf(t, payload)

	if err := store.AddForeignBlobDigestForNativeDigest(foreign, native); err != nil {
		t.Fatalf("AddForeignBlobDigestForNativeDigest: %v", err)
	}

	// Adding the same alias again is not an error.
	if err := store.AddForeignBlobDigestForNativeDigest(foreign, native); err != nil {
		t.Fatalf("adding the same alias twice: %v", err)
	}

	foreignPath := store.remotePathForMerkleId(foreign)

	target, err := os.Readlink(foreignPath)
	if err != nil {
		t.Fatalf("the alias at %s is not a symlink: %v", foreignPath, err)
	}

	if filepath.IsAbs(target) {
		t.Errorf("alias target %q is absolute; it must survive the store being moved", target)
	}

	if regular, links := countRegularFiles(t, h.remotePath); regular != 1 || links != 1 {
		t.Errorf("store holds %d files and %d links, want 1 and 1", regular, links)
	}

	// A fresh store, knowing nothing, must see all of it through the remote.
	fresh := newSftpAliasTestStore(t, h, h.remotePath)

	if !fresh.HasBlob(foreign) {
		t.Errorf("HasBlob is false for the alias")
	}

	resolved, ok, err := fresh.ResolveForeignBlobDigest(foreign)
	if err != nil || !ok {
		t.Fatalf("ResolveForeignBlobDigest: ok=%t err=%v", ok, err)
	}

	if !markl.Equals(resolved, native) {
		t.Errorf("alias resolved to %s, want %s", resolved, native)
	}

	if _, ok, err = fresh.ResolveForeignBlobDigest(native); err != nil || ok {
		t.Errorf("a native blob resolved as an alias: ok=%t err=%v", ok, err)
	}

	reader, err := fresh.MakeBlobReader(foreign)
	if err != nil {
		t.Fatalf("MakeBlobReader(alias): %v", err)
	}
	defer reader.Close() //defer:err-checked

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if string(got) != payload {
		t.Errorf("read %q through the alias, want %q", got, payload)
	}

	if !bytes.Equal(reader.GetMarklId().GetBytes(), foreign.GetBytes()) {
		t.Errorf("reading through the alias did not verify under the alias's hash type")
	}
}

// After PrimeBlobPresence, aliases already on the remote resolve from
// memory: the test removes the link behind the store's back, so a correct
// answer cannot have come from the remote.
func TestSftpForeignDigestAlias_PrimedResolvesWithoutAsking(t *testing.T) {
	h := newSFTPBenchHarness(t, 0, concurrentClientOptions()...)
	defer h.Close()

	const payload = "listed once"

	seeder := newSftpAliasTestStore(t, h, h.remotePath)
	native := writeSftpTestBlob(t, seeder, payload)
	foreign := sha256IdOf(t, payload)

	if err := seeder.AddForeignBlobDigestForNativeDigest(foreign, native); err != nil {
		t.Fatalf("AddForeignBlobDigestForNativeDigest: %v", err)
	}

	primed := newSftpAliasTestStore(t, h, h.remotePath)

	if err := primed.PrimeBlobPresence(); err != nil {
		t.Fatalf("PrimeBlobPresence: %v", err)
	}

	if err := os.Remove(seeder.remotePathForMerkleId(foreign)); err != nil {
		t.Fatalf("removing the alias: %v", err)
	}

	if !primed.HasBlob(foreign) {
		t.Errorf("HasBlob asked the remote about a listed alias")
	}

	resolved, ok, err := primed.ResolveForeignBlobDigest(foreign)
	if err != nil || !ok {
		t.Fatalf("ResolveForeignBlobDigest after priming: ok=%t err=%v", ok, err)
	}

	if !markl.Equals(resolved, native) {
		t.Errorf("alias resolved to %s, want %s", resolved, native)
	}
}

// Copying an id the source holds as an alias must carry the alias across,
// not a second copy of the bytes.
func TestCopyBlobIfNecessary_CarriesAliasesAsAliases(t *testing.T) {
	h := newSFTPBenchHarness(t, 0, concurrentClientOptions()...)
	defer h.Close()

	const payload = "stored once at the destination"

	dstPath := t.TempDir()

	src := newSftpAliasTestStore(t, h, h.remotePath)
	dst := newSftpAliasTestStore(t, h, dstPath)

	native := writeSftpTestBlob(t, src, payload)
	foreign := sha256IdOf(t, payload)

	if err := src.AddForeignBlobDigestForNativeDigest(foreign, native); err != nil {
		t.Fatalf("AddForeignBlobDigestForNativeDigest: %v", err)
	}

	// The alias first, before its native blob has been copied: the copy
	// must bring the native blob along.
	result := CopyBlobIfNecessary(errors.MakeContextDefault(), dst, src, foreign, nil, nil)
	if err := result.GetError(); err != nil {
		t.Fatalf("copying the alias: %v", err)
	}

	if result.DestBlobId == nil || !markl.Equals(result.DestBlobId, native) {
		t.Errorf("DestBlobId = %v, want the native digest %s", result.DestBlobId, native)
	}

	if written, _ := result.GetBytesWrittenAndState(); written != int64(len(payload)) {
		t.Errorf("reported %d bytes written, want %d (the native blob)", written, len(payload))
	}

	if regular, links := countRegularFiles(t, dstPath); regular != 1 || links != 1 {
		t.Fatalf("destination holds %d files and %d links, want 1 and 1", regular, links)
	}

	// Then the native id itself: already there.
	result = CopyBlobIfNecessary(errors.MakeContextDefault(), dst, src, native, nil, nil)
	if !result.Exists() {
		t.Errorf("the native blob was not reported as already present: %s", result)
	}

	// And the alias again, as a resumed sync would: present, and still
	// reporting where it points.
	result = CopyBlobIfNecessary(errors.MakeContextDefault(), dst, src, foreign, nil, nil)
	if !result.Exists() {
		t.Errorf("the alias was not reported as already present: %s", result)
	}

	if result.DestBlobId == nil || !markl.Equals(result.DestBlobId, native) {
		t.Errorf("resumed DestBlobId = %v, want %s", result.DestBlobId, native)
	}

	reader, err := dst.MakeBlobReader(foreign)
	if err != nil {
		t.Fatalf("MakeBlobReader(alias) at the destination: %v", err)
	}
	defer reader.Close() //defer:err-checked

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if string(got) != payload {
		t.Errorf("read %q through the destination alias, want %q", got, payload)
	}
}

// An alias in the source that does not match the blob it points at must
// not be reproduced at the destination.
func TestCopyBlobIfNecessary_RefusesAWrongAlias(t *testing.T) {
	h := newSFTPBenchHarness(t, 0, concurrentClientOptions()...)
	defer h.Close()

	dstPath := t.TempDir()

	src := newSftpAliasTestStore(t, h, h.remotePath)
	dst := newSftpAliasTestStore(t, h, dstPath)

	native := writeSftpTestBlob(t, src, "the real content")
	wrong := sha256IdOf(t, "some other content")

	if err := src.AddForeignBlobDigestForNativeDigest(wrong, native); err != nil {
		t.Fatalf("AddForeignBlobDigestForNativeDigest: %v", err)
	}

	result := CopyBlobIfNecessary(errors.MakeContextDefault(), dst, src, wrong, nil, nil)
	if result.GetError() == nil {
		t.Fatalf("a wrong alias was copied without error: %s", result)
	}

	if _, links := countRegularFiles(t, dstPath); links != 0 {
		t.Errorf("the wrong alias was reproduced at the destination")
	}
}
