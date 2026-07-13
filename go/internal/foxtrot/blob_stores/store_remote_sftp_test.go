//go:build test

package blob_stores

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_io"
	"github.com/amarbel-llc/piggy/go/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/ohio"
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
	var id scoped_id.Id
	if err := id.Set("sftp-default"); err != nil {
		t.Fatalf("scoped_id.Set: %v", err)
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

// TestSftpHasBlob_UnavailableReturnsFalse pins the #209 fix: when the
// SSH dial / handshake / auth fails, HasBlob must return false rather
// than panic out of the caller's Run frame. Multi-store fallbacks and
// the per-command blobFromRemainingStores walk rely on this signal to
// skip a degraded backend and continue probing the rest.
func TestSftpHasBlob_UnavailableReturnsFalse(t *testing.T) {
	sentinelErr := errors.Errorf("ssh: handshake failed")

	store := &remoteSftp{
		sshClientInitializer: func() (*ssh.Client, error) {
			return nil, sentinelErr
		},
	}

	// nullId triggers the early-return branch normally, so use a
	// non-null id to make sure we reach the init path. The
	// implementation calls tryInitialize before checking the null
	// guard, so even the null-id case must observe false.
	id := makeSftpFallbackTestId(t)

	if got := store.HasBlob(id); got != false {
		t.Fatalf("HasBlob: got %v, want false on init failure", got)
	}
}

// TestSftpMakeBlobReader_UnavailableReturnsClassifiableError pins the
// other half of the #209 fix: MakeBlobReader on an unreachable backend
// must return an error that blob_io.IsBlobStoreUnavailable classifies
// as unavailable, so Multi.MakeBlobReader and the per-command fallback
// can treat the error as miss-equivalent and continue.
func TestSftpMakeBlobReader_UnavailableReturnsClassifiableError(t *testing.T) {
	sentinelErr := errors.Errorf("ssh: handshake failed")

	store := &remoteSftp{
		sshClientInitializer: func() (*ssh.Client, error) {
			return nil, sentinelErr
		},
	}

	id := makeSftpFallbackTestId(t)

	reader, err := store.MakeBlobReader(id)
	if reader != nil {
		t.Fatalf("MakeBlobReader: got non-nil reader on init failure")
	}
	if err == nil {
		t.Fatal("MakeBlobReader: got nil error on init failure")
	}
	if !blob_io.IsBlobStoreUnavailable(err) {
		t.Fatalf("MakeBlobReader error %v is not classified unavailable", err)
	}
	// Cause must be reachable via errors.Unwrap for callers that
	// want the raw transport error.
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("MakeBlobReader: unwrap chain does not reach root cause %v", err)
	}
}

// makeSftpFallbackTestId returns a deterministic sha256 markl id with
// a stable hex pattern; reused by the #209 fallback tests above.
func makeSftpFallbackTestId(t *testing.T) domain_interfaces.MarklId {
	t.Helper()
	id, repool := markl.FormatHashSha256.GetMarklIdForString("sftp-fallback")
	t.Cleanup(repool)
	return id
}

// spyActiveContext is a minimal interfaces.ActiveContext stub used by
// TestSftpInitializeOnce_DoesNotPoisonContext to assert that the
// failure path does not invoke Cancel on the captured context.
// Methods not exercised by the test return zero values.
type spyActiveContext struct {
	cancelCalls int
}

var _ interfaces.ActiveContext = (*spyActiveContext)(nil)

func (c *spyActiveContext) Deadline() (time.Time, bool)          { return time.Time{}, false }
func (c *spyActiveContext) Done() <-chan struct{}                { return nil }
func (c *spyActiveContext) Err() error                           { return nil }
func (c *spyActiveContext) Value(_ any) any                      { return nil }
func (c *spyActiveContext) Cause() error                         { return nil }
func (c *spyActiveContext) GetState() interfaces.ContextState    { return interfaces.ContextStateStarted }
func (c *spyActiveContext) Cancel(_ error)                       { c.cancelCalls++ }
func (c *spyActiveContext) After(_ interfaces.FuncActiveContext) {}
func (c *spyActiveContext) Must(_ interfaces.FuncActiveContext)  {}

// Compile-time guard for the "context" stdlib import the spy uses
// only as type evidence; the methods above implement the interface
// without delegating to a real context.Context.
var _ context.Context = (*spyActiveContext)(nil)

