// Package inventory_log implements the per-blob audit log described in
// ADR 0004 and refined in docs/plans/2026-04-26-typed-write-log-design.md.
// A file-backed Observer writes a hyphence-wrapped NDJSON stream per
// session to $XDG_LOG_HOME/madder/inventory_log/YYYY-MM-DD/<id>.hyphence.
//
// The no-op observer is used when the inventory-log is disabled via
// --no-inventory-log or MADDER_INVENTORY_LOG=0.
package inventory_log

//go:generate dagnabit export

import (
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
)

// NopObserver is a no-op Observer / BlobWriteObserver. Used when the
// inventory-log is disabled so call sites can avoid nil checks.
type NopObserver struct{}

var (
	_ domain_interfaces.BlobWriteObserver = NopObserver{}
	_ Observer                            = NopObserver{}
)

func (NopObserver) OnBlobPublished(domain_interfaces.BlobWriteEvent) {}

func (NopObserver) Emit(domain_interfaces.LogEvent) {}

func (NopObserver) RegisterCodec(Codec) Codec { return nil }
