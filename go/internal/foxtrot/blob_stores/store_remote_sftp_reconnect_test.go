//go:build test

package blob_stores

import (
	"bytes"
	stderrors "errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_io"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

// reconnectRig hands a store connections to one in-process sftp server's
// directory and lets a test break the live one, as a dropped network path
// does (madder#306).
type reconnectRig struct {
	t    *testing.T
	root string

	mu        sync.Mutex
	current   *sftpBenchHarness
	harnesses []*sftpBenchHarness
	dials     int
	failDials int
}

func newReconnectRig(t *testing.T) *reconnectRig {
	t.Helper()

	rig := &reconnectRig{t: t, root: t.TempDir()}

	t.Cleanup(func() {
		for _, harness := range rig.harnesses {
			harness.Close()
		}
	})

	return rig
}

func (rig *reconnectRig) connect() (*ssh.Client, *sftp.Client, error) {
	rig.mu.Lock()
	defer rig.mu.Unlock()

	rig.dials++

	if rig.failDials > 0 {
		rig.failDials--
		return nil, nil, errors.Errorf("dial refused")
	}

	harness := newSFTPBenchHarness(rig.t, 0, concurrentClientOptions()...)
	rig.current = harness
	rig.harnesses = append(rig.harnesses, harness)

	return nil, harness.client, nil
}

// drop breaks the live connection. Requests on it fail with the sftp
// client's connection-lost error.
func (rig *reconnectRig) drop() {
	rig.mu.Lock()
	harness := rig.current
	rig.mu.Unlock()

	harness.Close()
}

func (rig *reconnectRig) dialCount() int {
	rig.mu.Lock()
	defer rig.mu.Unlock()

	return rig.dials
}

func (rig *reconnectRig) refuseAllDials() {
	rig.mu.Lock()
	defer rig.mu.Unlock()

	rig.failDials = 1 << 20
}

func fastReconnects(t *testing.T) []time.Duration {
	t.Helper()

	previous := sftpReconnectDelays
	sftpReconnectDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}

	t.Cleanup(func() { sftpReconnectDelays = previous })

	return sftpReconnectDelays
}

func newReconnectingStore(t *testing.T, rig *reconnectRig) *remoteSftp {
	t.Helper()

	_, client, err := rig.connect()
	if err != nil {
		t.Fatalf("first connect: %v", err)
	}

	store := newSftpHashTestStore(
		t, client, rig.root, true, markl.FormatHashBlake2b256,
	)
	store.connector = rig.connect

	return store
}

// seedBlob puts a blob in the rig's data root through a second store, so the
// store under test has not seen it and must ask the remote.
func (rig *reconnectRig) seedBlob(t *testing.T, payload string) domain_interfaces.MarklId {
	t.Helper()

	h := newSFTPBenchHarness(t, 0, concurrentClientOptions()...)
	defer h.Close()

	seeder := newSftpHashTestStore(
		t, h.client, rig.root, true, markl.FormatHashBlake2b256,
	)

	return writeSftpTestBlob(t, seeder, payload)
}

func tempFilesUnder(t *testing.T, root string) (names []string) {
	t.Helper()

	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasPrefix(info.Name(), "tmp_") {
			names = append(names, path)
		}

		return nil
	})

	return names
}

// A request that finds the connection dropped reconnects and goes through.
func TestSftpReconnect_RequestsSurviveADrop(t *testing.T) {
	fastReconnects(t)

	rig := newReconnectRig(t)
	store := newReconnectingStore(t, rig)

	first := rig.seedBlob(t, "written before the drop")

	rig.drop()

	if !store.HasBlob(first) {
		t.Fatalf("HasBlob: false after a drop, want a reconnect and the answer")
	}

	if got := rig.dialCount(); got != 2 {
		t.Errorf("dialled %d times, want 2 (the first connection and one reconnect)", got)
	}

	// Writing and reading work on the new connection.
	second := writeSftpTestBlob(t, store, "written after the drop")

	reader, err := store.MakeBlobReader(second)
	if err != nil {
		t.Fatalf("MakeBlobReader: %v", err)
	}
	defer reader.Close() //defer:err-checked

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if string(got) != "written after the drop" {
		t.Errorf("read %q", got)
	}

	if got := rig.dialCount(); got != 2 {
		t.Errorf("dialled %d times, want still 2", got)
	}
}

