//go:build test

package blob_stores

import (
	"os"
	"testing"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
)

// writeSftpTestBlobWith writes payload under the given hash type.
func writeSftpTestBlobWith(
	t *testing.T,
	store *remoteSftp,
	hash markl.FormatHash,
	payload string,
) domain_interfaces.MarklId {
	t.Helper()

	writer, err := store.MakeBlobWriter(hash)
	if err != nil {
		t.Fatalf("MakeBlobWriter: %v", err)
	}

	if _, err = writer.Write([]byte(payload)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err = writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	return writer.GetMarklId()
}

func TestCheckRelinkCandidate(t *testing.T) {
	h := newSFTPBenchHarness(t, 0, concurrentClientOptions()...)
	defer h.Close()

	store := newSftpAliasTestStore(t, h, h.remotePath)

	const payload = "a duplicate stored under two digests"

	native := writeSftpTestBlobWith(t, store, markl.FormatHashBlake2b256, payload)
	copyId := writeSftpTestBlobWith(t, store, markl.FormatHashSha256, payload)

	t.Run("relinkable", func(t *testing.T) {
		record := CheckRelinkCandidate(store, copyId, markl.FormatHashBlake2b256)

		if record.State != RelinkRelinkable || record.Mismatch() {
			t.Fatalf("state = %s (err %v), want relinkable", record.State, record.Err)
		}

		if !markl.Equals(record.Native, native) {
			t.Errorf("native = %s, want %s", record.Native, native)
		}

		if !record.NativeExists || !record.PlaintextMatches {
			t.Errorf("NativeExists=%t PlaintextMatches=%t", record.NativeExists, record.PlaintextMatches)
		}

		if want := int64(len(payload)); record.Bytes != want ||
			record.StoredBytes != want || record.ReclaimableBytes != want {
			t.Errorf("bytes=%d stored=%d reclaimable=%d, want %d",
				record.Bytes, record.StoredBytes, record.ReclaimableBytes, want)
		}
	})

	t.Run("native missing", func(t *testing.T) {
		lone := writeSftpTestBlobWith(t, store, markl.FormatHashSha256, "only copy there is")

		record := CheckRelinkCandidate(store, lone, markl.FormatHashBlake2b256)

		if record.State != RelinkNativeMissing || !record.Mismatch() {
			t.Fatalf("state = %s, want native_missing", record.State)
		}

		if record.NativeExists || !record.PlaintextMatches || record.ReclaimableBytes != 0 {
			t.Errorf("NativeExists=%t PlaintextMatches=%t reclaimable=%d",
				record.NativeExists, record.PlaintextMatches, record.ReclaimableBytes)
		}

		if record.Native == nil {
			t.Errorf("no native id reported for an intact object")
		}
	})

	t.Run("plaintext differs", func(t *testing.T) {
		damaged := writeSftpTestBlobWith(t, store, markl.FormatHashSha256, "bytes later damaged")

		if err := os.WriteFile(store.remotePathForMerkleId(damaged), []byte("something else"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		record := CheckRelinkCandidate(store, damaged, markl.FormatHashBlake2b256)

		if record.State != RelinkPlaintextMismatch || !record.Mismatch() {
			t.Fatalf("state = %s, want plaintext_mismatch", record.State)
		}

		if record.PlaintextMatches || record.ReclaimableBytes != 0 {
			t.Errorf("PlaintextMatches=%t reclaimable=%d", record.PlaintextMatches, record.ReclaimableBytes)
		}
	})

	t.Run("unreadable", func(t *testing.T) {
		absent := sha256IdOf(t, "never written")

		record := CheckRelinkCandidate(store, absent, markl.FormatHashBlake2b256)

		if record.State != RelinkUnreadable || record.Err == nil || record.Native != nil {
			t.Fatalf("state = %s err=%v native=%v, want unreadable", record.State, record.Err, record.Native)
		}
	})
}

// A native that is only a link, or does not read back to its own id, must
// not make the copy look relinkable: relinking would leave no good copy.
func TestCheckRelinkCandidate_RefusesAnUnsoundNative(t *testing.T) {
	h := newSFTPBenchHarness(t, 0, concurrentClientOptions()...)
	defer h.Close()

	store := newSftpAliasTestStore(t, h, h.remotePath)

	asLink := writeSftpTestBlobWith(t, store, markl.FormatHashBlake2b256, "native replaced by a link")
	asLinkCopy := writeSftpTestBlobWith(t, store, markl.FormatHashSha256, "native replaced by a link")

	damaged := writeSftpTestBlobWith(t, store, markl.FormatHashBlake2b256, "native damaged in place")
	damagedCopy := writeSftpTestBlobWith(t, store, markl.FormatHashSha256, "native damaged in place")

	// The native becomes a symlink straight back at the copy.
	nativePath := store.remotePathForMerkleId(asLink)
	if err := os.Remove(nativePath); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if err := os.Symlink(store.remotePathForMerkleId(asLinkCopy), nativePath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	if err := os.WriteFile(store.remotePathForMerkleId(damaged), []byte("not what it was"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// A fresh store, whose listing is what the command works from.
	fresh := newSftpAliasTestStore(t, h, h.remotePath)

	for _, err := range fresh.AllBlobs() {
		if err != nil {
			t.Fatalf("AllBlobs: %v", err)
		}
	}

	t.Run("native is a link", func(t *testing.T) {
		record := CheckRelinkCandidate(fresh, asLinkCopy, markl.FormatHashBlake2b256)

		if record.State != RelinkNativeNotRegular || !record.Mismatch() {
			t.Fatalf("state = %s (err %v), want native_not_regular", record.State, record.Err)
		}

		if record.ReclaimableBytes != 0 {
			t.Errorf("reclaimable = %d, want 0", record.ReclaimableBytes)
		}
	})

	t.Run("native does not read back to its id", func(t *testing.T) {
		record := CheckRelinkCandidate(fresh, damagedCopy, markl.FormatHashBlake2b256)

		if record.State != RelinkNativeInvalid || !record.Mismatch() {
			t.Fatalf("state = %s (err %v), want native_invalid", record.State, record.Err)
		}

		if record.ReclaimableBytes != 0 {
			t.Errorf("reclaimable = %d, want 0", record.ReclaimableBytes)
		}
	})
}

// Only an alias the listing saw as a symlink is reported as one; a full copy
// under a foreign digest is not.
func TestSftpIsForeignDigestAlias(t *testing.T) {
	h := newSFTPBenchHarness(t, 0, concurrentClientOptions()...)
	defer h.Close()

	store := newSftpAliasTestStore(t, h, h.remotePath)

	native := writeSftpTestBlob(t, store, "aliased")
	aliased := sha256IdOf(t, "aliased")

	if err := store.AddForeignBlobDigestForNativeDigest(aliased, native); err != nil {
		t.Fatalf("AddForeignBlobDigestForNativeDigest: %v", err)
	}

	copied := writeSftpTestBlobWith(t, store, markl.FormatHashSha256, "a full copy")

	fresh := newSftpAliasTestStore(t, h, h.remotePath)

	if fresh.IsForeignDigestAlias(aliased) {
		t.Errorf("reported an alias before any listing")
	}

	for _, err := range fresh.AllBlobs() {
		if err != nil {
			t.Fatalf("AllBlobs: %v", err)
		}
	}

	if !fresh.IsForeignDigestAlias(aliased) {
		t.Errorf("the alias was not reported as one")
	}

	if fresh.IsForeignDigestAlias(copied) {
		t.Errorf("a full copy was reported as an alias")
	}

	if fresh.IsForeignDigestAlias(native) {
		t.Errorf("the native object was reported as an alias")
	}
}
