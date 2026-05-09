package markl

import (
	"fmt"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/alfa/blech32"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

func (id Id) MarshalText() (bites []byte, err error) {
	if id.format == nil {
		return bites, err
	}

	var hrp string

	if prupose := id.GetPurposeId(); prupose != "" {
		hrp = fmt.Sprintf(
			"%s@%s",
			id.GetPurposeId(),
			id.format.GetMarklFormatId(),
		)
	} else {
		hrp = id.format.GetMarklFormatId()
	}

	if bites, err = blech32.Encode(hrp, id.data); err != nil {
		err = errors.Wrap(err)
		return bites, err
	}

	return bites, err
}

func (id *Id) UnmarshalText(bites []byte) (err error) {
	if len(bites) == 0 {
		id.Reset()
		return err
	}

	var purposeAndFormatId string
	var data []byte

	if purposeAndFormatId, data, err = blech32.Decode(bites); err != nil {
		err = errors.Wrapf(err, "Raw: %q", string(bites))
		return err
	}

	if err = id.applyDecodedHRPAndData(purposeAndFormatId, data); err != nil {
		err = errors.Wrapf(err, "Raw: %q", string(bites))
		return err
	}

	return err
}

// applyDecodedHRPAndData is the post-blech32 half of the RFC 0002 §4
// decode algorithm — shared between UnmarshalText (whose blech32 input
// is []byte) and Set (whose input is string). Splits the HRP on the
// first `@` to extract a purpose, then routes through SetMarklId for
// the format-resolution, (purpose, format) compatibility, and
// payload-size validations.
//
// Both decoders MUST run the blech32 step on the WHOLE input first
// (with the combined HRP) — that is what makes the checksum verify
// against the same bytes MarshalText computed it from.
func (id *Id) applyDecodedHRPAndData(hrp string, data []byte) (err error) {
	purpose, formatId, hasPurpose := strings.Cut(hrp, "@")
	if !hasPurpose {
		formatId = hrp
	}

	if hasPurpose {
		if err = id.SetPurposeId(purpose); err != nil {
			err = errors.Wrap(err)
			return err
		}
	}

	if err = id.SetMarklId(formatId, data); err != nil {
		err = errors.Wrap(err)
		return err
	}

	return err
}
