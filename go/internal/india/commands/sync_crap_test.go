package commands

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/amarbel-llc/crap/go-crap/v2/crap"
	"github.com/amarbel-llc/crap/go-crap/v2/ndjsoncrap"
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/piggy/go/pkgs/markl"
)

// testMarklId returns a deterministic sha256 markl id seeded from a
// string; same per-package idiom as blob_stores.makeSftpFallbackTestId.
func testMarklId(t *testing.T, seed string) domain_interfaces.MarklId {
	t.Helper()
	id, repool := markl.FormatHashSha256.GetMarklIdForString(seed)
	t.Cleanup(repool)
	return id
}

// emitSyncCrap replays the exact record sequence runStoreCrap produces for
// a given run: a coarse scan phase, then an Operation with one
// Item/Skip/Fail per blob, then a tallied operation_end. This is the seam
// the wire and viewport consumers see; driving the Reporter directly lets
// the per-blob verdict mapping (transferred -> Item, exists -> Skip,
// failed -> Fail) be asserted without standing up real blob stores.
func emitSyncCrap(
	out io.Writer,
	sourceLabel string,
	total int,
	emit func(op *crap.Operation),
) error {
	reporter := crap.NewReporter(out, crap.ReporterOptions{Source: "madder"})
	scan := reporter.Phase(fmt.Sprintf("scanning %s", sourceLabel))
	scan.Done()
	op := reporter.Operation("sync", crap.OpOptions{Total: total})
	emit(op)
	op.Finish()
	return reporter.Err()
}

func TestSyncRunStoreCrapStream(t *testing.T) {
	var buf bytes.Buffer

	okBlob := testMarklId(t, "ok-blob")
	skipBlob := testMarklId(t, "skip-blob")
	badBlob := testMarklId(t, "bad-blob")

	okLabel := formatSyncTestPoint(okBlob, 42)
	skipLabel := formatSyncTestPoint(skipBlob, 0)
	badLabel := formatSyncTestPoint(badBlob, 0)

	if err := emitSyncCrap(&buf, "source-store", 3, func(op *crap.Operation) {
		op.Item(okLabel, 42)
		op.Skip(skipLabel, syncStateExists)
		op.Fail(badLabel, errors.New("boom"))
	}); err != nil {
		t.Fatalf("emitSyncCrap: %v", err)
	}

	var records []ndjsoncrap.Record
	r := ndjsoncrap.NewReader(&buf)
	for {
		rec, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		records = append(records, rec)
	}

	// Meta, node_start(scan), node_end(scan), operation_start,
	// item(done), item(skipped), item(failed), operation_end.
	if len(records) != 8 {
		t.Fatalf("expected 8 records, got %d: %#v", len(records), records)
	}

	if _, ok := records[0].(ndjsoncrap.Meta); !ok {
		t.Errorf("record 0: expected Meta, got %T", records[0])
	}

	ns, ok := records[1].(ndjsoncrap.NodeStart)
	if !ok || ns.Name != "scanning source-store" || ns.Parent != nil {
		t.Errorf("record 1: expected top-level scan node_start, got %#v", records[1])
	}
	if ne, ok := records[2].(ndjsoncrap.NodeEnd); !ok || ne.ExitCode == nil || *ne.ExitCode != 0 {
		t.Errorf("record 2: expected scan node_end exit 0, got %#v", records[2])
	}

	os, ok := records[3].(ndjsoncrap.OperationStart)
	if !ok || os.Name != "sync" || os.Total != 3 {
		t.Errorf("record 3: expected operation_start sync Total=3, got %#v", records[3])
	}

	done, ok := records[4].(ndjsoncrap.Item)
	if !ok || done.State != ndjsoncrap.ItemDone || done.Label != okLabel || done.Bytes != 42 {
		t.Errorf("record 4: expected done item %q bytes=42, got %#v", okLabel, records[4])
	}

	skipped, ok := records[5].(ndjsoncrap.Item)
	if !ok || skipped.State != ndjsoncrap.ItemSkipped {
		t.Errorf("record 5: expected skipped item, got %#v", records[5])
	}
	if skipped.Directive == nil || skipped.Directive.Reason != syncStateExists {
		t.Errorf("record 5: expected skip directive reason %q, got %#v", syncStateExists, records[5])
	}

	failed, ok := records[6].(ndjsoncrap.Item)
	if !ok || failed.State != ndjsoncrap.ItemFailed || failed.Diagnostic["error"] != "boom" {
		t.Errorf("record 6: expected failed item with error diag, got %#v", records[6])
	}

	end, ok := records[7].(ndjsoncrap.OperationEnd)
	if !ok || end.Done != 1 || end.Skipped != 1 || end.Failed != 1 || end.Total != 3 {
		t.Errorf("record 7: expected operation_end{done:1,skipped:1,failed:1,total:3}, got %#v", records[7])
	}
	if end.OK {
		t.Errorf("record 7: operation_end ok should be false when failed>0, got %#v", records[7])
	}
}

