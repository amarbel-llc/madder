// Package blob_write_sink carries the per-blob result stream shared by
// `madder write` and `madder cache-write`. Each call the caller makes
// (success, check miss, failure, skip, notice) emits one TAP line or one
// NDJSON record depending on the concrete sink.
//
// Sync/fsck/pack-blobs produce different event shapes and keep their own
// per-command sinks; only blob-write-style commands reuse this one.
package blob_write_sink

import (
	"encoding/json"
	"fmt"
	"io"

	tap "github.com/amarbel-llc/bob/packages/tap-dancer/go"
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/charlie/tap_diagnostics"
)

// Sink abstracts write's output stream so Run can stay format-agnostic.
type Sink interface {
	// Success reports a blob that is freshly written or (in -check) already
	// present. In -check, the JSON sink emits present:true.
	Success(id domain_interfaces.MarklId, size int64, source, store string)
	// CheckMiss reports a -check miss: digest computed but blob absent. Counts
	// as a failure at the caller level.
	CheckMiss(id domain_interfaces.MarklId, size int64, source, store string)
	// Failure reports a per-arg error that prevented digest computation
	// (resolve error, read error, etc.).
	Failure(source string, err error)
	// Skip reports a skipped arg (e.g. null digest).
	Skip(source, reason string)
	// Notice reports informational events (store switches, shadow warnings).
	// TAP renders as a comment; JSON routes to stderr.
	Notice(msg string)
	// Finalize is called once after all args have been processed.
	Finalize()
}

// NewTAP constructs a TAP sink writing to w. Caller must invoke Finalize to
// emit the plan.
func NewTAP(w io.Writer) Sink {
	return &tapSink{tw: tap.NewWriter(w)}
}

// NewTAPWithWriter constructs a TAP sink using a caller-owned tap.Writer.
// Finalize emits the plan on that writer. Use this when the command also
// needs to write phase-level test points (e.g. pack-blobs passing its own
// TapWriter to an internal packer) so all test points share one stream.
func NewTAPWithWriter(tw *tap.Writer) Sink {
	return &tapSink{tw: tw}
}

// NewJSON constructs an NDJSON sink. out is the record stream; errOut
// receives Notice messages. isCheck controls whether Success records carry
// present:true.
func NewJSON(out, errOut io.Writer, isCheck bool) Sink {
	return &jsonSink{
		enc:     json.NewEncoder(out),
		errOut:  errOut,
		isCheck: isCheck,
	}
}

type tapSink struct {
	tw *tap.Writer
}

func (s *tapSink) Success(id domain_interfaces.MarklId, _ int64, source, _ string) {
	s.tw.Ok(fmt.Sprintf("%s %s", id, source))
}

func (s *tapSink) CheckMiss(id domain_interfaces.MarklId, _ int64, source, _ string) {
	s.tw.NotOk(
		fmt.Sprintf("%s %s", id, source),
		map[string]string{
			"severity": "fail",
			"message":  "untracked",
		},
	)
}

func (s *tapSink) Failure(source string, err error) {
	s.tw.NotOk(source, tap_diagnostics.FromError(err))
}

func (s *tapSink) Skip(source, reason string) {
	s.tw.Skip(source, reason)
}

func (s *tapSink) Notice(msg string) {
	s.tw.Comment(msg)
}

func (s *tapSink) Finalize() {
	s.tw.Plan()
}

// record is the wire shape of a single NDJSON write record. Fields are
// omitempty so each event type emits only the fields that apply.
type record struct {
	Id      string `json:"id,omitempty"`
	Size    *int64 `json:"size,omitempty"`
	Source  string `json:"source,omitempty"`
	Store   string `json:"store,omitempty"`
	Present *bool  `json:"present,omitempty"`
	Error   string `json:"error,omitempty"`
	Skipped string `json:"skipped,omitempty"`
}

type jsonSink struct {
	enc     *json.Encoder
	errOut  io.Writer
	isCheck bool
}

func (s *jsonSink) emit(rec record) {
	// json.Encoder already appends a newline, giving NDJSON framing.
	_ = s.enc.Encode(rec)
}

func (s *jsonSink) Success(id domain_interfaces.MarklId, size int64, source, store string) {
	rec := record{
		Id:     id.String(),
		Size:   &size,
		Source: source,
		Store:  store,
	}

	if s.isCheck {
		present := true
		rec.Present = &present
	}

	s.emit(rec)
}

func (s *jsonSink) CheckMiss(id domain_interfaces.MarklId, size int64, source, store string) {
	present := false
	s.emit(record{
		Id:      id.String(),
		Size:    &size,
		Source:  source,
		Store:   store,
		Present: &present,
	})
}

func (s *jsonSink) Failure(source string, err error) {
	s.emit(record{
		Source: source,
		Error:  err.Error(),
	})
}

func (s *jsonSink) Skip(source, reason string) {
	s.emit(record{
		Source:  source,
		Skipped: reason,
	})
}

func (s *jsonSink) Notice(msg string) {
	fmt.Fprintln(s.errOut, msg)
}

func (s *jsonSink) Finalize() {}
