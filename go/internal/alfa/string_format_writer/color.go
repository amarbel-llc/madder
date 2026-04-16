package string_format_writer

import (
	"github.com/amarbel-llc/madder/go/internal/0/fields"
)

func colorForType(t fields.Type) string {
	switch t {
	case fields.TypeId:
		return colorBlue
	case fields.TypeHash:
		return colorItalic
	case fields.TypeError:
		return colorRed
	case fields.TypeType:
		return colorYellow
	case fields.TypeUserData:
		return colorCyan
	case fields.TypeHeading:
		return colorRed
	default:
		return colorNone
	}
}
