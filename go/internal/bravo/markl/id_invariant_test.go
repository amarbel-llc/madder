package markl

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
)

// assertInvariant holds ADR-0001: either (a) null state (nil format,
// empty data) or (b) non-nil format with len(data) == format.GetSize().
func assertInvariant(t *testing.T, label string, id domain_interfaces.MarklId) {
	t.Helper()

	format := id.GetMarklFormat()
	data := id.GetBytes()

	switch {
	case format == nil && len(data) == 0:
		// null state
	case format == nil && len(data) != 0:
		t.Errorf("%s: nil format with %d bytes of data (invariant violation)", label, len(data))
	case format != nil && len(data) != format.GetSize():
		t.Errorf("%s: format %q size %d but data len %d (invariant violation)",
			label, format.GetMarklFormatId(), format.GetSize(), len(data))
	}
}

func TestInvariant_ZeroValueIdIsNullState(t *testing.T) {
	var id Id
	assertInvariant(t, "zero-value Id", &id)
}

func TestInvariant_HashGetMarklId(t *testing.T) {
	for formatId, fh := range formatHashes {
		fh := fh
		t.Run(formatId, func(t *testing.T) {
			hash, repool := fh.Get()
			defer repool()

			id, idRepool := hash.GetMarklId()
			defer idRepool()

			assertInvariant(t, "Hash.GetMarklId (unwritten)", id)

			if _, err := hash.Write([]byte("hello world")); err != nil {
				t.Fatal(err)
			}

			id2, id2Repool := hash.GetMarklId()
			defer id2Repool()

			assertInvariant(t, "Hash.GetMarklId (written)", id2)
		})
	}
}

func TestInvariant_HashGetBlobIdForReader(t *testing.T) {
	for formatId, fh := range formatHashes {
		fh := fh
		t.Run(formatId, func(t *testing.T) {
			hash, repool := fh.Get()
			defer repool()

			input := bytes.Repeat([]byte{0xAB}, fh.GetSize())

			id, idRepool := hash.GetBlobIdForReader(bytes.NewReader(input))
			defer idRepool()

			assertInvariant(t, "Hash.GetBlobIdForReader", id)
		})
	}
}

func TestInvariant_HashGetBlobIdForReaderAt(t *testing.T) {
	for formatId, fh := range formatHashes {
		fh := fh
		t.Run(formatId, func(t *testing.T) {
			hash, repool := fh.Get()
			defer repool()

			prefix := []byte("ignored-prefix-")
			body := bytes.Repeat([]byte{0xCD}, fh.GetSize())
			full := append(append([]byte{}, prefix...), body...)

			id, idRepool := hash.GetBlobIdForReaderAt(
				bytes.NewReader(full),
				int64(len(prefix)),
			)
			defer idRepool()

			assertInvariant(t, "Hash.GetBlobIdForReaderAt", id)
		})
	}
}

func TestInvariant_SetMarklId_CorrectSize(t *testing.T) {
	for formatId, fh := range formatHashes {
		fh := fh
		t.Run(formatId, func(t *testing.T) {
			bites := bytes.Repeat([]byte{0xEE}, fh.GetSize())

			var id Id
			if err := id.SetMarklId(formatId, bites); err != nil {
				t.Fatalf("SetMarklId with correct-size bytes should succeed, got: %v", err)
			}

			assertInvariant(t, "SetMarklId correct size", &id)
		})
	}
}

func TestInvariant_SetMarklId_WrongSize_Errors(t *testing.T) {
	for formatId, fh := range formatHashes {
		fh := fh
		t.Run(formatId, func(t *testing.T) {
			var id Id

			tooShort := bytes.Repeat([]byte{0x01}, fh.GetSize()-1)
			if err := id.SetMarklId(formatId, tooShort); err == nil {
				t.Errorf("SetMarklId with too-short bytes should error")
			}
			assertInvariant(t, "SetMarklId rejected too-short", &id)

			tooLong := bytes.Repeat([]byte{0x01}, fh.GetSize()+1)
			if err := id.SetMarklId(formatId, tooLong); err == nil {
				t.Errorf("SetMarklId with too-long bytes should error")
			}
			assertInvariant(t, "SetMarklId rejected too-long", &id)
		})
	}
}

