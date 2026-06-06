package xdg_location_type

//go:generate dagnabit export

import "github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"

type (
	Type interface {
		TypeGetter
		xdgLocationType()
		GetPrefix() rune
	}

	TypeGetter interface {
		GetLocationType() Type
	}

	Typee int
)

const (
	Unknown = Typee(iota)
	Cwd
	XDGUser
	XDGSystem
	XDGCache
)

var (
	_ TypeGetter = Typee(0)
	_ Type       = Typee(0)
)

func (Typee) xdgLocationType() {}

func (t Typee) GetLocationType() Type { return t }

// String renders the user-facing scope name (the vocabulary
// blob-store(7) uses), primarily for error messages like #230's
// unsupported-scope rejection. Hand-written rather than
// stringer-generated so the names read as documentation terms, not Go
// identifiers.
func (t Typee) String() string {
	switch t {
	case Cwd:
		return "CWD"

	case XDGUser:
		return "XDG user"

	case XDGSystem:
		return "XDG system"

	case XDGCache:
		return "XDG cache"

	default:
		return "unknown"
	}
}

func (t *Typee) SetPrefix(firstChar rune) (err error) {
	switch firstChar {
	case '/':
		*t = XDGSystem

	case '~':
		*t = XDGUser

	case '.':
		*t = Cwd

	case '_':
		*t = Unknown

	case '%':
		*t = XDGCache

	default:
		err = errors.Errorf(
			"unsupported rune for location type: %q",
			string(firstChar),
		)

		return err
	}

	return err
}

func (t Typee) IsPrefix(r rune) bool {
	switch r {
	case '/', '~', '.', '_', '%':
		return true

	default:
		return false
	}
}

func (t Typee) GetPrefix() rune {
	switch t {
	case XDGSystem:
		return '/'

	case XDGUser:
		return 0

	case Cwd:
		return '.'

	case Unknown:
		return '_'

	case XDGCache:
		return '%'

	default:
		panic(errors.Errorf("unsupported location type: %d", t))
	}
}
