package blob_store_id

//go:generate dagnabit export

import (
	"encoding"
	"fmt"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/0/xdg_location_type"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

// Id is a blob-store id. cwdDepth is a runtime CLI-rendering concern,
// only meaningful when location == Cwd: 0 = single-dot prefix (the
// deepest `.<utility>/` ancestor on the walk-up), 1 = `..`, etc. Wire-
// format serialization via MarshalText always emits the canonical
// single-dot form so on-disk refs stay stable across CWDs (#145).
type Id struct {
	location xdg_location_type.Typee
	id       string
	cwdDepth uint
	digest   markl.Id // FDR-0008 Phase 2; zero value = no digest
}

var (
	_ interfaces.Stringer      = Id{}
	_ interfaces.Setter        = &Id{}
	_ encoding.TextMarshaler   = Id{}
	_ encoding.TextUnmarshaler = &Id{}
)

func Make(id string) Id {
	return Id{
		location: xdg_location_type.XDGUser,
		id:       id,
	}
}

func MakeWithLocation(id string, locationType LocationTypeGetter) Id {
	return Id{
		location: locationType.GetLocationType().(xdg_location_type.Typee),
		id:       id,
	}
}

func (id Id) GetName() string {
	return id.id
}

func (id Id) IsEmpty() bool {
	return id.id == ""
}

func (id Id) GetLocationType() LocationType {
	return id.location
}

func (id Id) String() string {
	if id.id == "" {
		return ""
	}

	if id.location == xdg_location_type.Cwd {
		return strings.Repeat(".", int(id.cwdDepth)+1) + id.id
	}

	prefix := id.location.GetPrefix()

	if prefix == 0 {
		return id.id
	}

	return fmt.Sprintf("%c%s", prefix, id.id)
}

// Canonical returns the wire-format form of an Id: same as String for
// non-Cwd locations, and always single-dot for Cwd (depth dropped).
// MarshalText delegates here so on-disk references survive CWD changes.
func (id Id) Canonical() string {
	id.cwdDepth = 0
	return id.String()
}

func (id *Id) Set(value string) (err error) {
	if len(value) == 0 {
		err = errors.Errorf("empty blob_store_id")
		return err
	}

	if value[0] == '.' {
		dots := 0
		for dots < len(value) && value[dots] == '.' {
			dots++
		}

		if dots == len(value) {
			err = errors.Errorf(
				"blob_store_id is all dots, no name: %q",
				value,
			)
			return err
		}

		id.location = xdg_location_type.Cwd
		id.cwdDepth = uint(dots - 1)
		id.id = value[dots:]

		return err
	}

	id.cwdDepth = 0

	firstChar := rune(value[0])

	if id.location.IsPrefix(firstChar) {
		id.id = value[1:]

		if err = id.location.SetPrefix(firstChar); err != nil {
			err = errors.Errorf(
				"unsupported first char for blob_store_id: %q",
				string(firstChar),
			)

			return err
		}
	} else {
		id.location = xdg_location_type.XDGUser
		id.id = value
	}

	return err
}

func (id Id) Less(otherId Id) bool {
	if id.location != otherId.location {
		return id.location < otherId.location
	}

	if id.id != otherId.id {
		return id.id < otherId.id
	}

	return id.cwdDepth < otherId.cwdDepth
}

// WithCwdDepth returns a copy of id with the cwdDepth set. Caller is
// expected to ensure location == Cwd; depth is ignored on render for
// other locations.
func (id Id) WithCwdDepth(depth uint) Id {
	id.cwdDepth = depth
	return id
}

// GetCwdDepth returns the runtime walk-up rank of this id; 0 for
// non-Cwd locations.
func (id Id) GetCwdDepth() uint {
	return id.cwdDepth
}

func (id Id) GetDigest() markl.Id {
	return id.digest
}

func (id Id) HasDigest() bool {
	return !id.digest.IsNull()
}

func (id Id) WithDigest(digest markl.Id) Id {
	id.digest = digest
	return id
}

func (id Id) MarshalText() ([]byte, error) {
	return []byte(id.Canonical()), nil
}

func (id *Id) UnmarshalText(bites []byte) (err error) {
	if err = id.Set(string(bites)); err != nil {
		err = errors.Wrap(err)
		return err
	}

	return err
}
