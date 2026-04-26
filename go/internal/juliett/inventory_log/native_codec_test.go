package inventory_log

import (
	"encoding/json"
	"testing"
	tyme "time"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/0/ids"
)

// withFixedClock pins nowTai/pidNow for the duration of the test and
// restores them on cleanup. Tests that exercise the native codec rely on
// deterministic ts/pid output, so this is shared setup.
func withFixedClock(t *testing.T) {
	t.Helper()
	prevNow, prevPid := nowTai, pidNow
	t.Cleanup(func() {
		nowTai = prevNow
		pidNow = prevPid
	})
	nowTai = func() ids.Tai {
		return ids.TaiFromTime1(
			tyme.Date(2026, 4, 26, 12, 0, 0, 0, tyme.UTC),
		)
	}
	pidNow = func() int { return 12345 }
}

// TestNativeCodec_GoldenLine pins the JSON shape of a freshly emitted
// blob-write-published-v1 record. Field order matches the encoding order
// in blobWriteRecord; if Go's json package ever reorders, this test
// breaks loudly.
func TestNativeCodec_GoldenLine(t *testing.T) {
	withFixedClock(t)

	codec, ok := Global.Lookup("blob-write-published-v1")
	if !ok {
		t.Fatal("native codec not registered in Global")
	}

	ev := domain_interfaces.BlobWriteEvent{
		StoreId: "default",
		Size:    1234,
		Op:      domain_interfaces.BlobWriteOpWritten,
	}

	got, err := codec.Encode(ev)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	wantTs := nowTai().String()
	want := `{"type":"blob-write-published-v1","ts":"` + wantTs +
		`","utility":"madder","pid":12345,` +
		`"store_id":"default","markl_id":"","size":1234,"op":"written"}`

	if string(got) != want {
		t.Errorf("native codec line mismatch\n got: %s\nwant: %s", got, want)
	}
}

// TestNativeCodec_RoundTrip asserts Decode → Encode → Decode is
// idempotent on the fields the decoder retains. Decode is documented as
// lossy on Ts/Pid/Utility/MarklId; this test pins what survives.
func TestNativeCodec_RoundTrip(t *testing.T) {
	withFixedClock(t)

	codec, ok := Global.Lookup("blob-write-published-v1")
	if !ok {
		t.Fatal("native codec not registered in Global")
	}

	original := domain_interfaces.BlobWriteEvent{
		StoreId:     "myStore",
		Size:        9001,
		Op:          domain_interfaces.BlobWriteOpVerifyMatch,
		Description: "ingested from /mnt/import",
	}

	line, err := codec.Encode(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := codec.Decode(line)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	got := decoded.(domain_interfaces.BlobWriteEvent)
	if got.StoreId != original.StoreId {
		t.Errorf("StoreId: got %q, want %q", got.StoreId, original.StoreId)
	}
	if got.Size != original.Size {
		t.Errorf("Size: got %d, want %d", got.Size, original.Size)
	}
	if got.Op != original.Op {
		t.Errorf("Op: got %q, want %q", got.Op, original.Op)
	}
	if got.Description != original.Description {
		t.Errorf("Description: got %q, want %q", got.Description, original.Description)
	}
}

// TestNativeCodec_IsValidJSON guards against any future encoder change
// that breaks JSON validity. Catches issues earlier than full integration.
func TestNativeCodec_IsValidJSON(t *testing.T) {
	withFixedClock(t)

	codec, _ := Global.Lookup("blob-write-published-v1")

	line, err := codec.Encode(domain_interfaces.BlobWriteEvent{
		StoreId: "x",
		Size:    1,
		Op:      domain_interfaces.BlobWriteOpExists,
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(line, &parsed); err != nil {
		t.Fatalf("encoded line is not valid JSON: %v\n%s", err, line)
	}

	if parsed["type"] != "blob-write-published-v1" {
		t.Errorf("type field: got %v, want blob-write-published-v1", parsed["type"])
	}
}