// A listing whose connection drops part-way reconnects and walks again, so
// the caller gets every blob and no error, not a short list.
func TestSftpReconnect_ListingSurvivesADrop(t *testing.T) {
	fastReconnects(t)

	rig := newReconnectRig(t)
	store := newReconnectingStore(t, rig)

	want := map[string]bool{}

	for _, payload := range []string{"one", "two", "three", "four"} {
		want[writeSftpTestBlob(t, store, payload).String()] = true
	}

	rig.drop()

	got := map[string]bool{}

	for id, err := range store.AllBlobs() {
		if err != nil {
			t.Fatalf("AllBlobs after a drop: %v", err)
		}

		got[id.String()] = true
	}

	if len(got) != len(want) {
		t.Fatalf("listed %d blobs after a drop, want %d", len(got), len(want))
	}

	for id := range want {
		if !got[id] {
			t.Errorf("blob %s missing from the listing", id)
		}
	}
}

// Many requests that all find the dead connection make one new one between
// them.
func TestSftpReconnect_ConcurrentRequestsReconnectOnce(t *testing.T) {
	fastReconnects(t)

	rig := newReconnectRig(t)
	store := newReconnectingStore(t, rig)

	present := rig.seedBlob(t, "shared")

	rig.drop()

	var wg sync.WaitGroup

	for range 16 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			if !store.HasBlob(present) {
				t.Errorf("HasBlob reported a present blob absent")
			}
		}()
	}

	wg.Wait()

	if got := rig.dialCount(); got != 2 {
		t.Errorf("dialled %d times for 16 concurrent requests, want 2", got)
	}
}

// When the remote stays unreachable the store gives up after its bounded
// attempts and then fails at once, naming why, without dialling again.
// Failing each blob after its own backoff is how a drop became 20,000
// failures (madder#306).
func TestSftpReconnect_GivesUpAndStaysGivenUp(t *testing.T) {
	delays := fastReconnects(t)

	rig := newReconnectRig(t)
	store := newReconnectingStore(t, rig)

	id := writeSftpTestBlob(t, store, "unreachable later")

	rig.drop()
	rig.refuseAllDials()

	before := rig.dialCount()

	_, err := store.MakeBlobReader(id)
	if err == nil {
		t.Fatalf("MakeBlobReader succeeded against an unreachable remote")
	}

	if !IsStoreGivenUp(err) {
		t.Fatalf("error is not a given-up store: %v", err)
	}

	if !strings.Contains(ErrorText(err), "dial refused") {
		t.Errorf("error does not say why: %s", ErrorText(err))
	}

	attempts := rig.dialCount() - before
	if want := 1 + len(delays); attempts != want {
		t.Errorf("made %d reconnect attempts, want %d", attempts, want)
	}

	started := time.Now()

	for range 50 {
		if _, err = store.MakeBlobReader(id); !IsStoreGivenUp(err) {
			t.Fatalf("later request did not fail as given up: %v", err)
		}
	}

	if got := rig.dialCount() - before; got != attempts {
		t.Errorf("dialled again after giving up: %d attempts, want %d", got, attempts)
	}

	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("50 requests after giving up took %s, want them instant", elapsed)
	}
}

