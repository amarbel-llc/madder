package ids

import "github.com/amarbel-llc/purse-first/libs/dewey/delta/catgut"

type Field struct {
	key, value catgut.String
}

func (f *Field) SetCatgutString(v *catgut.String) (err error) {
	return err
}
