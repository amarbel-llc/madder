package commands

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/amarbel-llc/crap/go-crap/v2/ndjsoncrap"
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
)

// testMarklId returns a deterministic sha256 markl id seeded from a
// string; same per-package idiom as blob_stores.makeSftpFallbackTestId.
func testMarklId(t *testing.T, seed string) domain_interfaces.MarklId {
	t.Helper()
	id, repool := markl.FormatHashSha256.GetMarklIdForString(seed)
	t.Cleanup(repool)
	return id
}

func TestSyncCrapSinkStream(t *testing.T) {
	var buf bytes.Buffer
	sink := newSyncCrapSink(&buf, io.Discard)

	sink.transferred(testMarklId(t, "ok-blob"), 42)
	sink.failed(testMarklId(t, "bad-blob"), 0, errors.New("boom"))
	sink.listError(errors.New("list failed"))
	sink.summary(1, 2, 0, 3)
	sink.finalize()

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

	// Meta header, 3 tests, summary.
	if len(records) != 5 {
		t.Fatalf("expected 5 records, got %d: %#v", len(records), records)
	}
	if _, ok := records[0].(ndjsoncrap.Meta); !ok {
		t.Errorf("record 0: expected Meta, got %T", records[0])
	}
	first, ok := records[1].(ndjsoncrap.Test)
	if !ok || !first.OK || first.N != 1 {
		t.Errorf("record 1: expected passing Test n=1, got %#v", records[1])
	}
	second, ok := records[2].(ndjsoncrap.Test)
	if !ok || second.OK || second.Diagnostic["error"] != "boom" {
		t.Errorf("record 2: expected failing Test with error diag, got %#v", records[2])
	}
	last, ok := records[4].(ndjsoncrap.Summary)
	if !ok || last.Passed != 1 || last.Failed != 2 || last.Total != 3 {
		t.Errorf("record 4: expected Summary{1,2,_,3}, got %#v", records[4])
	}
	// tap-ndjson(7): a producer that learns its plan only after running
	// MAY omit the plan record and report the count in plan_count.
	if !ok || last.PlanCount != 3 || !last.Valid {
		t.Errorf("record 4: expected PlanCount=3 Valid=true, got %#v", records[4])
	}
}