// A write cut off by a drop fails with an error, not a panic; it leaves no
// temp file behind; and the store carries on with the next blob.
func TestSftpReconnect_WriteCutOffByADrop(t *testing.T) {
	fastReconnects(t)

	rig := newReconnectRig(t)
	store := newReconnectingStore(t, rig)

	writer, err := store.MakeBlobWriter(markl.FormatHashBlake2b256)
	if err != nil {
		t.Fatalf("MakeBlobWriter: %v", err)
	}

	if _, err = writer.Write([]byte("cut off")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	rig.drop()

	var closeErr error

	if recovered := recoverPanic(func() { closeErr = writer.Close() }); recovered != nil {
		t.Fatalf("Close panicked: %v", recovered)
	}

	if closeErr == nil {
		t.Fatalf("Close succeeded although the connection dropped under the write")
	}

	if strings.Contains(closeErr.Error(), "error group") &&
		!strings.Contains(ErrorText(closeErr), "connection") {
		t.Errorf("error does not say what happened: %s", ErrorText(closeErr))
	}

	if left := tempFilesUnder(t, rig.root); len(left) != 0 {
		t.Errorf("temp files left on the remote: %v", left)
	}

	next := writeSftpTestBlob(t, store, "the next blob")

	if !store.HasBlob(next) {
		t.Errorf("the store did not recover for the next blob")
	}
}

// A blob copy that a drop interrupts is tried once more on the fresh
// connection, so one drop costs a retry and not a failed blob.
// A request that fails with the bare io.EOF an ssh channel returns when it
// closes under it must come back as a connection-lost error the callers can
// wrap, not as a value that makes errors.Wrap panic.
func TestSftpWithClient_BareEOFBecomesConnectionLost(t *testing.T) {
	fastReconnects(t)

	rig := newReconnectRig(t)
	store := newReconnectingStore(t, rig)

	var (
		err       error
		recovered any
	)

	// As in the other tests the connection is dropped first: closing a
	// still-live harness client waits on a pipe half only drop() closes.
	rig.drop()

	finished := make(chan struct{})

	go func() {
		defer close(finished)

		recovered = recoverPanic(func() {
			_, err = withClient(store, func(*sftp.Client) (struct{}, error) {
				return struct{}{}, io.EOF
			})
			err = errors.Wrap(err)
		})
	}()

	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatalf("withClient did not return within 10s")
	}

	if recovered != nil {
		t.Fatalf("wrapping the result panicked: %v", recovered)
	}

	if !IsConnectionLost(err) {
		t.Errorf("a bare EOF came back as %v, want connection lost", err)
	}
}

func TestIsConnectionLost(t *testing.T) {
	for name, err := range map[string]error{
		"connection lost": sftp.ErrSSHFxConnectionLost,
		"no connection":   sftp.ErrSSHFxNoConnection,
		"wrapped":         errors.Wrap(sftp.ErrSSHFxConnectionLost),
	} {
		if !IsConnectionLost(err) {
			t.Errorf("%s: not recognised as a lost connection", name)
		}
	}

	for name, err := range map[string]error{
		"nil":        nil,
		"eof":        io.EOF,
		"not exist":  os.ErrNotExist,
		"given up":   blob_io.ErrBlobStoreUnavailable{StoreId: "x"},
		"permission": sftp.ErrSSHFxPermissionDenied,
	} {
		if IsConnectionLost(err) {
			t.Errorf("%s: wrongly recognised as a lost connection", name)
		}
	}
}

// A joined error reads as one line with the errors inside it.
func TestErrorText_SpellsOutJoinedErrors(t *testing.T) {
	joined := stderrors.Join(
		errors.Errorf("copying blob: connection lost"),
		errors.Errorf("removing temp file: connection lost"),
	)

	text := ErrorText(joined)

	for _, want := range []string{"copying blob: connection lost", "removing temp file: connection lost"} {
		if !strings.Contains(text, want) {
			t.Errorf("ErrorText = %q, want it to contain %q", text, want)
		}
	}

	if strings.Contains(text, "error group") {
		t.Errorf("ErrorText = %q still reads as an anonymous group", text)
	}

	if got := ErrorText(nil); got != "" {
		t.Errorf("ErrorText(nil) = %q", got)
	}

	if got := ErrorText(bytes.ErrTooLarge); got != bytes.ErrTooLarge.Error() {
		t.Errorf("ErrorText of a plain error = %q", got)
	}
}
