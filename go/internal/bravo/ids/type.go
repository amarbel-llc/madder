package ids

import (
	"strings"

	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

type typeStruct struct {
	Value string
}

func MustTypeStruct(value string) (tipe typeStruct) {
	if err := tipe.Set(value); err != nil {
		errors.PanicIfError(err)
	}

	return tipe
}

func (id typeStruct) String() string {
	if id.IsEmpty() {
		return ""
	} else {
		return "!" + id.Value
	}
}

func (id typeStruct) StringSansOp() string {
	return id.Value
}

func (id typeStruct) IsEmpty() bool {
	return id.Value == ""
}

func (id *typeStruct) Set(value string) (err error) {
	value = strings.ToLower(strings.TrimSpace(strings.Trim(value, ".! ")))

	if value == "" {
		err = errors.ErrorWithStackf("not a valid Type: empty string")
		return err
	}

	id.Value = value

	return err
}
