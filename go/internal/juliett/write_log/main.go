// Package write_log implements the per-blob audit log described in
// ADR 0004. A file-backed BlobWriteObserver emits one NDJSON record per
// blob publish to $XDG_LOG_HOME/madder/blob-writes-YYYY-MM-DD.ndjson, with
// daily rotation and O_APPEND atomicity for concurrent madder processes.
//
// The no-op observer is used when the write-log is disabled via
// --no-write-log or MADDER_WRITE_LOG=0.
package write_log

import (
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
)

// NopObserver is a BlobWriteObserver that does nothing. Used when the
// write-log is disabled so call sites can avoid nil checks.
type NopObserver struct{}

var _ domain_interfaces.BlobWriteObserver = NopObserver{}

func (NopObserver) OnBlobPublished(domain_interfaces.BlobWriteEvent) {}
