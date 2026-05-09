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

	// blech32.Decode reads HRP = "purpose@format" (or just "format")
	// as a unit so the checksum verifies against the same bytes
	// MarshalText computed it from. SetMarklId then runs the RFC 0002
	// §4 validations: GetFormatOrError, (purpose, format)
	// compatibility, and payload-size match.
	var purposeAndFormatId string
	var data []byte

	if purposeAndFormatId, data, err = blech32.Decode(bites); err != nil {
		err = errors.Wrapf(err, "Raw: %q", string(bites))
		return err
	}

	purpose, formatId, hasPurpose := strings.Cut(purposeAndFormatId, "@")
	if !hasPurpose {
		formatId = purposeAndFormatId
	}

	if hasPurpose {
		if err = id.SetPurposeId(purpose); err != nil {
			err = errors.Wrap(err)
			return err
		}
	}

	if err = id.SetMarklId(formatId, data); err != nil {
		err = errors.Wrapf(err, "Raw: %q", string(bites))
		return err
	}

	return err
}
