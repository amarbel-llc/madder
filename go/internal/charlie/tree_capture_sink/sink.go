// Package tree_capture_sink carries the per-entry result stream for
// `madder capture-tree`. Each filesystem entry becomes one TAP test
// point or one NDJSON record; each store group ends with a summary
// (TAP `ok` test point or NDJSON `store_group_receipt` record). Notices
// (store switches, shadow warnings) and per-arg failures use the
// dedicated methods.
//
// blob_write_sink covers the `madder write` event shape; capture-tree's
// per-entry events carry filesystem metadata that doesn't fit there, so
// it gets its own sink.
package tree_capture_sink

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	tap "github.com/amarbel-llc/bob/packages/tap-dancer/go"
	"github.com/amarbel-llc/madder/go/internal/charlie/tap_diagnostics"
	"github.com/amarbel-llc/madder/go/internal/charlie/tree_capture_receipt"
)

// Sink streams capture-tree results in either TAP or NDJSON form. Each
// concrete sink is single-threaded; capture-tree's walk is sequential.
type Sink interface {
	// Entry reports one filesystem entry that was successfully captured.
	// `store` is the active store-id at capture time; empty when the
	// default store is in use.
	Entry(store string, e tree_capture_receipt.Entry)

	// StoreGroupReceipt reports the receipt blob produced for one store
	// group, after every entry under that group has been emitted.
	StoreGroupReceipt(store, receiptID string, count int)

	// Notice reports informational events (store switches, shadow
	// warnings). TAP renders as a comment; NDJSON routes to stderr.
	Notice(msg string)

	// Failure reports a per-source error (capture-root missing, file
	// open failure, copy failure, etc.).
	Failure(source string, err error)

	// Finalize is called once after the last store group has been
	// emitted. TAP emits the plan; NDJSON flushes the buffered writer.
	Finalize()
}

// NewTAP constructs a TAP sink writing to w. Caller must invoke
// Finalize to emit the plan.
func NewTAP(w io.Writer) Sink {
	return &tapSink{tw: tap.NewWriter(w)}
}

// NewNDJSON constructs an NDJSON sink. out is the record stream;
// errOut receives Notice messages. The record stream is buffered;
// Finalize flushes.
func NewNDJSON(out, errOut io.Writer) Sink {
	buf := bufio.NewWriter(out)
	return &jsonSink{
		buf:    buf,
		enc:    json.NewEncoder(buf),
		errOut: errOut,
	}
}

type tapSink struct {
	tw *tap.Writer
}

func (s *tapSink) Entry(_ string, e tree_capture_receipt.Entry) {
	s.tw.Ok(formatTAPEntry(e))
}

func (s *tapSink) StoreGroupReceipt(store, receiptID string, count int) {
	s.tw.Ok(fmt.Sprintf(
		"receipt store=%s id=%s count=%d",
		quoteIfEmpty(store), receiptID, count,
	))
}

func (s *tapSink) Notice(msg string) {
	s.tw.Comment(msg)
}

func (s *tapSink) Failure(source string, err error) {
	s.tw.NotOk(source, tap_diagnostics.FromError(err))
}

func (s *tapSink) Finalize() {
	s.tw.Plan()
}

func formatTAPEntry(e tree_capture_receipt.Entry) string {
	rel := joinRootPath(e.Root, e.Path)
	mode := fmt.Sprintf("%04o", e.Mode.Perm())

	switch e.Type {
	case tree_capture_receipt.TypeFile:
		return fmt.Sprintf("%s file mode=%s size=%d blob=%s", rel, mode, e.Size, e.BlobId)
	case tree_capture_receipt.TypeDir:
		return fmt.Sprintf("%s dir mode=%s", rel, mode)
	case tree_capture_receipt.TypeSymlink:
		return fmt.Sprintf("%s symlink mode=%s target=%s", rel, mode, e.Target)
	default:
		return fmt.Sprintf("%s %s mode=%s", rel, e.Type, mode)
	}
}

// joinRootPath formats Root+Path for human-readable output. The
// receipt itself stores them separately for parser clarity, but a
// single combined string is friendlier in TAP test point messages.
func joinRootPath(root, path string) string {
	if path == "." || path == "" {
		return root
	}
	return root + "/" + path
}

func quoteIfEmpty(s string) string {
	if s == "" {
		return `""`
	}
	return s
}

type jsonSink struct {
	buf    *bufio.Writer
	enc    *json.Encoder
	errOut io.Writer
}

// entryRecord is the wire shape of one captured entry on the NDJSON
// stream. Mirrors tree_capture_receipt's recordV1 (so consumers can
// share a parser) and adds `store`. Type carries the filesystem entry
// kind for entries; the `store_group_receipt` summary uses
// summaryRecord, which sets Type to "store_group_receipt".
type entryRecord struct {
	Path   string `json:"path,omitempty"`
	Root   string `json:"root,omitempty"`
	Type   string `json:"type,omitempty"`
	Mode   string `json:"mode,omitempty"`
	Size   int64  `json:"size,omitempty"`
	BlobId string `json:"blob_id,omitempty"`
	Target string `json:"target,omitempty"`
	Store  string `json:"store,omitempty"`
	Source string `json:"source,omitempty"`
	Error  string `json:"error,omitempty"`
}

type summaryRecord struct {
	Type      string `json:"type"`
	Store     string `json:"store"`
	ReceiptID string `json:"receipt_id"`
	Count     int    `json:"count"`
}

func (s *jsonSink) Entry(store string, e tree_capture_receipt.Entry) {
	rec := entryRecord{
		Path:  e.Path,
		Root:  e.Root,
		Type:  e.Type,
		Mode:  fmt.Sprintf("%04o", e.Mode.Perm()),
		Store: store,
	}

	switch e.Type {
	case tree_capture_receipt.TypeFile:
		rec.Size = e.Size
		rec.BlobId = e.BlobId
	case tree_capture_receipt.TypeSymlink:
		rec.Target = e.Target
	}

	_ = s.enc.Encode(rec)
}

func (s *jsonSink) StoreGroupReceipt(store, receiptID string, count int) {
	_ = s.enc.Encode(summaryRecord{
		Type:      "store_group_receipt",
		Store:     store,
		ReceiptID: receiptID,
		Count:     count,
	})
}

func (s *jsonSink) Notice(msg string) {
	fmt.Fprintln(s.errOut, msg)
}

func (s *jsonSink) Failure(source string, err error) {
	_ = s.enc.Encode(entryRecord{
		Source: source,
		Error:  err.Error(),
	})
}

func (s *jsonSink) Finalize() {
	_ = s.buf.Flush()
}
