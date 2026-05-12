package markl

import (
	"bytes"
	"fmt"

	"github.com/amarbel-llc/madder/go/internal/alfa/blech32"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// MarshalText writes the RFC 0002 §3 wire form
// `[purpose@]<blech32(format, data)>`. The blech32 checksum binds to
// (format, data) only; the purpose, when present, is prepended
// textually after blech32 encoding so the same digest under different
// purposes shares a byte-identical blech32 body.
func (id Id) MarshalText() (bites []byte, err error) {
	if id.format == nil {
		return bites, err
	}

	if bites, err = blech32.Encode(id.format.GetMarklFormatId(), id.data); err != nil {
		err = errors.Wrap(err)
		return bites, err
	}

	if purpose := id.GetPurposeId(); purpose != "" {
		bites = []byte(fmt.Sprintf("%s@%s", purpose, string(bites)))
	}

	return bites, err
}

// UnmarshalText parses the RFC 0002 §4 wire form. The purpose, when
// present, is split off textually before blech32 decoding so the
// checksum verifies against HRP=format only.
func (id *Id) UnmarshalText(bites []byte) (err error) {
	if len(bites) == 0 {
		id.Reset()
		return err
	}

	body := bites

	if at := bytes.IndexByte(bites, '@'); at >= 0 {
		purpose := string(bites[:at])
		body = bites[at+1:]

		if err = id.SetPurposeId(purpose); err != nil {
			err = errors.Wrapf(err, "Raw: %q", string(bites))
			return err
		}
	}

	var formatId string
	var data []byte

	if formatId, data, err = blech32.Decode(body); err != nil {
		if purpose := id.GetPurposeId(); purpose != "" &&
			errors.Is(err, blech32.ErrInvalidChecksum) {
			if sep := bytes.LastIndexByte(body, '-'); sep > 0 {
				combinedHRP := purpose + "@" + string(body[:sep])
				if blech32.VerifyChecksumWithHRPOverride(combinedHRP, body) {
					return ErrLegacyCombinedHRPWireForm{
						Purpose: purpose,
						Raw:     string(bites),
					}
				}
			}
		}
		err = errors.Wrapf(err, "Raw: %q", string(bites))
		return err
	}

	if err = id.SetMarklId(formatId, data); err != nil {
		err = errors.Wrapf(err, "Raw: %q", string(bites))
		return err
	}

	return err
}
