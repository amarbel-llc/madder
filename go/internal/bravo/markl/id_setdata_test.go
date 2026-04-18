package markl

import (
	"bytes"
	"testing"
)

// ADR-0001 invariant: an Id's data length must match its format's declared
// size whenever data is non-empty. These tests exercise the setData
// construction boundary directly. See issue #13.

func TestSetData_NilFormatWithNonEmptyBytes_Errors(t *testing.T) {
	var id Id

	if err := id.setData([]byte{0x01, 0x02, 0x03}); err == nil {
		t.Fatal("expected error for non-empty bytes with nil format, got nil")
	}

	if got := len(id.GetBytes()); got != 0 {
		t.Errorf("id.data should remain empty after rejected setData, got len=%d", got)
	}
}

func TestSetData_NilFormatWithEmptyBytes_Permitted(t *testing.T) {
	var id Id

	if err := id.setData(nil); err != nil {
		t.Fatalf("empty bytes should be permitted with nil format, got: %v", err)
	}

	if err := id.setData([]byte{}); err != nil {
		t.Fatalf("empty bytes should be permitted with nil format, got: %v", err)
	}
}

func TestSetData_FormatSetWithWrongSize_Errors(t *testing.T) {
	for formatId, fh := range formatHashes {
		fh := fh
		t.Run(formatId, func(t *testing.T) {
			var id Id
			id.format = fh
			tooShort := make([]byte, fh.GetSize()-1)

			if err := id.setData(tooShort); err == nil {
				t.Errorf("expected error for %d-byte input to %s (size %d)",
					len(tooShort), formatId, fh.GetSize())
			}

			tooLong := make([]byte, fh.GetSize()+1)

			if err := id.setData(tooLong); err == nil {
				t.Errorf("expected error for %d-byte input to %s (size %d)",
					len(tooLong), formatId, fh.GetSize())
			}
		})
	}
}

func TestSetData_FormatSetWithCorrectSize_Succeeds(t *testing.T) {
	for formatId, fh := range formatHashes {
		fh := fh
		t.Run(formatId, func(t *testing.T) {
			var id Id
			id.format = fh
			bites := bytes.Repeat([]byte{0xAB}, fh.GetSize())

			if err := id.setData(bites); err != nil {
				t.Fatalf("correct-size input should succeed, got: %v", err)
			}

			if !bytes.Equal(id.GetBytes(), bites) {
				t.Errorf("id.data mismatch: got %x, want %x", id.GetBytes(), bites)
			}
		})
	}
}
