package markl

import (
	"bytes"
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/alfa/blech32"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// blech32Charset duplicates internal/alfa/blech32's unexported
// charsetString to let these tests construct corruptions that stay
// within the charset (a naive XOR-flip on the last char can land
// outside it and surface as a charset-violation error rather than a
// checksum failure).
const blech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

func cycleCharsetByte(c byte) byte {
	idx := strings.IndexByte(blech32Charset, c)
	if idx < 0 {
		return blech32Charset[0]
	}
	return blech32Charset[(idx+1)%len(blech32Charset)]
}

// TestUnmarshalText_LegacyCombinedHRPWireForm pins the diagnostic
// behaviour from #168: when a markl-id text body fails the canonical
// split-HRP checksum (RFC 0002 §3.3) but verifies under the legacy
// combined `<purpose>@<format>` HRP form (RFC 0002 §9.1), UnmarshalText
// returns ErrLegacyCombinedHRPWireForm in place of the bare
// blech32.ErrInvalidChecksum so callers can distinguish a pre-v0.3.16
// file from genuine corruption.
func TestUnmarshalText_LegacyCombinedHRPWireForm(t *testing.T) {
	const (
		purpose = testPurposePub
		format  = FormatIdEd25519Pub
	)

	combinedHRP := purpose + "@" + format
	payload := bytes.Repeat([]byte{0xAB}, 32)

	legacyEncoded, err := blech32.Encode(combinedHRP, payload)
	if err != nil {
		t.Fatalf("blech32.Encode(%q, ...): %v", combinedHRP, err)
	}

	var id Id
	err = id.UnmarshalText(legacyEncoded)
	if err == nil {
		t.Fatalf(
			"UnmarshalText(%q) should error, got nil",
			string(legacyEncoded),
		)
	}

	var legacy ErrLegacyCombinedHRPWireForm
	if !errors.As(err, &legacy) {
		t.Fatalf(
			"expected ErrLegacyCombinedHRPWireForm, got %T: %v",
			err, err,
		)
	}

	if legacy.Purpose != purpose {
		t.Errorf("Purpose: got %q, want %q", legacy.Purpose, purpose)
	}

	if legacy.Raw != string(legacyEncoded) {
		t.Errorf("Raw: got %q, want %q", legacy.Raw, string(legacyEncoded))
	}

	if legacy.FormatId != format {
		t.Errorf("FormatId: got %q, want %q", legacy.FormatId, format)
	}

	if !bytes.Equal(legacy.Data, payload) {
		t.Errorf("Data: got %x, want %x", legacy.Data, payload)
	}

	if len(legacy.SplitHRPChecksum) != 6 {
		t.Errorf(
			"SplitHRPChecksum: got %q (len %d), want 6 chars",
			legacy.SplitHRPChecksum, len(legacy.SplitHRPChecksum),
		)
	}

	// Splice path: replace the last 6 chars of the legacy body
	// section (post-`@`) with SplitHRPChecksum and re-parse. The
	// result must round-trip cleanly under the canonical
	// UnmarshalText.
	at := strings.IndexByte(legacy.Raw, '@')
	if at < 0 {
		t.Fatalf("expected `@` in legacy.Raw: %q", legacy.Raw)
	}
	prefix := legacy.Raw[:at+1]
	bodySection := legacy.Raw[at+1:]
	if len(bodySection) < 6 {
		t.Fatalf("body section too short: %q", bodySection)
	}
	spliced := prefix + bodySection[:len(bodySection)-6] + legacy.SplitHRPChecksum

	var splicedId Id
	if err := splicedId.UnmarshalText([]byte(spliced)); err != nil {
		t.Fatalf("splice round-trip UnmarshalText(%q): %v", spliced, err)
	}

	if splicedId.GetPurposeId() != purpose {
		t.Errorf(
			"splice round-trip purpose: got %q, want %q",
			splicedId.GetPurposeId(), purpose,
		)
	}

	// Programmatic path: build a fresh Id from FormatId+Data,
	// re-marshal, re-parse, assert the recovered Id matches.
	var programmaticId Id
	if err := programmaticId.SetPurposeId(legacy.Purpose); err != nil {
		t.Fatalf("SetPurposeId(%q): %v", legacy.Purpose, err)
	}
	if err := programmaticId.SetMarklId(legacy.FormatId, legacy.Data); err != nil {
		t.Fatalf(
			"SetMarklId(%q, %x): %v",
			legacy.FormatId, legacy.Data, err,
		)
	}

	canonical, err := programmaticId.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}

	var reparsed Id
	if err := reparsed.UnmarshalText(canonical); err != nil {
		t.Fatalf(
			"programmatic round-trip UnmarshalText(%q): %v",
			string(canonical), err,
		)
	}

	if reparsed.GetPurposeId() != purpose {
		t.Errorf(
			"programmatic round-trip purpose: got %q, want %q",
			reparsed.GetPurposeId(), purpose,
		)
	}

	if !Equals(programmaticId, reparsed) {
		t.Errorf(
			"programmatic round-trip: ids differ before/after re-parse",
		)
	}
}

