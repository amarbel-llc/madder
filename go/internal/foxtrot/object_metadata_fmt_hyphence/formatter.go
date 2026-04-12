package object_metadata_fmt_hyphence

import (
	"io"

	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ohio"
)

type formatter []funcWrite

func (formatter formatter) FormatMetadata(
	writer io.Writer,
	formatterContext FormatterContext,
) (n int64, err error) {
	return ohio.WriteSeq(writer, formatterContext, formatter...)
}
