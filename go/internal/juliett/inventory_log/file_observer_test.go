package inventory_log

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
)

// TestFileObserver_NoEmitNoFile pins the laziness contract: a session
// that never emits produces no file on disk.
func TestFileObserver_NoEmitNoFile(t *testing.T) {
	dir := t.TempDir()
	o := NewFileObserver(dir)
	if err := o.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty dir, got %d entries", len(entries))
	}
}

// TestFileObserver_Integration_Native emits a native BlobWriteEvent
// through FileObserver, closes the session, then re-parses the on-disk
// hyphence + NDJSON document and asserts the contents.
func TestFileObserver_Integration_Native(t *testing.T) {
	withFixedClock(t)

	dir := t.TempDir()
	o := NewFileObserver(dir)
	o.randHex = func() string { return "abcd" }

	o.OnBlobPublished(domain_interfaces.BlobWriteEvent{
		StoreId: "default",
		Size:    1234,
		Op:      domain_interfaces.BlobWriteOpWritten,
	})

	if err := o.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	dayDir := filepath.Join(dir, "2026-04-26")
	entries, err := os.ReadDir(dayDir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dayDir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 session file, got %d: %v", len(entries), entries)
	}

	if !strings.HasSuffix(entries[0].Name(), "-abcd.hyphence") {
		t.Errorf("session file name: got %q, want suffix -abcd.hyphence", entries[0].Name())
	}

	content, err := os.ReadFile(filepath.Join(dayDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}

	body := assertHyphenceHeader(t, content)

	lines := splitNDJSON(body)
	if len(lines) != 1 {
		t.Fatalf("expected 1 NDJSON line, got %d:\n%s", len(lines), body)
	}

	var rec map[string]any
	if err := json.Unmarshal(lines[0], &rec); err != nil {
		t.Fatalf("parse line: %v\n%s", err, lines[0])
	}
	if rec["type"] != "blob-write-published-v1" {
		t.Errorf("type field: got %v, want blob-write-published-v1", rec["type"])
	}
	if rec["op"] != "written" {
		t.Errorf("op field: got %v, want written", rec["op"])
	}
}

// myCustomEvent is an importer-defined LogEvent used only in this test
// suite. Its LogType is unique within this package's tests.
type myCustomEvent struct {
	Foo string `json:"foo"`
	Bar int    `json:"bar"`
}

func (myCustomEvent) LogType() string { return "inventory_log_test-myevent-v1" }

// TestFileObserver_Integration_ImporterExtension emits an importer-
// defined event registered globally and asserts it round-trips through
// the session file.
func TestFileObserver_Integration_ImporterExtension(t *testing.T) {
	withFixedClock(t)

	// Encode wraps the typed event in a JSON object that includes the
	// "type" discriminator. This mirrors what an importer would write.
	encode := func(e myCustomEvent) ([]byte, error) {
		type wire struct {
			Type string `json:"type"`
			Foo  string `json:"foo"`
			Bar  int    `json:"bar"`
		}
		return json.Marshal(wire{Type: e.LogType(), Foo: e.Foo, Bar: e.Bar})
	}
	decode := func(line []byte) (myCustomEvent, error) {
		var e myCustomEvent
		err := json.Unmarshal(line, &e)
		return e, err
	}

	codec := MakeCodec[myCustomEvent](
		"inventory_log_test-myevent-v1",
		encode,
		decode,
	)
	Global.Register(codec)

	dir := t.TempDir()
	o := NewFileObserver(dir)
	o.randHex = func() string { return "ext0" }

	o.Emit(myCustomEvent{Foo: "hello", Bar: 42})

	if err := o.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	dayDir := filepath.Join(dir, "2026-04-26")
	entries, _ := os.ReadDir(dayDir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 session file, got %d", len(entries))
	}

	content, _ := os.ReadFile(filepath.Join(dayDir, entries[0].Name()))
	body := assertHyphenceHeader(t, content)
	lines := splitNDJSON(body)
	if len(lines) != 1 {
		t.Fatalf("expected 1 NDJSON line, got %d", len(lines))
	}

	got, err := codec.Decode(lines[0])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	gotEvent := got.(myCustomEvent)
	if gotEvent.Foo != "hello" || gotEvent.Bar != 42 {
		t.Errorf("decoded event: got %+v, want {Foo:hello Bar:42}", gotEvent)
	}
}

// CapturingObserver is the canonical test-capture pattern from the
// design doc: an Observer implementation that collects LogEvents in a
// slice rather than serializing them to a file.
type CapturingObserver struct {
	Events []domain_interfaces.LogEvent
}

func (c *CapturingObserver) Emit(e domain_interfaces.LogEvent) {
	c.Events = append(c.Events, e)
}

func (c *CapturingObserver) RegisterCodec(Codec) Codec { return nil }

// TestCapturingObserver_NoFiles asserts the design-doc claim that a
// custom Observer can capture events without producing any files on
// disk and without going through codecs at all.
func TestCapturingObserver_NoFiles(t *testing.T) {
	cap := &CapturingObserver{}

	wrapped := AsBlobWriteObserver(cap)
	wrapped.OnBlobPublished(domain_interfaces.BlobWriteEvent{
		StoreId: "x",
		Size:    1,
		Op:      domain_interfaces.BlobWriteOpExists,
	})

	if len(cap.Events) != 1 {
		t.Fatalf("expected 1 captured event, got %d", len(cap.Events))
	}

	got := cap.Events[0].(domain_interfaces.BlobWriteEvent)
	if got.StoreId != "x" || got.Size != 1 ||
		got.Op != domain_interfaces.BlobWriteOpExists {
		t.Errorf("captured event mismatch: %+v", got)
	}
}

// assertHyphenceHeader checks the file starts with a hyphence document
// whose metadata is exactly `! madder-inventory_log-ndjson-v1`. Returns
// the body bytes (with the leading `\n` separator stripped).
func assertHyphenceHeader(t *testing.T, content []byte) []byte {
	t.Helper()

	const boundary = "---\n"
	if !bytes.HasPrefix(content, []byte(boundary)) {
		t.Fatalf("expected leading %q, got %q", boundary, content[:min(len(content), 16)])
	}

	parts := bytes.SplitN(content, []byte(boundary), 3)
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts split on %q, got %d", boundary, len(parts))
	}

	metadata := parts[1]
	wantMeta := "! " + bodyTypeNDJSON + "\n"
	if string(metadata) != wantMeta {
		t.Errorf("metadata block: got %q, want %q", metadata, wantMeta)
	}

	body := parts[2]
	body = bytes.TrimPrefix(body, []byte("\n"))
	return body
}

// splitNDJSON splits body bytes on newlines, dropping the trailing
// empty element that bytes.Split produces.
func splitNDJSON(body []byte) [][]byte {
	body = bytes.TrimRight(body, "\n")
	if len(body) == 0 {
		return nil
	}
	return bytes.Split(body, []byte("\n"))
}