// TestUnmarshalText_GenuineCorruptionStaysInvalidChecksum pins that
// flipping a bit in a properly-encoded markl-id body still surfaces as
// blech32.ErrInvalidChecksum (not the legacy-form error). This is the
// negative half of #168: the diagnostic must not silently accept
// arbitrary bit-flipped inputs as "legacy" — only inputs that actually
// verify under the combined-HRP HRP shape.
func TestUnmarshalText_GenuineCorruptionStaysInvalidChecksum(t *testing.T) {
	const (
		purpose = "test-purpose-v1"
		format  = FormatIdHashBlake2b256
	)

	payload := bytes.Repeat([]byte{0xCD}, 32)

	body, err := blech32.Encode(format, payload)
	if err != nil {
		t.Fatalf("blech32.Encode(%q, ...): %v", format, err)
	}

	encoded := append([]byte(purpose+"@"), body...)

	// Flip the last data character to break the checksum without
	// constructing anything that would verify under any HRP shape.
	encoded[len(encoded)-1] = cycleCharsetByte(encoded[len(encoded)-1])

	var id Id
	err = id.UnmarshalText(encoded)
	if err == nil {
		t.Fatalf(
			"UnmarshalText(%q) should error, got nil",
			string(encoded),
		)
	}

	if !errors.Is(err, blech32.ErrInvalidChecksum) {
		t.Errorf(
			"expected blech32.ErrInvalidChecksum, got: %v",
			err,
		)
	}

	var legacy ErrLegacyCombinedHRPWireForm
	if errors.As(err, &legacy) {
		t.Errorf(
			"corrupted input must not surface as ErrLegacyCombinedHRPWireForm: %v",
			err,
		)
	}
}

// TestUnmarshalText_GenuineCorruptionWithoutPurpose pins that the
// legacy-form re-verification only triggers when the input carries a
// `<purpose>@` prefix. A purposeless input with a checksum failure must
// surface as plain ErrInvalidChecksum; the combined-HRP form has no
// meaning without a purpose.
func TestUnmarshalText_GenuineCorruptionWithoutPurpose(t *testing.T) {
	const format = FormatIdHashBlake2b256

	payload := bytes.Repeat([]byte{0xEF}, 32)

	encoded, err := blech32.Encode(format, payload)
	if err != nil {
		t.Fatalf("blech32.Encode(%q, ...): %v", format, err)
	}

	encoded[len(encoded)-1] = cycleCharsetByte(encoded[len(encoded)-1])

	var id Id
	err = id.UnmarshalText(encoded)
	if err == nil {
		t.Fatalf(
			"UnmarshalText(%q) should error, got nil",
			string(encoded),
		)
	}

	if !errors.Is(err, blech32.ErrInvalidChecksum) {
		t.Errorf(
			"expected blech32.ErrInvalidChecksum, got: %v",
			err,
		)
	}

	var legacy ErrLegacyCombinedHRPWireForm
	if errors.As(err, &legacy) {
		t.Errorf(
			"purposeless input must not surface as ErrLegacyCombinedHRPWireForm: %v",
			err,
		)
	}
}
