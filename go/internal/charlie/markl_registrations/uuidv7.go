package markl_registrations

import (
	"crypto/rand"
	"io"
	"time"

	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

// FormatIdUuidv7 is madder's own markl format for a blob store's instance
// identity (FDR-0010): a UUIDv7 (RFC 9562) carried as a 16-byte markl-id
// payload, rendered `uuidv7-<blech32>`. This is madder's FIRST self-defined
// markl format — every other format lives upstream in piggy (madder#255) —
// so it is registered here at madder's own site (AllFormats, see main.go's
// init). It activates for external in-process consumers via the store-config
// funnel per madder#278.
const FormatIdUuidv7 = "uuidv7"

// uuidv7ByteLen is the fixed UUID payload size (RFC 9562 §4: 128 bits).
const uuidv7ByteLen = 16

// FormatUuidv7 is the bare (non-hash) markl format for the uuidv7 instance
// id — just an id and a fixed 16-byte size. Registered in the package init
// via AllFormats.
var FormatUuidv7 = markl.Format{
	Id:   FormatIdUuidv7,
	Size: uuidv7ByteLen,
}

// layoutUuidv7 writes a RFC 9562 §5.7 UUIDv7 into dst: a 48-bit big-endian
// Unix-millisecond timestamp (dst[0:6]), then rand_a/rand_b from randSource
// (dst[6:16]), with the 4-bit version (7, high nibble of dst[6]) and 2-bit
// variant (0b10, top bits of dst[8]) overwritten last. The clock and
// randomness are injected so RFC 9562 §A.6's published vector can be
// reproduced exactly in tests.
func layoutUuidv7(now time.Time, randSource io.Reader, dst *[uuidv7ByteLen]byte) (err error) {
	// unix_ts_ms: 48 bits, big-endian.
	ms := uint64(now.UnixMilli())
	dst[0] = byte(ms >> 40)
	dst[1] = byte(ms >> 32)
	dst[2] = byte(ms >> 24)
	dst[3] = byte(ms >> 16)
	dst[4] = byte(ms >> 8)
	dst[5] = byte(ms)

	// rand_a (12 bits) + rand_b (62 bits) fill dst[6:16].
	if _, err = io.ReadFull(randSource, dst[6:]); err != nil {
		err = errors.Wrap(err)
		return err
	}

	// ver = 0x7 in the high nibble of dst[6]; var = 0b10 in the top two bits
	// of dst[8]. Overwriting these is idempotent on already-conformant bytes,
	// which is what lets the §A.6 vector test feed the published final bytes.
	dst[6] = (dst[6] & 0x0f) | 0x70
	dst[8] = (dst[8] & 0x3f) | 0x80

	return err
}

// MintInstanceId mints a fresh UUIDv7 instance identity as a markl.Id of
// format uuidv7. It is called ONCE when a blob store is created and never on
// read (FDR-0010's no-lazy-mint rule). Uses the wall clock + crypto/rand.
func MintInstanceId() (id markl.Id, err error) {
	var dst [uuidv7ByteLen]byte

	if err = layoutUuidv7(time.Now(), rand.Reader, &dst); err != nil {
		err = errors.Wrap(err)
		return id, err
	}

	if err = id.SetMarklId(FormatIdUuidv7, dst[:]); err != nil {
		err = errors.Wrap(err)
		return id, err
	}

	return id, err
}
