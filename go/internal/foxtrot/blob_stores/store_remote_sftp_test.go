//go:build test

package blob_stores

import (
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
)

// recordingObserver captures every event handed to OnBlobPublished so
// unit tests can assert against the full payload. Lives here rather
// than in a shared test helper because blob_stores is the only package
// that needs a directly-inspectable observer today.
type recordingObserver struct {
	events []domain_interfaces.BlobWriteEvent
}

func (o *recordingObserver) OnBlobPublished(ev domain_interfaces.BlobWriteEvent) {
	o.events = append(o.events, ev)
}

// TestSftpMoverEmitWriteEvent_FiresOnceWithWrittenOp pins #50: the SFTP
// mover must call observer.OnBlobPublished exactly once per successful
// upload with op = "written". We exercise the helper directly because
// the full Close() path requires a real *sftp.Client for Rename/Stat
// and mocking the upstream pkg/sftp API is out of scope for this fix.
// The Close() call site is visually verified and covered by a future
// bats SFTP integration test (tracked separately).
func TestSftpMoverEmitWriteEvent_FiresOnceWithWrittenOp(t *testing.T) {
	var id blob_store_id.Id
	if err := id.Set("sftp-default"); err != nil {
		t.Fatalf("blob_store_id.Set: %v", err)
	}

	observer := &recordingObserver{}

	store := &remoteSftp{
		id:       id,
		observer: observer,
	}
	mover := &sftpMover{
		store: store,
	}

	mover.emitWriteEvent(domain_interfaces.BlobWriteOpWritten, 4321)

	if len(observer.events) != 1 {
		t.Fatalf(
			"expected 1 observer event, got %d",
			len(observer.events),
		)
	}

	ev := observer.events[0]
	if ev.StoreId != "sftp-default" {
		t.Errorf("StoreId = %q, want sftp-default", ev.StoreId)
	}
	if ev.Size != 4321 {
		t.Errorf("Size = %d, want 4321", ev.Size)
	}
	if ev.Op != domain_interfaces.BlobWriteOpWritten {
		t.Errorf(
			"Op = %q, want %q",
			ev.Op, domain_interfaces.BlobWriteOpWritten,
		)
	}
}

// TestSftpMoverEmitWriteEvent_NilObserverIsNoop guards the
// write-log-disabled case: when the utility opts out (--no-write-log
// or MADDER_WRITE_LOG=0), command_components hands the store a
// NopObserver. remoteSftp.observer being nil must not panic; the
// emitter is the right layer to absorb that.
func TestSftpMoverEmitWriteEvent_NilObserverIsNoop(t *testing.T) {
	store := &remoteSftp{}
	mover := &sftpMover{store: store}

	// Panicking here would fail the test; finishing cleanly is the
	// success condition.
	mover.emitWriteEvent(domain_interfaces.BlobWriteOpWritten, 0)
}
