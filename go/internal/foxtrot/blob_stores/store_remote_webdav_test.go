//go:build test

package blob_stores

import (
	"net/http"
	"strings"
	"testing"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/piggy/go/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
)

// TestWebdavMakeBlobWriter_SingleHashRejectsForeignType pins the #262
// loud-fail guard for WebDAV: a single-hash store must reject a request
// for a hash type other than its configured one before any network call
// (the guard runs ahead of mover.initialize, so once is latched to skip
// the live-connection path).
func TestWebdavMakeBlobWriter_SingleHashRejectsForeignType(t *testing.T) {
	var id scoped_id.Id
	if err := id.Set("webdav-hashtest"); err != nil {
		t.Fatalf("scoped_id.Set: %v", err)
	}

	store := &remoteWebdav{
		remoteBlobStoreBase: remoteBlobStoreBase{
			id:              id,
			multiHash:       false,
			defaultHashType: markl.FormatHashSha256,
		},
	}
	store.once.Do(func() {}) // latch: initialize() will not run

	writer, err := store.MakeBlobWriter(markl.FormatHashBlake2b256)
	if writer != nil {
		t.Fatalf("MakeBlobWriter: got non-nil writer on rejected request")
	}
	if err == nil {
		t.Fatal("MakeBlobWriter: got nil error requesting a foreign hash type")
	}
	if !strings.Contains(err.Error(), "single-hash") {
		t.Errorf("error %q missing 'single-hash' anchor", err.Error())
	}
}

// TestWebdavMoverEmitWriteEvent_FiresOnceWithWrittenOp mirrors the
// SFTP pin: the mover must call observer.OnBlobPublished exactly
// once per successful upload with op = "written". Exercises the
// helper directly because the full Close() path requires a live
// WebDAV server; that's covered by webdav.bats.
func TestWebdavMoverEmitWriteEvent_FiresOnceWithWrittenOp(t *testing.T) {
	var id scoped_id.Id
	if err := id.Set("webdav-default"); err != nil {
		t.Fatalf("scoped_id.Set: %v", err)
	}

	observer := &recordingObserver{}

	store := &remoteWebdav{
		remoteBlobStoreBase: remoteBlobStoreBase{
			id:       id,
			observer: observer,
		},
	}
	mover := &webdavMover{
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
	if ev.StoreId != "webdav-default" {
		t.Errorf("StoreId = %q, want webdav-default", ev.StoreId)
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

// TestWebdavMoverEmitWriteEvent_NilObserverIsNoop guards the
// inventory-log-disabled case: command_components hands the store
// a NopObserver when --no-inventory-log or MADDER_INVENTORY_LOG=0
// is in effect. remoteWebdav.observer being nil must not panic.
func TestWebdavMoverEmitWriteEvent_NilObserverIsNoop(t *testing.T) {
	store := &remoteWebdav{}
	mover := &webdavMover{store: store}

	mover.emitWriteEvent(domain_interfaces.BlobWriteOpWritten, 0)
}

// TestWebdavMoverGetMarklId_PanicsBeforeInitialize is the WebDAV
// twin of the SFTP pin (#184): defending against a state that
// shouldn't exist (initialize sets writer before MakeBlobWriter
// returns) MUST fail loudly if the invariant is violated, not
// silently return nil or stack-overflow.
func TestWebdavMoverGetMarklId_PanicsBeforeInitialize(t *testing.T) {
	mover := &webdavMover{}

	r := recoverPanic(func() { _ = mover.GetMarklId() })
	if r == nil {
		t.Fatal("GetMarklId on nil-writer mover did not panic")
	}
	err, ok := r.(error)
	if !ok {
		t.Fatalf("panic value is not an error: %T %v", r, r)
	}
	if !strings.Contains(err.Error(), "mover.writer is nil") {
		t.Errorf("panic error %q missing 'mover.writer is nil' anchor", err.Error())
	}
}

// TestWebdavInitializeOnce_PanicsOnInitFailure mirrors the SFTP
// pin on issue #134: when httpClientInitializer returns an error,
// HasBlob/AllBlobs callers must see a panic carrying the wrapped
// underlying error rather than a dewey ContextState sentinel.
func TestWebdavInitializeOnce_PanicsOnInitFailure(t *testing.T) {
	sentinelErr := errors.Errorf("http client init blew up")

	store := &remoteWebdav{
		httpClientInitializer: func() (*http.Client, error) {
			return nil, sentinelErr
		},
	}

	r := recoverPanic(func() { store.initializeOnce() })
	if r == nil {
		t.Fatal("initializeOnce did not panic on init failure")
	}

	err, ok := r.(error)
	if !ok {
		t.Fatalf("panic value is not an error: %T %v", r, r)
	}
	if !strings.Contains(err.Error(), "http client init blew up") {
		t.Errorf(
			"panic error %q does not contain the underlying init "+
				"failure message",
			err.Error(),
		)
	}
}

// TestWebdavInitializeOnce_RepanicsAcrossCalls mirrors the SFTP
// pin on the sync.Once-doesn't-re-run-after-panic gotcha.
func TestWebdavInitializeOnce_RepanicsAcrossCalls(t *testing.T) {
	calls := 0
	sentinelErr := errors.Errorf("http client init blew up")

	store := &remoteWebdav{
		httpClientInitializer: func() (*http.Client, error) {
			calls++
			return nil, sentinelErr
		},
	}

	for i := 0; i < 3; i++ {
		r := recoverPanic(func() { store.initializeOnce() })
		if r == nil {
			t.Fatalf("call %d did not panic", i+1)
		}
		err, ok := r.(error)
		if !ok {
			t.Fatalf("call %d: panic value is not an error: %T %v", i+1, r, r)
		}
		if !strings.Contains(err.Error(), "http client init blew up") {
			t.Errorf(
				"call %d: panic error %q does not contain the "+
					"underlying init failure message",
				i+1, err.Error(),
			)
		}
	}

	if calls != 1 {
		t.Errorf(
			"httpClientInitializer was called %d times; "+
				"sync.Once.Do should run f exactly once even "+
				"after panics",
			calls,
		)
	}
}

// TestWebdavInitializeOnce_DoesNotPoisonContext pins the structural
// reason init-failure-via-panic matters for long-lived servers
// (madder-mcp serve): the captured context must not be touched on
// the failure path.
func TestWebdavInitializeOnce_DoesNotPoisonContext(t *testing.T) {
	tracker := &spyActiveContext{}

	store := &remoteWebdav{
		remoteBlobStoreBase: remoteBlobStoreBase{ctx: tracker},
		httpClientInitializer: func() (*http.Client, error) {
			return nil, errors.Errorf("http client init blew up")
		},
	}

	_ = recoverPanic(func() { store.initializeOnce() })

	if tracker.cancelCalls != 0 {
		t.Errorf(
			"ctx.Cancel was called %d times during init failure; "+
				"the failure path should never touch the captured "+
				"context",
			tracker.cancelCalls,
		)
	}
}