// TestSyncScanDiagnostic pins the diagnostic syncScanDiagnostic builds for
// a failed scan phase (#237): the source store id plus the
// connection-identifying config keys, and — load-bearing — none of the
// secret or local-path keys (password, private-key-path).
func TestSyncScanDiagnostic(t *testing.T) {
	config := &blob_store_configs.TomlSFTPV0{
		Host:           "sftp.example.test",
		Port:           2222,
		User:           "syncer",
		Password:       "hunter2",
		PrivateKeyPath: "/home/someone/.ssh/id_ed25519",
		RemotePath:     "/srv/blobs",
	}

	diag := syncScanDiagnostic("/repo-default", config)

	want := map[string]any{
		"source":          "/repo-default",
		"blob-store-type": "sftp",
		"host":            "sftp.example.test",
		"port":            "2222",
		"user":            "syncer",
		"remote-path":     "/srv/blobs",
	}

	for key, wantValue := range want {
		if diag[key] != wantValue {
			t.Errorf("diag[%q]: got %#v, want %#v", key, diag[key], wantValue)
		}
	}

	// Exhaustive: the allowlist is the sole gate before wire emission, so
	// any key outside `want` (password, private-key-path, a future
	// allowlist addition…) is a leak, not an enhancement.
	for key, got := range diag {
		if _, ok := want[key]; !ok {
			t.Errorf("diag[%q] must not surface, got %#v", key, got)
		}
	}

	if diag := syncScanDiagnostic("src", nil); diag["source"] != "src" {
		t.Errorf("nil config: expected source-only diag, got %#v", diag)
	}
}

// TestSyncScanFailDiagOnWire replays the scan-failure record sequence
// runStoreCrap produces and asserts the diagnostic rides the scan
// node_end (go-crap crap#22), making the phase a self-sufficient verdict
// unit — no paired result-family Test record.
func TestSyncScanFailDiagOnWire(t *testing.T) {
	var buf bytes.Buffer

	reporter := crap.NewReporter(&buf, crap.ReporterOptions{Source: "madder"})
	scan := reporter.Phase("scanning src")
	scan.FailDiag(
		errors.New("connect refused"),
		syncScanDiagnostic("src", &blob_store_configs.TomlSFTPV0{
			Host:       "sftp.example.test",
			RemotePath: "/srv/blobs",
		}),
	)
	if err := reporter.Err(); err != nil {
		t.Fatalf("reporter.Err: %v", err)
	}

	var nodeEnd *ndjsoncrap.NodeEnd
	r := ndjsoncrap.NewReader(&buf)
	for {
		rec, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if ne, ok := rec.(ndjsoncrap.NodeEnd); ok {
			nodeEnd = &ne
		}
	}

	if nodeEnd == nil {
		t.Fatal("no node_end record on the wire")
	}
	if nodeEnd.ExitCode == nil || *nodeEnd.ExitCode != 1 {
		t.Errorf("node_end: expected exit 1, got %#v", nodeEnd)
	}
	if nodeEnd.Diagnostic["source"] != "src" ||
		nodeEnd.Diagnostic["host"] != "sftp.example.test" {
		t.Errorf("node_end diagnostic missing scan context, got %#v",
			nodeEnd.Diagnostic)
	}
}

// TestSyncCrapDistinctLabels guards the MarklId aliasing footgun: two
// distinct blobs must yield two distinct item labels. runStoreCrap uses a
// two-pass walk (count, then transfer) precisely so retained ids cannot
// alias a single reused pool buffer; this asserts the labels stay distinct.
func TestSyncCrapDistinctLabels(t *testing.T) {
	var buf bytes.Buffer

	a := testMarklId(t, "blob-a")
	b := testMarklId(t, "blob-b")

	labelA := formatSyncTestPoint(a, 0)
	labelB := formatSyncTestPoint(b, 0)

	if labelA == labelB {
		t.Fatalf("two distinct blobs produced identical labels %q", labelA)
	}

	if err := emitSyncCrap(&buf, "src", 2, func(op *crap.Operation) {
		op.Item(labelA, 0)
		op.Item(labelB, 0)
	}); err != nil {
		t.Fatalf("emitSyncCrap: %v", err)
	}

	var labels []string
	r := ndjsoncrap.NewReader(&buf)
	for {
		rec, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if item, ok := rec.(ndjsoncrap.Item); ok {
			labels = append(labels, item.Label)
		}
	}

	if len(labels) != 2 {
		t.Fatalf("expected 2 item labels, got %d: %#v", len(labels), labels)
	}
	if labels[0] == labels[1] {
		t.Errorf("item labels aliased to the same value %q (clone footgun)", labels[0])
	}
}