// TestAllBlobsForBase_YieldsLexByteOrder pins the BlobStore.AllBlobs
// ordering contract over the parallel-walk fan-out introduced in PR
// #192. The pre-PR Walk did not sort; the fan-out sorts each bucket's
// ids internally AND consumes buckets in sorted name order, so the
// full cross-bucket stream is in lex byte order. Without this test
// the fan-out machinery could silently regress to per-bucket-only
// order (or, after the simplification that removed the dispatcher
// goroutine + close-once guards, to no order at all).
func TestAllBlobsForBase_YieldsLexByteOrder(t *testing.T) {
	h := newSFTPBenchHarness(t, 0)
	defer h.Close()
	wantCount := populateBuckets(t, h.client, h.remotePath)

	store := newBenchStore(t, h.client, h.remotePath)

	var got []domain_interfaces.MarklId
	for id, err := range store.allBlobsForBase(
		h.remotePath,
		markl.FormatHashSha256,
	) {
		if err != nil {
			t.Fatalf("allBlobsForBase: %v", err)
		}
		got = append(got, id)
	}

	if len(got) != wantCount {
		t.Fatalf(
			"yielded %d blobs, populateBuckets created %d",
			len(got), wantCount,
		)
	}

	for i := 1; i < len(got); i++ {
		prev := got[i-1].GetBytes()
		cur := got[i].GetBytes()
		if bytes.Compare(prev, cur) > 0 {
			t.Fatalf(
				"ids not in lex byte order: got[%d]=%x > got[%d]=%x",
				i-1, prev, i, cur,
			)
		}
	}
}

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

// nopHashTestBlobIOWrapper is a no-compression / no-encryption
// BlobIOWrapper for the hash-type write/read tests below.
// makeEnvDirConfig needs a non-nil wrapper; nil encryption makes
// MakeConfig fall back to its nop wrapper, and NopeIOWrapper leaves
// bytes verbatim so the harness server stores exactly what was
// written.
type nopHashTestBlobIOWrapper struct{}

func (nopHashTestBlobIOWrapper) GetBlobEncryption() domain_interfaces.MarklId {
	return nil
}

func (nopHashTestBlobIOWrapper) GetBlobCompression() interfaces.IOWrapper {
	return ohio.NopeIOWrapper{}
}

// newSftpHashTestStore builds a remoteSftp wired to a harness client
// with the hash-type knobs the #261 multi-hash tests exercise. Like
// newBenchStore it latches once.Do so initialize() (which needs a real
// ssh.Client and a remote blob_store-config) is skipped; unlike it, the
// caller controls multiHash / defaultHashType and a nop blobIOWrapper is
// attached so the write and read paths can build an env-dir config.
func newSftpHashTestStore(
	tb testing.TB,
	client *sftp.Client,
	remotePath string,
	multiHash bool,
	defaultHash markl.FormatHash,
) *remoteSftp {
	tb.Helper()
	var id scoped_id.Id
	if err := id.Set("sftp-hashtest"); err != nil {
		tb.Fatalf("scoped_id.Set: %v", err)
	}
	store := &remoteSftp{
		ctx:             benchContext{},
		id:              id,
		config:          benchRemotePathConfig{remotePath: remotePath},
		buckets:         defaultBuckets,
		multiHash:       multiHash,
		defaultHashType: defaultHash,
		blobIOWrapper:   nopHashTestBlobIOWrapper{},
		blobCache:       map[string]struct{}{},
		sftpClient:      client,
	}
	store.once.Do(func() {}) // latch: initialize() will not run
	return store
}

// TestSftpMakeBlobWriter_MultiHashHonorsRequestedType pins the #261
// write-side fix: a multi-hash SFTP store must digest the blob with the
// caller-requested hash type and land it under the matching <format-id>/
// subtree, not silently substitute the store default. Pre-fix the writer
// dropped the argument and always used defaultHashType.
func TestSftpMakeBlobWriter_MultiHashHonorsRequestedType(t *testing.T) {
	h := newSFTPBenchHarness(t, 0)
	defer h.Close()

	store := newSftpHashTestStore(
		t, h.client, h.remotePath, true, markl.FormatHashSha256,
	)

	writer, err := store.MakeBlobWriter(markl.FormatHashBlake2b256)
	if err != nil {
		t.Fatalf("MakeBlobWriter(blake2b256): %v", err)
	}

	payload := []byte("multi-hash-sftp-write-payload")
	if _, err = writer.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	id := writer.GetMarklId()
	wantFormat := markl.FormatHashBlake2b256.GetMarklFormatId()
	if got := id.GetMarklFormat().GetMarklFormatId(); got != wantFormat {
		t.Fatalf(
			"written blob id format = %q, want %q (requested type ignored)",
			got, wantFormat,
		)
	}

	// The blob must physically land under the requested type's subtree
	// — proof the multi-hash path layout followed the digest's format,
	// not the store default.
	blobPath := store.remotePathForMerkleId(id)
	if !strings.Contains(blobPath, wantFormat) {
		t.Fatalf(
			"blob path %q is not under the %q subtree",
			blobPath, wantFormat,
		)
	}
	if _, err = os.Stat(blobPath); err != nil {
		t.Fatalf("blob not written at %q: %v", blobPath, err)
	}
}

