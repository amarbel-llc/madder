package markl

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"golang.org/x/crypto/blake2b"
)

// TestGetMarklId_LenEqualsFormatSize locks in the invariant established by
// ADR-0001: every id returned by Hash.GetMarklId has len(data) == format size,
// regardless of whether the hash has been written to.
func TestGetMarklId_LenEqualsFormatSize(t *testing.T) {
	for formatId, fh := range formatHashes {
		fh := fh
		t.Run(formatId, func(t *testing.T) {
			hash, repool := fh.Get()
			defer repool()

			idUnwritten, idRepool := hash.GetMarklId()
			if got, want := len(idUnwritten.GetBytes()), fh.GetSize(); got != want {
				t.Errorf("unwritten: len=%d, want %d", got, want)
			}
			idRepool()

			if _, err := hash.Write([]byte("hello world")); err != nil {
				t.Fatal(err)
			}

			idWritten, idRepool := hash.GetMarklId()
			if got, want := len(idWritten.GetBytes()), fh.GetSize(); got != want {
				t.Errorf("after write: len=%d, want %d", got, want)
			}
			idRepool()
		})
	}
}

// TestGetMarklId_UnwrittenMatchesFormatNull guards the ADR-0001 invariant at
// the byte level: an unwritten hash must produce the same bytes as
// formatHash.null (the digest of empty input computed at init).
func TestGetMarklId_UnwrittenMatchesFormatNull(t *testing.T) {
	for formatId, fh := range formatHashes {
		fh := fh
		t.Run(formatId, func(t *testing.T) {
			hash, repool := fh.Get()
			defer repool()

			id, idRepool := hash.GetMarklId()
			defer idRepool()

			got := id.GetBytes()
			want := fh.null.GetBytes()

			if !bytes.Equal(got, want) {
				t.Errorf(
					"unwritten id diverges from formatHash.null\n got  len=%d %x\n want len=%d %x",
					len(got), got, len(want), want,
				)
			}
		})
	}
}

// TestGetMarklId_MatchesStdlibSHA256 verifies the digest bytes match the
// stdlib reference for SHA-256 across a range of inputs.
func TestGetMarklId_MatchesStdlibSHA256(t *testing.T) {
	cases := []struct {
		name  string
		input []byte
	}{
		{"empty", nil},
		{"single_byte", []byte{0x00}},
		{"hello_world", []byte("hello world")},
		{"multi_line", []byte("line1\nline2\nline3\n")},
		{"block_boundary_63B", bytes.Repeat([]byte{0xab}, 63)},
		{"block_boundary_64B", bytes.Repeat([]byte{0xab}, 64)},
		{"block_boundary_65B", bytes.Repeat([]byte{0xab}, 65)},
		{"large_4KB", bytes.Repeat([]byte{0xcd}, 4096)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hash, repool := FormatHashSha256.Get()
			defer repool()

			if _, err := hash.Write(tc.input); err != nil {
				t.Fatal(err)
			}

			id, idRepool := hash.GetMarklId()
			defer idRepool()

			want := sha256.Sum256(tc.input)
			if !bytes.Equal(id.GetBytes(), want[:]) {
				t.Errorf("got %x, want %x", id.GetBytes(), want[:])
			}
		})
	}
}

// TestGetMarklId_MatchesStdlibBlake2b256 mirrors the SHA-256 reference test
// for Blake2b-256.
func TestGetMarklId_MatchesStdlibBlake2b256(t *testing.T) {
	cases := []struct {
		name  string
		input []byte
	}{
		{"empty", nil},
		{"single_byte", []byte{0x00}},
		{"hello_world", []byte("hello world")},
		{"block_boundary_127B", bytes.Repeat([]byte{0xab}, 127)},
		{"block_boundary_128B", bytes.Repeat([]byte{0xab}, 128)},
		{"block_boundary_129B", bytes.Repeat([]byte{0xab}, 129)},
		{"large_4KB", bytes.Repeat([]byte{0xcd}, 4096)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hash, repool := FormatHashBlake2b256.Get()
			defer repool()

			if _, err := hash.Write(tc.input); err != nil {
				t.Fatal(err)
			}

			id, idRepool := hash.GetMarklId()
			defer idRepool()

			ref, err := blake2b.New256(nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ref.Write(tc.input); err != nil {
				t.Fatal(err)
			}
			want := ref.Sum(nil)

			if !bytes.Equal(id.GetBytes(), want) {
				t.Errorf("got %x, want %x", id.GetBytes(), want)
			}
		})
	}
}

// TestHash_Reset_ReturnsToEmptyDigest verifies Reset brings the hash back to
// the empty-input state (matching the digest of "").
func TestHash_Reset_ReturnsToEmptyDigest(t *testing.T) {
	for formatId, fh := range formatHashes {
		fh := fh
		t.Run(formatId, func(t *testing.T) {
			hash, repool := fh.Get()
			defer repool()

			if _, err := hash.Write([]byte("polluting content")); err != nil {
				t.Fatal(err)
			}

			hash.Reset()

			id, idRepool := hash.GetMarklId()
			defer idRepool()

			if !bytes.Equal(id.GetBytes(), fh.null.GetBytes()) {
				t.Errorf(
					"after Reset id != formatHash.null\n got  %x\n want %x",
					id.GetBytes(), fh.null.GetBytes(),
				)
			}
		})
	}
}

// TestHash_Sum_MatchesGetMarklId verifies that Sum and GetMarklId produce the
// same digest for the same hash state.
func TestHash_Sum_MatchesGetMarklId(t *testing.T) {
	hash, repool := FormatHashSha256.Get()
	defer repool()

	if _, err := hash.Write([]byte("consistency check")); err != nil {
		t.Fatal(err)
	}

	viaSum := hash.Sum(nil)

	id, idRepool := hash.GetMarklId()
	defer idRepool()
	viaId := id.GetBytes()

	if !bytes.Equal(viaSum, viaId) {
		t.Errorf("Sum(nil) != GetMarklId bytes\n Sum %x\n Id  %x", viaSum, viaId)
	}
}

// GetBlobIdForReader and GetBlobIdForReaderAt do NOT compute digests — they
// read hash.Size() bytes from the reader and treat them as raw id bytes.
// These tests verify that raw-read behaviour.

func TestGetBlobIdForReader_ReadsRawBytes(t *testing.T) {
	input := bytes.Repeat([]byte{0xab}, sha256.Size)

	hash, repool := FormatHashSha256.Get()
	defer repool()

	id, idRepool := hash.GetBlobIdForReader(bytes.NewReader(input))
	defer idRepool()

	if !bytes.Equal(id.GetBytes(), input) {
		t.Errorf("got %x, want %x", id.GetBytes(), input)
	}
}

func TestGetBlobIdForReaderAt_ReadsRawBytesAtOffset(t *testing.T) {
	prefix := bytes.Repeat([]byte{0x00}, 16)
	idBytes := bytes.Repeat([]byte{0xcd}, sha256.Size)
	suffix := bytes.Repeat([]byte{0xff}, 16)

	input := make([]byte, 0, len(prefix)+len(idBytes)+len(suffix))
	input = append(input, prefix...)
	input = append(input, idBytes...)
	input = append(input, suffix...)

	hash, repool := FormatHashSha256.Get()
	defer repool()

	id, idRepool := hash.GetBlobIdForReaderAt(
		bytes.NewReader(input),
		int64(len(prefix)),
	)
	defer idRepool()

	if !bytes.Equal(id.GetBytes(), idBytes) {
		t.Errorf("got %x, want %x", id.GetBytes(), idBytes)
	}
}