func TestInvariant_SetHexBytes_CorrectHex(t *testing.T) {
	for formatId, fh := range formatHashes {
		fh := fh
		t.Run(formatId, func(t *testing.T) {
			raw := bytes.Repeat([]byte{0xAB}, fh.GetSize())
			hexStr := hex.EncodeToString(raw)

			var id Id
			if err := SetHexBytes(formatId, &id, []byte(hexStr)); err != nil {
				t.Fatalf("SetHexBytes with correct-size hex should succeed, got: %v", err)
			}

			assertInvariant(t, "SetHexBytes correct size", &id)

			if !bytes.Equal(id.GetBytes(), raw) {
				t.Errorf("SetHexBytes round-trip mismatch")
			}
		})
	}
}

func TestInvariant_SetHexBytes_WrongSize_Errors(t *testing.T) {
	for formatId, fh := range formatHashes {
		fh := fh
		t.Run(formatId, func(t *testing.T) {
			var id Id

			// hex of wrong decoded length
			shortHex := strings.Repeat("ab", fh.GetSize()-1)
			if err := SetHexBytes(formatId, &id, []byte(shortHex)); err == nil {
				t.Errorf("SetHexBytes with too-short hex should error")
			}
			assertInvariant(t, "SetHexBytes rejected too-short", &id)

			longHex := strings.Repeat("ab", fh.GetSize()+1)
			if err := SetHexBytes(formatId, &id, []byte(longHex)); err == nil {
				t.Errorf("SetHexBytes with too-long hex should error")
			}
			assertInvariant(t, "SetHexBytes rejected too-long", &id)
		})
	}
}

func TestInvariant_ResetWith(t *testing.T) {
	for formatId, fh := range formatHashes {
		fh := fh
		t.Run(formatId, func(t *testing.T) {
			hash, repool := fh.Get()
			defer repool()

			if _, err := hash.Write([]byte("seed")); err != nil {
				t.Fatal(err)
			}

			src, srcRepool := hash.GetMarklId()
			defer srcRepool()

			srcConcrete, ok := src.(*Id)
			if !ok {
				t.Fatalf("expected *Id, got %T", src)
			}

			var dst Id
			dst.ResetWith(*srcConcrete)

			assertInvariant(t, "ResetWith", &dst)

			if !bytes.Equal(dst.GetBytes(), src.GetBytes()) {
				t.Errorf("ResetWith data mismatch")
			}
		})
	}
}

func TestInvariant_ResetWith_EmptySrcClearsStaleData(t *testing.T) {
	for formatId, fh := range formatHashes {
		fh := fh
		t.Run(formatId, func(t *testing.T) {
			hash, repool := fh.Get()
			defer repool()

			if _, err := hash.Write([]byte("seed")); err != nil {
				t.Fatal(err)
			}

			populated, populatedRepool := hash.GetMarklId()
			defer populatedRepool()

			dst, ok := populated.(*Id)
			if !ok {
				t.Fatalf("expected *Id, got %T", populated)
			}

			var empty Id
			dst.ResetWith(empty)

			assertInvariant(t, "ResetWith empty src", dst)

			if !dst.IsEmpty() {
				t.Errorf("expected IsEmpty after ResetWith(empty), got data=%d bytes format=%v",
					len(dst.GetBytes()), dst.GetMarklFormat())
			}
			if got := len(dst.GetBytes()); got != 0 {
				t.Errorf("expected GetBytes len 0, got %d", got)
			}
			if got := dst.GetMarklFormat(); got != nil {
				t.Errorf("expected nil format, got %v", got)
			}
		})
	}
}

func TestInvariant_AfterReset(t *testing.T) {
	for formatId, fh := range formatHashes {
		fh := fh
		t.Run(formatId, func(t *testing.T) {
			hash, repool := fh.Get()
			defer repool()

			if _, err := hash.Write([]byte("populate")); err != nil {
				t.Fatal(err)
			}

			id, idRepool := hash.GetMarklId()
			defer idRepool()

			concrete := id.(*Id)
			concrete.Reset()
			assertInvariant(t, "after Reset", concrete)

			// populate again, then ResetWithPurpose
			concrete2, concrete2Repool := hash.GetMarklId()
			defer concrete2Repool()
			concrete2Cast := concrete2.(*Id)
			concrete2Cast.ResetWithPurpose("some-purpose")
			assertInvariant(t, "after ResetWithPurpose", concrete2Cast)
		})
	}
}

func TestInvariant_ResetDataForFormat_NilFormatPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic, got nil")
		}
		err, ok := r.(error)
		if !ok {
			t.Fatalf("expected panic value to be an error, got %T: %v", r, r)
		}
		if !errors.Is(err, ErrNilFormat) {
			t.Errorf("expected panic wrapping ErrNilFormat, got: %v", err)
		}
	}()

	var id Id
	id.resetDataForFormat(nil)
}
