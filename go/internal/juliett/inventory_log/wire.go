package inventory_log

import (
	"os"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// envVarDisable is the exact env var name that suppresses the log when
// set to "0". Mirrors command_components.makeBlobWriteObserver so the
// CLI and library entry points agree on the disable contract.
const envVarDisable = "MADDER_INVENTORY_LOG"

// Compile-time assertion that every concrete value WireDefault and
// WireWithCleanup return also satisfies domain_interfaces.BlobWriteObserver.
// command_components.makeBlobWriteObserver type-asserts the returned
// Observer to BlobWriteObserver; that assertion stays safe as long as
// every concrete return path here implements both interfaces.
var (
	_ domain_interfaces.BlobWriteObserver = NopObserver{}
	_ domain_interfaces.BlobWriteObserver = (*FileObserver)(nil)
)

// WireDefault constructs a FileObserver rooted at MadderInventoryLogDir
// and registers its Close on ctx via errors.ContextCloseAfter, so the
// trailing hyphence buffer is flushed when ctx.Run completes. Returns
// the observer (it satisfies BlobWriteObserver via AsBlobWriteObserver,
// or directly via type assertion since *FileObserver implements both).
//
// Honors MADDER_INVENTORY_LOG=0 (exactly "0", trim-space tolerant) by
// returning a NopObserver — same disable contract the CLI's --no-
// inventory-log flag uses, so importers can opt out the same way.
//
// Use this when you have an errors.Context (the common case for code
// running inside a futility command). For callers without a context,
// use WireWithCleanup.
func WireDefault(ctx errors.Context) Observer {
	if isDisabledByEnv() {
		return NopObserver{}
	}

	obs := NewFileObserver(MadderInventoryLogDir())
	errors.ContextCloseAfter(ctx, obs)
	return obs
}

// WireWithCleanup constructs a FileObserver rooted at
// MadderInventoryLogDir and returns it alongside a cleanup function
// the caller must invoke (typically `defer cleanup()`) to flush the
// hyphence buffer at shutdown.
//
// Honors MADDER_INVENTORY_LOG=0 the same way as WireDefault. When
// disabled, the returned cleanup is a no-op.
//
// Use this when you don't have an errors.Context — for embedded
// libraries, test harnesses, or non-futility-driven entry points.
// Most callers should prefer WireDefault.
func WireWithCleanup() (obs Observer, cleanup func() error) {
	if isDisabledByEnv() {
		return NopObserver{}, func() error { return nil }
	}

	fobs := NewFileObserver(MadderInventoryLogDir())
	return fobs, fobs.Close
}

func isDisabledByEnv() bool {
	return strings.TrimSpace(os.Getenv(envVarDisable)) == "0"
}
