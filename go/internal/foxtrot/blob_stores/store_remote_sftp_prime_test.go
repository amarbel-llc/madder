//go:build test

package blob_stores

import (
	"os"
	"testing"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
)

func writeSftpTestBlob(
	t *testing.T,
	store *remoteSftp,
	payload string,
) domain_interfaces.MarklId {
	t.Helper()

	writer, err := store.MakeBlobWriter(markl.FormatHashBlake2b256)
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

// After PrimeBlobPresence, HasBlob must answer from the one listing and
// not ask the remote again, for present and for absent blobs alike. The
// test makes the remote disagree with the listing behind the store's back:
// an answer that still matches the listing cannot have come from the
// remote.
func TestSftpPrimeBlobPresence_HasBlobStopsAskingTheRemote(t *testing.T) {
	h := newSFTPBenchHarness(t, 0, concurrentClientOptions()...)
	defer h.Close()

	newStore := func() *remoteSftp {
		return newSftpHashTestStore(
			t, h.client, h.remotePath, true, markl.FormatHashBlake2b256,
		)
	}

	seeder := newStore()
	present := writeSftpTestBlob(t, seeder, "present when listed")

	primed := newStore()

	if err := primed.PrimeBlobPresence(); err != nil {
		t.Fatalf("PrimeBlobPresence: %v", err)
	}

	// Remove the listed blob and add an unlisted one, bypassing `primed`.
	if err := os.Remove(seeder.remotePathForMerkleId(present)); err != nil {
		t.Fatalf("removing the listed blob: %v", err)
	}

	addedLater := writeSftpTestBlob(t, seeder, "added after the listing")

	if !primed.HasBlob(present) {
		t.Errorf("HasBlob asked the remote about a blob that was in the listing")
	}

	if primed.HasBlob(addedLater) {
		t.Errorf("HasBlob asked the remote about a blob that was not in the listing")
	}

	// A store that has not been primed still asks, so it sees the truth.
	unprimed := newStore()

	if unprimed.HasBlob(present) {
		t.Errorf("unprimed HasBlob reported a removed blob as present")
	}

	if !unprimed.HasBlob(addedLater) {
		t.Errorf("unprimed HasBlob missed a blob that is on the remote")
	}
}

// What the primed store writes itself must show up as present, or a sync
// would upload a blob it meets twice.
func TestSftpPrimeBlobPresence_SeesItsOwnWrites(t *testing.T) {
	h := newSFTPBenchHarness(t, 0, concurrentClientOptions()...)
	defer h.Close()

	store := newSftpHashTestStore(
		t, h.client, h.remotePath, true, markl.FormatHashBlake2b256,
	)

	// An empty store: there are no bucket directories to list yet.
	if err := store.PrimeBlobPresence(); err != nil {
		t.Fatalf("PrimeBlobPresence on an empty store: %v", err)
	}

	id := writeSftpTestBlob(t, store, "written after priming")

	if !store.HasBlob(id) {
		t.Errorf("a blob written after priming reads as absent")
	}
}

// A listing that fails must leave HasBlob asking the remote: a partial
// listing treated as complete would report uploaded blobs as absent, and
// worse, absent blobs could never be reported present.
func TestSftpPrimeBlobPresence_FailedListingKeepsAsking(t *testing.T) {
	h := newSFTPBenchHarness(t, 0, concurrentClientOptions()...)
	defer h.Close()

	seeder := newSftpHashTestStore(
		t, h.client, h.remotePath, true, markl.FormatHashBlake2b256,
	)
	id := writeSftpTestBlob(t, seeder, "on the remote")

	broken := newSftpHashTestStore(
		t, h.client, h.remotePath+"-does-not-exist", true, markl.FormatHashBlake2b256,
	)

	if err := broken.PrimeBlobPresence(); err == nil {
		t.Fatalf("PrimeBlobPresence succeeded against a missing remote path")
	}

	if broken.blobCacheComplete {
		t.Fatalf("a failed listing was recorded as complete")
	}

	// Pointed back at the real path, it must find the blob by asking.
	broken.config = benchRemotePathConfig{remotePath: h.remotePath}

	if !broken.HasBlob(id) {
		t.Errorf("HasBlob did not ask the remote after a failed listing")
	}
}
