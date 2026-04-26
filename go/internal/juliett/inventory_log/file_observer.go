package inventory_log

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/0/ids"
	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// bodyTypeNDJSON is the on-disk hyphence `! type-string` for inventory-log
// session files written by FileObserver. The body following the metadata
// block is one JSON object per line, each carrying a top-level `"type"`
// field that selects the per-event codec.
const bodyTypeNDJSON = "madder-inventory_log-ndjson-v1"

// FileObserver writes a hyphence-wrapped NDJSON stream per session to
// $XDG_LOG_HOME/madder/inventory_log/YYYY-MM-DD/<tai>-<hex4>.hyphence.
// Lazy-opens its file on first Emit so a session that publishes zero
// events produces no file.
//
// Errors are swallowed per xdg_log_home(7).
type FileObserver struct {
	rootDir string

	now     func() ids.Tai
	randHex func() string

	mu          sync.Mutex
	description string
	codecs      map[string]Codec
	started     bool
	file        *os.File
	body        *pipedWriterTo
	writerDone  chan error
}

// DescriptionSetter is the narrow capability interface callers use to
// attach per-invocation intent to every event the observer produces.
// FileObserver implements it; NopObserver does not — type assertions at
// the call site naturally no-op when logging is disabled.
type DescriptionSetter interface {
	SetDescription(s string)
}

var (
	_ Observer                            = (*FileObserver)(nil)
	_ domain_interfaces.BlobWriteObserver = (*FileObserver)(nil)
	_ DescriptionSetter                   = (*FileObserver)(nil)
)

// NewFileObserver returns a FileObserver rooted at rootDir (typically
// MadderInventoryLogDir()). The file is opened on first Emit, not now,
// so a no-op session produces no file.
func NewFileObserver(rootDir string) *FileObserver {
	return &FileObserver{
		rootDir: rootDir,
		now:     ids.NowTai,
		randHex: defaultRandHex,
	}
}

// SetDescription records a caller-supplied string to stamp into every
// subsequent BlobWriteEvent's Description field. Safe before any
// Emit fires; called mid-stream the new value takes effect for
// subsequent events only.
func (o *FileObserver) SetDescription(s string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.description = s
}

// RegisterCodec installs a per-Observer codec for a non-reserved type.
// Reserved types panic. Returns the codec it displaced (nil if none).
func (o *FileObserver) RegisterCodec(c Codec) Codec {
	typeStr := c.Type()
	if _, reserved := reservedTypes[typeStr]; reserved {
		panic(errors.ErrorWithStackf(
			"codec type %q is reserved by inventory_log; per-Observer override forbidden",
			typeStr,
		).Error())
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	if o.codecs == nil {
		o.codecs = make(map[string]Codec)
	}

	prev := o.codecs[typeStr]
	o.codecs[typeStr] = c
	return prev
}

// OnBlobPublished implements domain_interfaces.BlobWriteObserver. It
// adapts the existing store-side call site to the new Observer.Emit
// path so stores don't need to change.
func (o *FileObserver) OnBlobPublished(ev domain_interfaces.BlobWriteEvent) {
	o.mu.Lock()
	if o.description != "" {
		ev.Description = o.description
	}
	o.mu.Unlock()

	o.Emit(ev)
}

// Emit dispatches event through the per-Observer codec table, falling
// back to Global. No codec found → best-effort drop, per ADR 0004.
func (o *FileObserver) Emit(event domain_interfaces.LogEvent) {
	codec, ok := o.lookupCodec(event.LogType())
	if !ok {
		return
	}

	line, err := codec.Encode(event)
	if err != nil {
		return
	}

	if err := o.ensureStarted(); err != nil {
		return
	}

	line = append(line, '\n')
	_, _ = o.body.Write(line)
}

func (o *FileObserver) lookupCodec(typeStr string) (Codec, bool) {
	o.mu.Lock()
	if c, ok := o.codecs[typeStr]; ok {
		o.mu.Unlock()
		return c, true
	}
	o.mu.Unlock()

	return Global.Lookup(typeStr)
}

// ensureStarted opens the session file and starts the hyphence writer
// goroutine. Idempotent; subsequent calls are cheap.
func (o *FileObserver) ensureStarted() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.started {
		return nil
	}

	sessionTai := o.now()
	dayDir := filepath.Join(
		o.rootDir,
		sessionTai.AsTime().UTC().Format("2006-01-02"),
	)
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		return err
	}

	sessionId := sessionTai.String() + "-" + o.randHex()
	path := filepath.Join(dayDir, sessionId+".hyphence")

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}

	body := newPipedWriterTo()
	done := make(chan error, 1)

	go func() {
		_, werr := hyphence.Writer{
			Metadata: ndjsonHeaderWriter{},
			Blob:     body,
		}.WriteTo(f)
		done <- werr
	}()

	o.file = f
	o.body = body
	o.writerDone = done
	o.started = true

	return nil
}

// Close finishes the session: closes the body pipe, waits for the
// hyphence writer goroutine, and closes the file. If no Emit was ever
// called (started == false), Close is a no-op.
//
// Wired into the CLI lifecycle via errors.ContextCloseAfter at observer
// construction (see command_components/env_blob_store.go); fires when
// errCtx.Run() inside futility's wrapped.Run completes. Tests that
// construct a FileObserver directly must call Close explicitly.
func (o *FileObserver) Close() error {
	o.mu.Lock()
	if !o.started {
		o.mu.Unlock()
		return nil
	}
	body := o.body
	file := o.file
	done := o.writerDone
	o.mu.Unlock()

	_ = body.Close()
	werr := <-done
	cerr := file.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// ndjsonHeaderWriter writes the body of the hyphence metadata block:
// a single `! type-string` line. hyphence.Writer wraps this with the
// `---` boundary lines; we just need to emit the `!` line itself.
type ndjsonHeaderWriter struct{}

var _ io.WriterTo = ndjsonHeaderWriter{}

func (ndjsonHeaderWriter) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write([]byte("! " + bodyTypeNDJSON + "\n"))
	return int64(n), err
}

func defaultRandHex() string {
	var buf [2]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Best-effort: zeros are still unique enough across days,
		// per xdg_log_home(7)'s best-effort guarantee.
		return "0000"
	}
	return hex.EncodeToString(buf[:])
}
