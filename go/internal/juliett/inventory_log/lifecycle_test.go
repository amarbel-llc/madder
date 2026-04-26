package inventory_log

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// TestFileObserver_ClosedByContextAfter is the regression test for
// madder#75. It pins the production wiring contract:
// errors.ContextCloseAfter on the observer's construction context
// fires Close when errCtx.Run completes — which is the same path
// futility.wrapped.Run uses to wrap cmd.Run. Without this hook firing,
// hyphence's bufio.Writer never flushes its trailing buffer and the
// session file is empty or truncated.
func TestFileObserver_ClosedByContextAfter(t *testing.T) {
	withFixedClock(t)

	dir := t.TempDir()
	obs := NewFileObserver(dir)
	obs.randHex = func() string { return "ctxa" }

	ctx := errors.MakeContextDefault()
	errors.ContextCloseAfter(ctx, obs)

	if err := ctx.Run(func(_ errors.Context) {
		obs.OnBlobPublished(domain_interfaces.BlobWriteEvent{
			StoreId: "default",
			Size:    7,
			Op:      domain_interfaces.BlobWriteOpWritten,
		})
	}); err != nil {
		t.Fatalf("ctx.Run: %v", err)
	}

	// At this point ctx.Run has returned, runAfter has fired, and
	// FileObserver.Close has flushed and closed the file.
	dayDir := filepath.Join(dir, "2026-04-26")
	entries, err := os.ReadDir(dayDir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dayDir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 session file, got %d", len(entries))
	}

	content, err := os.ReadFile(filepath.Join(dayDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	body := assertHyphenceHeader(t, content)
	lines := splitNDJSON(body)
	if len(lines) != 1 {
		t.Fatalf("expected 1 NDJSON line after After-hook close, got %d:\n%s",
			len(lines), body)
	}
}
