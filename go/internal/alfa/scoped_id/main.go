package scoped_id

//go:generate dagnabit export

import (
	"bytes"
	"encoding"
	"fmt"
	"strings"

	"code.linenisgreat.com/madder/go/internal/0/xdg_location_type"
	"github.com/amarbel-llc/piggy/go/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

// Id is a scoped id: a location-prefixed, optionally-named handle to an
// addressable root — a madder blob store or a dodder repo. cwdDepth is a
// runtime CLI-rendering concern,
// only meaningful when location == Cwd: 0 = single-dot prefix (the
// deepest `.<utility>/` ancestor on the walk-up), 1 = `..`, etc. Wire-
// format serialization via MarshalText always emits the canonical
// single-dot form so on-disk refs stay stable across CWDs (#145).
//
// remoteFirst is the FDR-0019 system-scope spelling marker, only
// meaningful when location == XDGSystem: the `/name` spelling parses as
// remote-first (consult the consuming repo's remotes, fall back to the
// system-scoped `name`), while `//name` forces the system scope and
// never matches a remote. madder has no remote transport, so it ignores
// the marker and resolves both spellings to the system scope; dodder
// reads it to drive remote-first resolution. (See blob-store(7).)
type Id struct {
	location    xdg_location_type.Typee
	id          string
	cwdDepth    uint
	remoteFirst bool
	digest      markl.Id // FDR-0008 Phase 2; zero value = no digest
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

// IsRemoteFirst reports the FDR-0019 system-scope spelling: true for the
// single-slash `/name` (remote-first), false for `//name` (forced
// system) and every non-system location. madder ignores this; dodder
// reads it to drive remote-first resolution.
func (id Id) IsRemoteFirst() bool {
	return id.remoteFirst
}

func (id Id) String() string {
	if id.id == "" {
		return ""
	}

	if id.location == xdg_location_type.Cwd {
		return strings.Repeat(".", int(id.cwdDepth)+1) + id.id
	}

	// FDR-0019: system scope is spelled `//name` (forced system); the
	// single-slash `/name` is the remote-first spelling. Both parse to
	// XDGSystem, distinguished only by remoteFirst.
	if id.location == xdg_location_type.XDGSystem {
		if id.remoteFirst {
			return "/" + id.id
		}
		return "//" + id.id
	}

	prefix := id.location.GetPrefix()

	if prefix == 0 {
		return id.id
	}

	return fmt.Sprintf("%c%s", prefix, id.id)
}

// DisambiguatedString returns the form a user can type to address the
// store unambiguously even when a file of the same name exists in CWD
// (#231). XDG-user ids render with the `~` parse-only alias — their
// bare String() is exactly what file-first arg resolution would
// re-route to the file. Every other location's prefixed String()
// already bypasses the filesystem probe and is returned unchanged.
func (id Id) DisambiguatedString() string {
	if id.location == xdg_location_type.XDGUser {
		return "~" + id.String()
	}

	return id.String()
}

// Canonical returns the wire-format form of an Id: same as String for
// non-Cwd locations, and always single-dot for Cwd (depth dropped).
// MarshalText delegates here so on-disk references survive CWD changes.
// FDR-0008 Phase 2: when a digest suffix is set, it is appended as
// `@<markl-id>`. String() stays bare to preserve BlobStoreMap-key call
// sites.
func (id Id) Canonical() string {
	id.cwdDepth = 0
	bare := id.String()
	if id.digest.IsNull() {
		return bare
	}
	return bare + "@" + id.digest.String()
}

func (id *Id) Set(value string) (err error) {
	if len(value) == 0 {
		err = errors.Errorf("empty scoped_id")
		return err
	}

	// FDR-0008 Phase 2: split on the first `@`. The name charset
	// ([a-zA-Z0-9_-]) excludes `@`, so the first occurrence is
	// unambiguously the digest separator.
	left, digestText, hasDigest := strings.Cut(value, "@")
	if hasDigest {
		if len(left) == 0 {
			err = errors.Errorf(
				"scoped_id is empty before `@`: %q", value,
			)
			return err
		}
		if err = id.digest.Set(digestText); err != nil {
			err = errors.Wrapf(err,
				"scoped_id digest: %q", digestText)
			return err
		}
		value = left
	} else {
		id.digest = markl.Id{}
	}

	id.remoteFirst = false

	if value[0] == '.' {
		dots := 0
		for dots < len(value) && value[dots] == '.' {
			dots++
		}

		if dots == len(value) {
			err = errors.Errorf(
				"scoped_id is all dots, no name: %q",
				value,
			)
			return err
		}

		id.location = xdg_location_type.Cwd
		id.cwdDepth = uint(dots - 1)
		id.id = value[dots:]

		return validateName(id.id)
	}

	id.cwdDepth = 0

	// FDR-0019: slash-prefixed ids are system scope. `//name` forces the
	// system scope; `/name` is remote-first (a consuming repo resolves
	// remotes first, falling back to the system-scoped `name`). Bare `/`
	// stays the legacy nameless system selector. `//name` was previously
	// rejected by validateName (the embedded slash failed the charset),
	// so adopting it is purely additive.
	if value[0] == '/' {
		id.location = xdg_location_type.XDGSystem

		if len(value) > 1 && value[1] == '/' {
			id.id = value[2:]

			if id.id == "" {
				err = errors.Errorf(
					"scoped_id is all slashes, no name: %q",
					value,
				)
				return err
			}
		} else {
			id.id = value[1:]

			if id.id != "" {
				id.remoteFirst = true
			}
		}

		return validateName(id.id)
	}

	firstChar := rune(value[0])

	if id.location.IsPrefix(firstChar) {
		id.id = value[1:]

		if err = id.location.SetPrefix(firstChar); err != nil {
			err = errors.Errorf(
				"unsupported first char for scoped_id: %q",
				string(firstChar),
			)

			return err
		}
	} else {
		id.location = xdg_location_type.XDGUser
		id.id = value
	}

	return validateName(id.id)
}

// validateName enforces the documented name charset on parsed ids —
// blob-store(7): "The ID portion after the prefix may contain only
// [a-zA-Z0-9_-]." Without it, a path-shaped value like
// "/home/user/store" parsed as an XDGSystem id whose name carried
// slashes, and init string-joined that name into a nested directory
// tree under the store root (#227). Direct construction via Make /
// MakeWithLocation stays unvalidated: those take trusted, internal
// names (e.g. discovery reading existing directory base names).
func validateName(name string) error {
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_',
			r == '-':
		default:
			return errors.Errorf(
				"scoped_id name may contain only [a-zA-Z0-9_-]; "+
					"got %q in %q",
				string(r),
				name,
			)
		}
	}

	return nil
}

func (id Id) Less(otherId Id) bool {
	if id.location != otherId.location {
		return id.location < otherId.location
	}

	if id.id != otherId.id {
		return id.id < otherId.id
	}

	if id.cwdDepth != otherId.cwdDepth {
		return id.cwdDepth < otherId.cwdDepth
	}

	// FDR-0019: plain system (`//name`, remoteFirst=false) sorts before
	// remote-first (`/name`, remoteFirst=true).
	if id.remoteFirst != otherId.remoteFirst {
		return !id.remoteFirst
	}

	// FDR-0008 Phase 2: digest as the final tie-breaker. Compares
	// the data bytes of the markl.Id lexicographically; null digests
	// sort first.
	return bytes.Compare(id.digest.GetBytes(), otherId.digest.GetBytes()) < 0
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