// TestSftpMultiHash_RoundTrip pins the read-side half of the #261 fix:
// once a multi-hash store can write a non-default hash type, it must also
// read it back verifying under that same type. Pre-fix MakeBlobReader
// hardcoded defaultHashType, so a blake2b blob read back yielded a sha256
// id that never matched the requested digest.
func TestSftpMultiHash_RoundTrip(t *testing.T) {
	h := newSFTPBenchHarness(t, 0)
	defer h.Close()

	store := newSftpHashTestStore(
		t, h.client, h.remotePath, true, markl.FormatHashSha256,
	)

	payload := []byte("multi-hash-sftp-roundtrip-payload")

	writer, err := store.MakeBlobWriter(markl.FormatHashBlake2b256)
	if err != nil {
		t.Fatalf("MakeBlobWriter(blake2b256): %v", err)
	}
	if _, err = writer.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	id := writer.GetMarklId()

	reader, err := store.MakeBlobReader(id)
	if err != nil {
		t.Fatalf("MakeBlobReader: %v", err)
	}
	t.Cleanup(func() { _ = reader.Close() })

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("read payload %q != written %q", got, payload)
	}

	// The reader must have digested under the blob's own format; its
	// computed id must reproduce the requested blake2b digest.
	if readID := reader.GetMarklId(); !bytes.Equal(
		readID.GetBytes(), id.GetBytes(),
	) {
		t.Fatalf(
			"reader id %x != written id %x (read verified under wrong hash)",
			readID.GetBytes(), id.GetBytes(),
		)
	}
}

// TestSftpMakeBlobWriter_SingleHashRejectsForeignType pins the loud-fail
// half of #261: a single-hash store can only ever hold its configured
// type, so requesting a different one must return an error rather than
// silently write a blob under the default hash. No temp file may leak.
func TestSftpMakeBlobWriter_SingleHashRejectsForeignType(t *testing.T) {
	h := newSFTPBenchHarness(t, 0)
	defer h.Close()

	store := newSftpHashTestStore(
		t, h.client, h.remotePath, false, markl.FormatHashSha256,
	)

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

	entries, readErr := os.ReadDir(h.remotePath)
	if readErr != nil {
		t.Fatalf("ReadDir(%q): %v", h.remotePath, readErr)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "tmp_") {
			t.Errorf("stray temp file created on rejected write: %q", e.Name())
		}
	}
}

// TestSftpMakeBlobWriter_SingleHashAcceptsNilAndMatching guards the
// non-mismatch paths of the #261 loud-fail check: a nil request (fall
// back to default) and an explicit request for the store's own type must
// both still succeed and write under the default hash.
func TestSftpMakeBlobWriter_SingleHashAcceptsNilAndMatching(t *testing.T) {
	wantFormat := markl.FormatHashSha256.GetMarklFormatId()

	cases := []struct {
		name    string
		request domain_interfaces.FormatHash
	}{
		{"nil", nil},
		{"matching", markl.FormatHashSha256},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newSFTPBenchHarness(t, 0)
			defer h.Close()

			store := newSftpHashTestStore(
				t, h.client, h.remotePath, false, markl.FormatHashSha256,
			)

			writer, err := store.MakeBlobWriter(tc.request)
			if err != nil {
				t.Fatalf("MakeBlobWriter(%s): %v", tc.name, err)
			}
			if _, err = writer.Write([]byte("single-hash-payload")); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if err = writer.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			if got := writer.GetMarklId().
				GetMarklFormat().GetMarklFormatId(); got != wantFormat {
				t.Fatalf("blob id format = %q, want %q", got, wantFormat)
			}
		})
	}
}
