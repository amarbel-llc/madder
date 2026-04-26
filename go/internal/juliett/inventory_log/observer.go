package inventory_log

import (
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
)

// Observer is the pluggable inventory-log sink. FileObserver and
// NopObserver implement it; importer-defined observers (test-capture
// shims, multi-sink fan-outs, etc.) implement it directly.
type Observer interface {
	// Emit dispatches event through the Observer's effective codec set:
	// per-Observer overrides (for non-reserved types) first, then Global.
	// Reserved types always dispatch through their native codec.
	// No codec found for a non-reserved type → best-effort drop.
	Emit(event domain_interfaces.LogEvent)

	// RegisterCodec installs a per-Observer codec for a non-reserved
	// type. Reserved types panic. Returns the codec it displaced (nil
	// if none) so importers can wrap.
	RegisterCodec(c Codec) (previous Codec)
}
