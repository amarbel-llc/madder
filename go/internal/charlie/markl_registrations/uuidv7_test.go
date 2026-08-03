//go:build test

package markl_registrations

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

// TestLayoutUuidv7_RFC9562_A6Vector reproduces RFC 9562 §A.6's published
// UUIDv7 test vector exactly, validating the deterministic layout (48-bit
// big-endian ms timestamp + version 7 + variant 0b10). The random fields
// (rand_a/rand_b) are not reproducible in general, so the test injects
// §A.6's exact final bytes as the rand source; the version/variant
// overwrite is idempotent on those already-conformant bytes, so the result
// must equal the published value.
func TestLayoutUuidv7_RFC9562_A6Vector(t *testing.T) {
	// RFC 9562 §A.6: unix_ts_ms 1645557742000 (0x017F22E279B0);
	// final 017F22E2-79B0-7CC3-98C4-DC0C0C07398F.
	want, err := hex.DecodeString("017f22e279b07cc398c4dc0c0c07398f")
	if err != nil {
		t.Fatal(err)
	}

	var got [uuidv7ByteLen]byte
	if err := layoutUuidv7(
		time.UnixMilli(1645557742000),
		bytes.NewReader(want[6:]), // the example's rand_a/rand_b bytes
		&got,
	); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got[:], want) {
		t.Errorf(
			"uuidv7 layout = %x, want RFC 9562 §A.6 vector %x",
			got[:],
			want,
		)
	}
}

// TestLayoutUuidv7_TimestampBigEndian pins the 48-bit big-endian ms
// timestamp encoding independent of the random fields.
func TestLayoutUuidv7_TimestampBigEndian(t *testing.T) {
	var got [uuidv7ByteLen]byte
	if err := layoutUuidv7(
		time.UnixMilli(0x017F22E279B0),
		bytes.NewReader(make([]byte, uuidv7ByteLen)), // zero rand
		&got,
	); err != nil {
		t.Fatal(err)
	}

	wantTs := []byte{0x01, 0x7F, 0x22, 0xE2, 0x79, 0xB0}
	if !bytes.Equal(got[0:6], wantTs) {
		t.Errorf("timestamp bytes = %x, want %x", got[0:6], wantTs)
	}
}

// TestMintInstanceId_Properties checks that real mints are well-formed
// uuidv7 markl-ids: format uuidv7, 16 bytes, version nibble 7, variant
// 0b10, non-decreasing timestamps (uuidv7 is time-ordered), and unique.
func TestMintInstanceId_Properties(t *testing.T) {
	seen := make(map[string]bool)
	var prevMs uint64

	for i := 0; i < 200; i++ {
		id, err := MintInstanceId()
		if err != nil {
			t.Fatalf("mint %d: %v", i, err)
		}

		s := id.StringWithFormat()
		if !strings.HasPrefix(s, FormatIdUuidv7+"-") {
			t.Fatalf("mint %d: text %q lacks %q prefix", i, s, FormatIdUuidv7+"-")
		}
		if seen[s] {
			t.Errorf("mint %d: duplicate id %q", i, s)
		}
		seen[s] = true

		b := id.GetBytes()
		if len(b) != uuidv7ByteLen {
			t.Fatalf("mint %d: %d payload bytes, want %d", i, len(b), uuidv7ByteLen)
		}
		if v := b[6] >> 4; v != 7 {
			t.Errorf("mint %d: version nibble %d, want 7", i, v)
		}
		if variant := b[8] >> 6; variant != 0b10 {
			t.Errorf("mint %d: variant bits %02b, want 10", i, variant)
		}

		ms := uint64(b[0])<<40 | uint64(b[1])<<32 | uint64(b[2])<<24 |
			uint64(b[3])<<16 | uint64(b[4])<<8 | uint64(b[5])
		if ms < prevMs {
			t.Errorf(
				"mint %d: timestamp %d < previous %d (uuidv7 must be time-ordered)",
				i, ms, prevMs,
			)
		}
		prevMs = ms
	}
}
