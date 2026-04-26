// Package tree_capture_receipt encodes the per-store-group receipt blob
// produced by `madder capture-tree`. The receipt is a hyphence-wrapped
// NDJSON document: a single metadata line declaring the type tag,
// followed by one JSON object per filesystem entry. Entries are sorted
// by (Root, Path) so equivalent inputs yield byte-identical receipts —
// which means equivalent inputs yield identical receipt blob IDs.
package tree_capture_receipt

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"sort"

	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// TypeTag is the hyphence metadata `! type-string` written at the top of
// every receipt. Bumps to -v2 when the entry schema changes.
const TypeTag = "madder-tree_capture-receipt-v1"

// Header is the literal byte sequence that precedes the NDJSON body in
// every receipt. Exposed so tests can assert on it without re-deriving
// the hyphence boundary form.
const Header = "---\n! " + TypeTag + "\n---\n\n"

// EntryType categorizes a filesystem entry. The string values are the
// canonical wire tags written into the receipt's "type" field.
const (
	TypeFile    = "file"
	TypeDir     = "dir"
	TypeSymlink = "symlink"
	TypeOther   = "other"
)

// Entry is one filesystem entry recorded in a tree-capture receipt.
//
// Mode carries the lstat permission bits (only Mode.Perm() is rendered;
// type bits are dropped). Size and BlobId apply to regular files;
// Target applies to symlinks. The Root/Path split lets a single receipt
// describe entries from multiple capture-roots without path collisions.
type Entry struct {
	Path   string
	Root   string
	Type   string
	Mode   fs.FileMode
	Size   int64
	BlobId string
	Target string
}

// recordV1 is the on-disk JSON shape for one entry in
// madder-tree_capture-receipt-v1. omitempty keeps file-only fields out
// of dir/symlink records and vice versa.
type recordV1 struct {
	Path   string `json:"path"`
	Root   string `json:"root"`
	Type   string `json:"type"`
	Mode   string `json:"mode"`
	Size   int64  `json:"size,omitempty"`
	BlobId string `json:"blob_id,omitempty"`
	Target string `json:"target,omitempty"`
}

// Write serializes entries as a hyphence-wrapped NDJSON receipt to w.
// Entries are sorted in place by (Root, Path) before encoding so two
// captures of the same tree produce byte-identical output. Returns the
// number of bytes written.
func Write(w io.Writer, entries []Entry) (int64, error) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Root != entries[j].Root {
			return entries[i].Root < entries[j].Root
		}
		return entries[i].Path < entries[j].Path
	})

	var total int64

	headerBytes, err := io.WriteString(w, Header)
	if err != nil {
		return total, errors.Wrap(err)
	}
	total += int64(headerBytes)

	for i := range entries {
		line, err := encodeEntry(entries[i])
		if err != nil {
			return total, errors.Wrap(err)
		}

		n, err := w.Write(line)
		total += int64(n)
		if err != nil {
			return total, errors.Wrap(err)
		}
	}

	return total, nil
}

func encodeEntry(e Entry) ([]byte, error) {
	rec := recordV1{
		Path: e.Path,
		Root: e.Root,
		Type: e.Type,
		Mode: fmt.Sprintf("%04o", e.Mode.Perm()),
	}

	switch e.Type {
	case TypeFile:
		rec.Size = e.Size
		rec.BlobId = e.BlobId

	case TypeSymlink:
		rec.Target = e.Target

	case TypeDir, TypeOther:
		// no extra fields

	default:
		return nil, errors.ErrorWithStackf(
			"tree_capture_receipt: unknown entry type %q (path=%q root=%q)",
			e.Type, e.Path, e.Root,
		)
	}

	body, err := json.Marshal(rec)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return append(body, '\n'), nil
}
