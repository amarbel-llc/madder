//go:build test

package blob_stores

import (
	"testing"
	"time"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
)

// stalledStore is a store whose reads never return until released, like a
// remote that stopped answering.
type stalledStore struct {
	domain_interfaces.BlobStore
	release chan struct{}
}

func (store stalledStore) MakeBlobReader(
	domain_interfaces.MarklId,
) (domain_interfaces.BlobReader, error) {
	<-store.release

	return nil, errStalledStoreReleased
}

type stalledStoreError struct{}

func (stalledStoreError) Error() string { return "stalled store released" }

var errStalledStoreReleased = stalledStoreError{}

// madder#312: a read stalled on a remote must not hang the report.
func TestCheckRelinkCandidateWithin_AbandonsAStalledRead(t *testing.T) {
	store := stalledStore{release: make(chan struct{})}
	defer close(store.release)

	id := sha256IdOf(t, "stalled")

	start := time.Now()

	record := CheckRelinkCandidateWithin(
		store, id, markl.FormatHashBlake2b256, 50*time.Millisecond,
	)

	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("took %s, the limit did not apply", elapsed)
	}

	if record.State != RelinkUnreadable || record.Err == nil {
		t.Fatalf("state = %s err=%v, want unreadable with an error", record.State, record.Err)
	}

	if record.StoredBytes != -1 {
		t.Errorf("StoredBytes = %d, want -1 (unknown)", record.StoredBytes)
	}
}
