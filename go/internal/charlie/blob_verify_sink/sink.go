// Package blob_verify_sink carries the per-blob result stream shared by
// `madder fsck` and `madder cache-fsck`.
//
// Events:
//   - Verified(id, store)  — blob read and hash recomputed successfully
//   - Missing(id, store)   — blob is listed but not present
//   - Corrupt(id, store, err) — blob present but hash mismatch or read error
//   - ReadError(store, err)   — couldn't read the blob listing entry (no id)
//   - Notice(msg)             — informational (store header, progress ticks,
//     per-store summary); stderr in JSON mode
//   - Finalize()              — flush (TAP plan; noop for JSON)
package blob_verify_sink

import (
	"encoding/json"
	"fmt"
	"io"

	tap "github.com/amarbel-llc/tap/go"
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/charlie/tap_diagnostics"
)

type Sink interface {
	Verified(id domain_interfaces.MarklId, store string)
	Missing(id domain_interfaces.MarklId, store string)
	Corrupt(id domain_interfaces.MarklId, store string, err error)
	ReadError(store string, err error)
	Notice(msg string)
	BailOut(msg string)
	Finalize()
}

func NewTAP(w io.Writer) Sink {
	return &tapSink{tw: tap.NewWriter(w)}
}

// NewJSON constructs an NDJSON sink. The encoder writes directly to
// `out` — no outer buffering — so each Encode call results in a single
// `Write` syscall. On a pipe (or merged stdout/stderr fd from `2>&1`)
// that single write is atomic up to PIPE_BUF, which is enough for the
// record sizes we emit and prevents the byte-level interleaving we'd
// otherwise see when stderr Notices land mid-record. See #154.
func NewJSON(out, errOut io.Writer) Sink {
	return &jsonSink{
		enc:    json.NewEncoder(out),
		errOut: errOut,
	}
}

type tapSink struct {
	tw *tap.Writer
}

func (s *tapSink) Verified(id domain_interfaces.MarklId, _ string) {
	s.tw.Ok(id.String())
}

func (s *tapSink) Missing(id domain_interfaces.MarklId, _ string) {
	s.tw.NotOk(id.String(), map[string]string{
		"severity": "fail",
		"message":  "blob missing",
	})
}

func (s *tapSink) Corrupt(id domain_interfaces.MarklId, _ string, err error) {
	s.tw.NotOk(id.String(), tap_diagnostics.FromError(err))
}

func (s *tapSink) ReadError(_ string, err error) {
	s.tw.NotOk("(unknown blob)", tap_diagnostics.FromError(err))
}

func (s *tapSink) Notice(msg string) {
	s.tw.Comment(msg)
}

func (s *tapSink) BailOut(msg string) {
	s.tw.BailOut(msg)
}

func (s *tapSink) Finalize() {
	s.tw.Plan()
}

// record is the wire shape of a single NDJSON verify record.
type record struct {
	Id    string `json:"id,omitempty"`
	Store string `json:"store,omitempty"`
	State string `json:"state,omitempty"`
	Error string `json:"error,omitempty"`
}

// State values emitted on NDJSON records. These are part of the CLI
// wire contract — external consumers switch on them.
const (
	StateVerified  = "verified"
	StateMissing   = "missing"
	StateCorrupt   = "corrupt"
	StateReadError = "read_error"
	StateBailOut   = "bail_out"
)

type jsonSink struct {
	enc    *json.Encoder
	errOut io.Writer
}

func (s *jsonSink) emit(rec record) {
	_ = s.enc.Encode(rec)
}

func (s *jsonSink) Verified(id domain_interfaces.MarklId, store string) {
	s.emit(record{Id: id.String(), Store: store, State: StateVerified})
}

func (s *jsonSink) Missing(id domain_interfaces.MarklId, store string) {
	s.emit(record{Id: id.String(), Store: store, State: StateMissing})
}

func (s *jsonSink) Corrupt(id domain_interfaces.MarklId, store string, err error) {
	s.emit(record{Id: id.String(), Store: store, State: StateCorrupt, Error: err.Error()})
}

func (s *jsonSink) ReadError(store string, err error) {
	s.emit(record{Store: store, State: StateReadError, Error: err.Error()})
}

// Notice writes a TAP-comment-formatted line ("# <msg>") to errOut so
// the merged stdout+stderr stream stays parseable as TAP — the JSON
// event records are valid TAP "ok"/"not ok" lines from a strict-TAP
// consumer's standpoint, and the leading "# " on notices keeps them
// from being misclassified. The single Fprintln is one Write syscall,
// atomic on a pipe up to PIPE_BUF.
func (s *jsonSink) Notice(msg string) {
	fmt.Fprintln(s.errOut, "# "+msg)
}

// Finalize is a no-op for the JSON sink. The encoder writes
// unbuffered directly to `out`; nothing to flush.
func (s *jsonSink) Finalize() {}

func (s *jsonSink) BailOut(msg string) {
	// Bail-out becomes a final error record so consumers can detect the
	// stream was cut short by a caller-side abort rather than by EOF.
	s.emit(record{State: StateBailOut, Error: msg})
}
