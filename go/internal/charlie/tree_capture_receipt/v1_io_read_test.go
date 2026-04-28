package tree_capture_receipt

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadV1_RoundTripNoHint(t *testing.T) {
	// Inputs in arbitrary order. WriteV1 sorts in place by (Root, Path),
	// so the wire ordering is the sorted permutation of these entries.
	input := []EntryV1{
		{Path: "a.txt", Root: "src", Type: TypeFile, Mode: 0o644, Size: 10, BlobId: "blake2b256-x"},
		{Path: ".", Root: "src", Type: TypeDir, Mode: 0o755},
		{Path: "link", Root: "src", Type: TypeSymlink, Mode: 0o777, Target: "../bar"},
	}
	wantSorted := []EntryV1{
		{Path: ".", Root: "src", Type: TypeDir, Mode: 0o755},
		{Path: "a.txt", Root: "src", Type: TypeFile, Mode: 0o644, Size: 10, BlobId: "blake2b256-x"},
		{Path: "link", Root: "src", Type: TypeSymlink, Mode: 0o777, Target: "../bar"},
	}

	var buf bytes.Buffer
	if _, err := WriteV1(&buf, append([]EntryV1{}, input...)); err != nil {
		t.Fatalf("WriteV1: %v", err)
	}

	got, err := ReadV1(&buf)
	if err != nil {
		t.Fatalf("ReadV1: %v", err)
	}

	if got.Hint != nil {
		t.Errorf("expected nil Hint, got %+v", got.Hint)
	}

	if len(got.Entries) != len(wantSorted) {
		t.Fatalf("entries len: got %d want %d", len(got.Entries), len(wantSorted))
	}

	for i := range wantSorted {
		if got.Entries[i] != wantSorted[i] {
			t.Errorf("entries[%d]:\n  got:  %+v\n  want: %+v", i, got.Entries[i], wantSorted[i])
		}
	}
}

func TestReadV1_RoundTripWithHint(t *testing.T) {
	hint := &StoreHint{
		StoreId:       ".work",
		ConfigMarklId: "blake2b256-9ft3m74l5t2ppwjrvfg3wp380jqj2zfrm6zevxqx34sdethvey0s5vm9gd",
	}

	want := []EntryV1{
		{Path: "a.txt", Root: "src", Type: TypeFile, Mode: 0o644, Size: 10, BlobId: "blake2b256-x"},
	}

	var buf bytes.Buffer
	if _, err := WriteV1WithHint(&buf, append([]EntryV1{}, want...), hint); err != nil {
		t.Fatalf("WriteV1WithHint: %v", err)
	}

	got, err := ReadV1(&buf)
	if err != nil {
		t.Fatalf("ReadV1: %v", err)
	}

	if got.Hint == nil {
		t.Fatalf("expected non-nil Hint, got nil")
	}
	if got.Hint.StoreId != hint.StoreId {
		t.Errorf("Hint.StoreId: got %q want %q", got.Hint.StoreId, hint.StoreId)
	}
	if got.Hint.ConfigMarklId != hint.ConfigMarklId {
		t.Errorf("Hint.ConfigMarklId: got %q want %q", got.Hint.ConfigMarklId, hint.ConfigMarklId)
	}
}

func TestReadFrom_DispatchesV1(t *testing.T) {
	want := []EntryV1{
		{Path: "a", Root: ".", Type: TypeFile, Mode: 0o644, Size: 1, BlobId: "x"},
	}

	var buf bytes.Buffer
	if _, err := WriteV1(&buf, append([]EntryV1{}, want...)); err != nil {
		t.Fatalf("WriteV1: %v", err)
	}

	blob, typeTag, err := ReadFrom(&buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}

	if typeTag != TypeTagV1 {
		t.Errorf("typeTag: got %q want %q", typeTag, TypeTagV1)
	}

	v1, ok := blob.(*V1)
	if !ok {
		t.Fatalf("expected *V1, got %T", blob)
	}

	if len(v1.Entries) != 1 || v1.Entries[0].Path != "a" {
		t.Errorf("entries: got %+v", v1.Entries)
	}
}

func TestReadFrom_RejectsUnknownTypeTag(t *testing.T) {
	receipt := "---\n! madder-tree_capture-receipt-v999\n---\n\n"

	_, typeTag, err := ReadFrom(strings.NewReader(receipt))
	if err == nil {
		t.Fatal("expected error for unknown type-tag, got nil")
	}
	if typeTag != "madder-tree_capture-receipt-v999" {
		t.Errorf("typeTag: got %q want %q", typeTag, "madder-tree_capture-receipt-v999")
	}
}

func TestReadV1_RejectsWrongTypeTag(t *testing.T) {
	receipt := "---\n! madder-tree_capture-receipt-v999\n---\n\n"

	_, err := ReadV1(strings.NewReader(receipt))
	if err == nil {
		t.Fatal("expected error for wrong type-tag, got nil")
	}
}

func TestReadV1_TolerantOfUnknownMetadataLine(t *testing.T) {
	receipt := "---\n" +
		"- store/.work < blake2b256-x\n" +
		"- future-key value\n" +
		"! " + TypeTagV1 + "\n" +
		"---\n\n" +
		`{"path":"a","root":".","type":"file","mode":"0644","size":1,"blob_id":"x"}` + "\n"

	got, err := ReadV1(strings.NewReader(receipt))
	if err != nil {
		t.Fatalf("ReadV1: %v", err)
	}

	if got.Hint == nil || got.Hint.StoreId != ".work" {
		t.Errorf("expected hint .work, got %+v", got.Hint)
	}
}
