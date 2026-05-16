//go:build test

package blob_stores

import (
	"context"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
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
// inventory-log-disabled case: when the utility opts out
// (--no-inventory-log or MADDER_INVENTORY_LOG=0), command_components
// hands the store a NopObserver. remoteSftp.observer being nil must
// not panic; the emitter is the right layer to absorb that.
func TestSftpMoverEmitWriteEvent_NilObserverIsNoop(t *testing.T) {
	store := &remoteSftp{}
	mover := &sftpMover{store: store}

	// Panicking here would fail the test; finishing cleanly is the
	// success condition.
	mover.emitWriteEvent(domain_interfaces.BlobWriteOpWritten, 0)
}

// TestSftpMoverGetMarklId_PanicsBeforeInitialize pins #184: the
// pre-fix code self-recursed on nil writer, producing a stack overflow
// instead of a debuggable error. The branch defends against a state
// that shouldn't exist — initialize sets writer before MakeBlobWriter
// returns the mover — but the defense MUST fail loudly if the
// invariant is ever violated, not silently or by stack overflow.
func TestSftpMoverGetMarklId_PanicsBeforeInitialize(t *testing.T) {
	mover := &sftpMover{}

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

// recoverPanic runs f and returns the panic value, or nil if f did not
// panic. Used by the initializeOnce panic-semantics tests below where
// each case asserts both that a panic happened and what its payload
// is.
func recoverPanic(f func()) (r any) {
	defer func() { r = recover() }()
	f()
	return r
}

// TestSftpInitializeOnce_PanicsOnInitFailure pins issue #134's first
// failure mode: when sshClientInitializer returns an error, the
// caller of HasBlob/AllBlobs/etc. should see a panic carrying the
// wrapped underlying error rather than the dewey
// ContextStateSucceeded sentinel that the pre-fix Cancel(err) path
// produced.
func TestSftpInitializeOnce_PanicsOnInitFailure(t *testing.T) {
	sentinelErr := errors.Errorf("ssh dial blew up")

	store := &remoteSftp{
		sshClientInitializer: func() (*ssh.Client, error) {
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
	if !strings.Contains(err.Error(), "ssh dial blew up") {
		t.Errorf(
			"panic error %q does not contain the underlying ssh "+
				"failure message",
			err.Error(),
		)
	}
}

// TestSftpInitializeOnce_RepanicsAcrossCalls pins issue #134's second
// failure mode: sync.Once.Do does not re-run f after a panic, so
// without an initErr field the second call would silently no-op and
// downstream usage would NPE on sftpClient = nil. The fix caches the
// init error and re-panics it on every subsequent call.
func TestSftpInitializeOnce_RepanicsAcrossCalls(t *testing.T) {
	calls := 0
	sentinelErr := errors.Errorf("ssh dial blew up")

	store := &remoteSftp{
		sshClientInitializer: func() (*ssh.Client, error) {
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
		if !strings.Contains(err.Error(), "ssh dial blew up") {
			t.Errorf(
				"call %d: panic error %q does not contain the "+
					"underlying ssh failure message",
				i+1, err.Error(),
			)
		}
	}

	// sync.Once.Do contract: f runs exactly once, even if it
	// panicked. The retry-each-call panic path must not re-enter
	// initialize() and re-dial; reading initErr is the cheap path.
	if calls != 1 {
		t.Errorf(
			"sshClientInitializer was called %d times; "+
				"sync.Once.Do should run f exactly once even "+
				"after panics",
			calls,
		)
	}
}

// TestSftpInitializeOnce_DoesNotPoisonContext pins the structural
// reason this fix matters for long-lived servers (madder-mcp serve):
// the pre-fix path called ctx.Cancel(err), which closes the shared
// context's Done channel as a side effect. With the fix, init failure
// must not touch the context at all — only panic. We assert this by
// constructing the store with a ctx that records calls; the fix means
// ctx is reachable through the field but never invoked from
// initializeOnce on the failure path.
func TestSftpInitializeOnce_DoesNotPoisonContext(t *testing.T) {
	tracker := &spyActiveContext{}

	store := &remoteSftp{
		ctx: tracker,
		sshClientInitializer: func() (*ssh.Client, error) {
			return nil, errors.Errorf("ssh dial blew up")
		},
	}

	_ = recoverPanic(func() { store.initializeOnce() })

	if tracker.cancelCalls != 0 {
		t.Errorf(
			"ctx.Cancel was called %d times during init failure; "+
				"the fix should never touch the captured "+
				"context on the failure path",
			tracker.cancelCalls,
		)
	}
}

// spyActiveContext is a minimal interfaces.ActiveContext stub used by
// TestSftpInitializeOnce_DoesNotPoisonContext to assert that the
// failure path does not invoke Cancel on the captured context.
// Methods not exercised by the test return zero values.
type spyActiveContext struct {
	cancelCalls int
}

var _ interfaces.ActiveContext = (*spyActiveContext)(nil)

func (c *spyActiveContext) Deadline() (time.Time, bool)            { return time.Time{}, false }
func (c *spyActiveContext) Done() <-chan struct{}                  { return nil }
func (c *spyActiveContext) Err() error                             { return nil }
func (c *spyActiveContext) Value(_ any) any                        { return nil }
func (c *spyActiveContext) Cause() error                           { return nil }
func (c *spyActiveContext) GetState() interfaces.ContextState      { return interfaces.ContextStateStarted }
func (c *spyActiveContext) Cancel(_ error)                         { c.cancelCalls++ }
func (c *spyActiveContext) After(_ interfaces.FuncActiveContext)   {}
func (c *spyActiveContext) Must(_ interfaces.FuncActiveContext)    {}

// Compile-time guard for the "context" stdlib import the spy uses
// only as type evidence; the methods above implement the interface
// without delegating to a real context.Context.
var _ context.Context = (*spyActiveContext)(nil)

// TestShouldSkipBlobWalkEntry pins #148: the blob walker must skip
// blob_store-config and tmp_* entries so they're not yielded as
// fake blobs and parsed as hex digests. The bug surfaced for
// single-hash stores where the walker iterates <root> directly and
// the config file is a sibling of bucket dirs.
func TestShouldSkipBlobWalkEntry(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"blob_store-config", true},
		{"tmp_abcdef", true},
		{"tmp_", true},
		// Real blob filenames (62 hex chars after a 2-char bucket;
		// trailing 62 chars are the leaf in a buckets=[2] layout)
		// must NOT be skipped.
		{"abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", false},
		{"00", false},
		// Edge cases that look adjacent but are not the targeted
		// names.
		{"blob_store-config.bak", false},
		{"tmp", false}, // no trailing _
		{"", false},
	}

	for _, tc := range cases {
		got := shouldSkipBlobWalkEntry(tc.name)
		if got != tc.want {
			t.Errorf("shouldSkipBlobWalkEntry(%q) = %v, want %v",
				tc.name, got, tc.want)
		}
	}
}
