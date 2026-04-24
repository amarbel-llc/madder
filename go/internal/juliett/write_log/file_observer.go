package write_log

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
)

// FileObserver writes one NDJSON record per OnBlobPublished call to
// $XDG_LOG_HOME/madder/blob-writes-YYYY-MM-DD.ndjson, lazily opening the
// day's file on first write and reopening when the calendar day rolls
// over. O_APPEND writes shorter than PIPE_BUF (4096 B on Linux) are
// atomic, so concurrent madder processes sharing the same log file do
// not interleave partial records.
//
// Errors are swallowed per xdg_log_home(7): deletion of log files MUST
// NOT affect application correctness, and by extension neither must
// failure to write them. Surfacing open/write failures to the user is
// a follow-up consideration (probably via debug.Options).
type FileObserver struct {
	dir string

	// now is injectable for tests that drive day rollover.
	now func() time.Time

	mu          sync.Mutex
	currentDate string
	currentFile *os.File

	// description is stamped into every emitted event's Description
	// field when non-empty, overriding whatever the event carried.
	// Set by the caller (e.g. Write.Run) via SetDescription. See
	// ADR 0004 and issue #51.
	description string
}

// DescriptionSetter is the narrow capability interface callers use to
// attach per-invocation intent to every record the observer produces.
// FileObserver implements it; NopObserver does not — type assertions
// at the call site naturally no-op when logging is disabled.
type DescriptionSetter interface {
	SetDescription(s string)
}

var (
	_ domain_interfaces.BlobWriteObserver = (*FileObserver)(nil)
	_ DescriptionSetter                   = (*FileObserver)(nil)
)

// SetDescription records a caller-supplied string to stamp into every
// subsequent event's Description field. Safe to call before any
// OnBlobPublished has fired; called mid-stream, the new value takes
// effect for subsequent events only (already-written records are
// immutable by construction).
func (o *FileObserver) SetDescription(s string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.description = s
}

// NewFileObserver returns a FileObserver rooted at dir. The directory is
// created on first write (not now) so that the constructor has no side
// effects — a no-op write produces no file.
func NewFileObserver(dir string) *FileObserver {
	return &FileObserver{
		dir: dir,
		now: time.Now,
	}
}

// OnBlobPublished serializes the event as NDJSON and appends it to the
// day's log file. Failures never propagate.
func (o *FileObserver) OnBlobPublished(ev domain_interfaces.BlobWriteEvent) {
	now := o.now()

	o.mu.Lock()
	if o.description != "" {
		ev.Description = o.description
	}
	o.mu.Unlock()

	rec := recordFromEvent(
		ev,
		now.UTC().Format(time.RFC3339Nano),
		os.Getpid(),
	)

	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	line = append(line, '\n')

	o.mu.Lock()
	defer o.mu.Unlock()

	date := now.UTC().Format("2006-01-02")
	if date != o.currentDate || o.currentFile == nil {
		if o.currentFile != nil {
			_ = o.currentFile.Close()
			o.currentFile = nil
		}
		if err := os.MkdirAll(o.dir, 0o755); err != nil {
			return
		}
		path := filepath.Join(o.dir, "blob-writes-"+date+".ndjson")
		f, err := os.OpenFile(
			path,
			os.O_WRONLY|os.O_APPEND|os.O_CREATE,
			0o644,
		)
		if err != nil {
			return
		}
		o.currentFile = f
		o.currentDate = date
	}

	_, _ = o.currentFile.Write(line)
}
