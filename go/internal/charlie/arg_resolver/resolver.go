// Package arg_resolver centralizes how madder commands classify positional
// arguments as files, blob IDs, or blob-store-id switches.
//
// Each command declares the set of Modes it accepts via a bitmask. Resolve
// tries each mode in a fixed precedence:
//
//  1. ModeBlobId — unambiguous (markl IDs carry a hash-type prefix),
//     tried first when set.
//  2. ModeFile — filesystem open; a NotExist is recoverable and falls
//     through to the next mode, but any other filesystem error
//     short-circuits as KindError.
//  3. ModeStoreSwitch — tried last because unprefixed blob-store-id
//     names can collide with file paths in CWD.
//
// Ingest commands (write, pack-blobs) enable ModeFile | ModeStoreSwitch
// and get file-first semantics; read commands (cat, has) enable
// ModeBlobId | ModeStoreSwitch and get blob-id-first semantics. The
// asymmetry is intentional — see blob-store(7).
//
// An argument that matches no enabled mode returns KindError with a
// diagnostic that lists which modes were tried; the caller renders it.
//
// DetectShadow reports when a file-resolved arg shares a bare name with
// a configured blob-store-id — the warning that tells the caller "your
// file shadows a blob-store-id; use './foo' or the prefixed form to
// disambiguate." Any command with both ModeFile and ModeStoreSwitch
// should call DetectShadow on a KindFile result.
package arg_resolver

//go:generate dagnabit export

import (
	"fmt"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_io"
	"github.com/amarbel-llc/piggy/go/markl/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
)

// Mode is a bitmask declaring which argument shapes a command accepts.
type Mode uint8

const (
	ModeFile        Mode = 1 << 0
	ModeBlobId      Mode = 1 << 1
	ModeStoreSwitch Mode = 1 << 2
)

// Kind tags the successful resolution (or the error) for a given arg.
type Kind uint8

const (
	KindError Kind = iota
	KindFile
	KindBlobId
	KindStoreSwitch
)

// Resolved carries the outcome of resolving one positional argument.
// Only the field corresponding to Kind is meaningful.
type Resolved struct {
	Kind Kind
	Arg  string

	BlobReader  domain_interfaces.BlobReader // KindFile
	BlobId      markl.Id                     // KindBlobId
	BlobStoreId scoped_id.Id                 // KindStoreSwitch
	Err         error                        // KindError
}

// Resolve classifies arg according to the modes the caller accepts.
// Callers own the BlobReader when Kind == KindFile and must close it.
func Resolve(arg string, mode Mode) Resolved {
	resolved := Resolved{Arg: arg}

	if mode&ModeBlobId != 0 {
		var id markl.Id
		if err := id.Set(arg); err == nil {
			resolved.Kind = KindBlobId
			resolved.BlobId = id
			return resolved
		}
	}

	if mode&ModeFile != 0 {
		reader, err := blob_io.NewFileReaderOrErrNotExist(
			blob_io.DefaultConfig,
			arg,
		)
		if err == nil {
			resolved.Kind = KindFile
			resolved.BlobReader = reader
			return resolved
		} else if !errors.IsNotExist(err) {
			resolved.Kind = KindError
			resolved.Err = err
			return resolved
		}
		// NotExist falls through to the remaining modes.
	}

	if mode&ModeStoreSwitch != 0 {
		var id scoped_id.Id
		if err := id.Set(arg); err == nil {
			resolved.Kind = KindStoreSwitch
			resolved.BlobStoreId = id
			return resolved
		}
	}

	resolved.Kind = KindError
	resolved.Err = unresolvedError(arg, mode)
	return resolved
}

// DetectShadow reports whether arg, when it resolves to a file in CWD,
// shares a bare name with one of the candidate blob-store-ids. Prefixed
// names never shadow (the prefix bypasses the filesystem probe in
// scoped_id.Id.Set). Returns the shadowed id and true on a hit.
//
// Callers should invoke this only when both ModeFile and ModeStoreSwitch
// are accepted — it has no meaning otherwise.
func DetectShadow(arg string, candidates []scoped_id.Id) (shadowed scoped_id.Id, ok bool) {
	var asStoreId scoped_id.Id
	if err := asStoreId.Set(arg); err != nil {
		return shadowed, false
	}

	// Prefixed names (/, ., ~, _, %) can't shadow a file in CWD because
	// the blob_store_id parse consumed the prefix; the remainder wouldn't
	// match a bare filesystem lookup.
	if asStoreId.String() != arg {
		return shadowed, false
	}

	for _, id := range candidates {
		if id.GetName() == asStoreId.GetName() {
			return id, true
		}
	}
	return shadowed, false
}

// FormatShadowWarning builds the canonical message for a file arg that
// shadows a configured blob-store-id. Centralized so every caller of
// DetectShadow emits the same phrasing and disambiguation hint. The
// hint renders the id's DisambiguatedString — for an unprefixed (XDG
// user) store the bare name is exactly the argument that just resolved
// to the file, so suggesting it back would be circular (#231); the `~`
// parse-only alias bypasses the filesystem probe.
func FormatShadowWarning(arg string, shadowed scoped_id.Id) string {
	return fmt.Sprintf(
		"warning: %q shadows blob-store-id %q; use './%s' for the file or %q for the blob-store-id",
		arg, shadowed, arg, shadowed.DisambiguatedString(),
	)
}

// FormatStoreSwitchNotice builds the canonical message callers emit when
// a KindStoreSwitch arg rebinds the active store.
func FormatStoreSwitchNotice(id scoped_id.Id) string {
	return fmt.Sprintf("switched to blob-store-id: %s", id)
}

func unresolvedError(arg string, mode Mode) error {
	var tried []string
	if mode&ModeBlobId != 0 {
		tried = append(tried, "blob-id")
	}
	if mode&ModeFile != 0 {
		tried = append(tried, "file path")
	}
	if mode&ModeStoreSwitch != 0 {
		tried = append(tried, "blob-store-id")
	}

	return fmt.Errorf(
		"invalid argument (not a %s): %q",
		strings.Join(tried, " or "),
		arg,
	)
}
