package write_log

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
)

// TestFileObserver_WritesOneRecord pins the golden-path shape: a single
// OnBlobPublished call emits exactly one NDJSON record with every
// contracted field populated. MarklId is left nil here to keep the
// test free of bravo/markl construction boilerplate; markl
// serialization is covered by bravo/markl's own unit tests, and
// recordFromEvent explicitly handles the nil case.
func TestFileObserver_WritesOneRecord(t *testing.T) {
	dir := t.TempDir()
	o := NewFileObserver(dir)
	o.now = func() time.Time {
		return time.Date(2026, 4, 24, 12, 34, 56, 0, time.UTC)
	}

	o.OnBlobPublished(domain_interfaces.BlobWriteEvent{
		StoreId: "default",
		Size:    1234,
		Op:      domain_interfaces.BlobWriteOpWritten,
	})

	path := filepath.Join(dir, "blob-writes-2026-04-24.ndjson")
	bites, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	lines := strings.Split(strings.TrimRight(string(bites), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 NDJSON line, got %d: %q", len(lines), lines)
	}

	var rec Record
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("parse record: %v\nline: %s", err, lines[0])
	}

	if rec.Utility != "madder" {
		t.Errorf("Utility = %q, want %q", rec.Utility, "madder")
	}
	if rec.StoreId != "default" {
		t.Errorf("StoreId = %q, want %q", rec.StoreId, "default")
	}
	if rec.MarklId != "" {
		t.Errorf("MarklId = %q, want empty (nil MarklId)", rec.MarklId)
	}
	if rec.Size != 1234 {
		t.Errorf("Size = %d, want 1234", rec.Size)
	}
	if rec.Op != "written" {
		t.Errorf("Op = %q, want %q", rec.Op, "written")
	}
	if rec.Pid == 0 {
		t.Error("Pid is 0; expected the running process's PID")
	}
	if rec.Ts != "2026-04-24T12:34:56Z" {
		t.Errorf("Ts = %q, want 2026-04-24T12:34:56Z", rec.Ts)
	}
}

// TestFileObserver_DayRollover writes two records across a UTC day
// boundary and asserts each lands in its own dated file.
func TestFileObserver_DayRollover(t *testing.T) {
	dir := t.TempDir()
	o := NewFileObserver(dir)

	day1 := time.Date(2026, 4, 24, 23, 59, 59, 0, time.UTC)
	day2 := time.Date(2026, 4, 25, 0, 0, 1, 0, time.UTC)
	calls := []time.Time{day1, day2}
	i := 0
	o.now = func() time.Time {
		t := calls[i]
		i++
		return t
	}

	ev := domain_interfaces.BlobWriteEvent{
		StoreId: "default",
		Size:    10,
		Op:      domain_interfaces.BlobWriteOpWritten,
	}

	o.OnBlobPublished(ev)
	o.OnBlobPublished(ev)

	for _, expected := range []string{
		"blob-writes-2026-04-24.ndjson",
		"blob-writes-2026-04-25.ndjson",
	} {
		bites, err := os.ReadFile(filepath.Join(dir, expected))
		if err != nil {
			t.Fatalf("read %s: %v", expected, err)
		}
		if strings.Count(string(bites), "\n") != 1 {
			t.Errorf("%s: expected exactly 1 NDJSON line", expected)
		}
	}
}

// TestFileObserver_ConcurrentWritesNoInterleaving: N goroutines each
// emit one event; the log file must contain N complete NDJSON lines
// with no partial/corrupt records.
func TestFileObserver_ConcurrentWritesNoInterleaving(t *testing.T) {
	dir := t.TempDir()
	o := NewFileObserver(dir)
	o.now = func() time.Time {
		return time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	}

	const n = 64
	done := make(chan struct{}, n)
	ev := domain_interfaces.BlobWriteEvent{
		StoreId: "default",
		Size:    1,
		Op:      domain_interfaces.BlobWriteOpWritten,
	}
	for i := 0; i < n; i++ {
		go func() {
			o.OnBlobPublished(ev)
			done <- struct{}{}
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}

	bites, err := os.ReadFile(filepath.Join(dir, "blob-writes-2026-04-24.ndjson"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(bites), "\n"), "\n")
	if len(lines) != n {
		t.Fatalf("expected %d lines, got %d", n, len(lines))
	}
	for i, line := range lines {
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Errorf("line %d not valid NDJSON: %v\n%q", i, err, line)
		}
	}
}
