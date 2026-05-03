package commands_cutting_garden

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/amarbel-llc/madder/go/internal/charlie/capture_sink"
	"github.com/amarbel-llc/madder/go/internal/echo/env_dir"
)

// captureLogEntry is one NDJSON line in $XDG_STATE_HOME/<scope>/captures.log.
// One entry per receipt produced — a single capture invocation that touches
// N store-groups produces N entries.
type captureLogEntry struct {
	// Ts is the RFC3339 UTC timestamp at which the receipt was written.
	Ts string `json:"ts"`
	// ReceiptID is the markl-id of the receipt blob, as produced by
	// writeReceiptBlob in capture.go.
	ReceiptID string `json:"receipt_id"`
	// StoreID is the blob-store-id string the receipt landed in. Empty
	// string for the default store, matching blob_store_id.Id.IsEmpty()
	// conventions in the rest of capture.go and the user-facing NDJSON
	// sink (where receipt_store_of_group also yields an empty string).
	StoreID string `json:"store_id"`
	// Roots is the directory args for this store-group's receipt, in the
	// order they were captured.
	Roots []string `json:"roots"`
}

// captureLogFileName is the leaf filename under
// <cgEnvDir.GetXDG().State>/<scope>/captures.log. cg's audit trail
// of past captures.
const captureLogFileName = "captures.log"

// appendCaptureLog appends entries as NDJSON lines to the captures.log
// file under cgEnvDir's $XDG_STATE_HOME/<scope>/. Best-effort: errors
// surface as sink notices, never fatal. The blob is the source of
// truth; the log is observability.
//
// Mirrors inventory_log's swallow-on-error policy (xdg_log_home(7)
// constraints) — if a user's $XDG_STATE_HOME is unwritable, the
// capture itself still succeeds.
//
// No daily rotation, no hyphence wrapping, no codec registry. This is
// a focused multi-scope tracer; richer infrastructure would be a
// generalization of inventory_log if a real consumer ever wants it.
func appendCaptureLog(
	cgEnvDir env_dir.Env,
	sink capture_sink.Sink,
	entries []captureLogEntry,
) {
	if len(entries) == 0 {
		return
	}

	path := cgEnvDir.GetXDG().State.MakePath(captureLogFileName).String()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		sink.Notice(fmt.Sprintf(
			"notice: cannot create captures.log directory %q: %v",
			filepath.Dir(path), err,
		))
		return
	}

	file, err := os.OpenFile(
		path,
		os.O_WRONLY|os.O_APPEND|os.O_CREATE,
		0o644,
	)
	if err != nil {
		sink.Notice(fmt.Sprintf(
			"notice: cannot open captures.log %q: %v", path, err,
		))
		return
	}
	defer file.Close() //nolint:errcheck // best-effort log; close error is non-fatal

	encoder := json.NewEncoder(file)
	for _, entry := range entries {
		if err := encoder.Encode(entry); err != nil {
			sink.Notice(fmt.Sprintf(
				"notice: captures.log write error at %q: %v", path, err,
			))
			return
		}
	}
}

// rootPaths extracts the .path field from each captureRoot in order.
// captureRoot also carries a shadowNotice; the log records only the
// path itself.
func rootPaths(roots []captureRoot) []string {
	out := make([]string, len(roots))
	for i, r := range roots {
		out[i] = r.path
	}
	return out
}

// captureLogTimestamp returns the current RFC3339 UTC timestamp.
// Indirected through a function so tests (or a future --fixed-clock
// debug knob) can stub it; today it is just time.Now().
var captureLogTimestamp = func() string {
	return time.Now().UTC().Format(time.RFC3339)
}
