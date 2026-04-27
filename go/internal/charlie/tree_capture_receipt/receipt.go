// Package tree_capture_receipt encodes the per-store-group receipt blob
// produced by `madder tree-capture`. The receipt is a hyphence-wrapped
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

	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// TypeTag is the hyphence metadata `! type-string` written at the top of
// every receipt. Bumps to -v2 when the entry schema changes.
const TypeTag = "madder-tree_capture-receipt-v1"

// Header is the literal byte sequence that precedes the NDJSON body in
// every receipt when no StoreHint is present. Derived from the
// hyphence boundary + type-tag line; exposed for tests that strip it
// to inspect just the body.
const Header = hyphence.Boundary + "\n" +
	"! " + TypeTag + "\n" +
	hyphence.Boundary + "\n\n"

// StoreHint is the optional `- store/<id> < <markl-id>` metadata line
// per RFC 0003 §Producer Rules §Receipt Metadata: Store Hint.
// Consumers (tree-restore) use it to auto-resolve the source store.
type StoreHint struct {
	StoreId       string
	ConfigMarklId string
}

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
// Equivalent to WriteWithHint(w, entries, nil).
func Write(w io.Writer, entries []Entry) (int64, error) {
	return WriteWithHint(w, entries, nil)
}

// WriteWithHint serializes entries as a hyphence-wrapped NDJSON
// receipt to w, optionally prefixing a store-hint metadata line per
// RFC 0003 §Producer Rules §Receipt Metadata: Store Hint.
// Entries are sorted in place by (Root, Path) before encoding so two
// captures of the same tree produce byte-identical output. Returns the
// number of bytes written.
func WriteWithHint(w io.Writer, entries []Entry, hint *StoreHint) (int64, error) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Root != entries[j].Root {
			return entries[i].Root < entries[j].Root
		}
		return entries[i].Path < entries[j].Path
	})

	hw := hyphence.Writer{
		Metadata: &metadataWriter{hint: hint},
		Blob:     &bodyWriter{entries: entries},
	}

	n, err := hw.WriteTo(w)
	if err != nil {
		return n, errors.Wrap(err)
	}

	return n, nil
}

// metadataWriter emits the receipt's hyphence metadata block: an
// optional store-hint line followed by the type line. hyphence.Writer
// wraps this output in `---\n` boundaries.
type metadataWriter struct {
	hint *StoreHint
}

func (mw *metadataWriter) WriteTo(w io.Writer) (int64, error) {
	var total int64

	if mw.hint != nil {
		n, err := fmt.Fprintf(w, "- store/%s < %s\n", mw.hint.StoreId, mw.hint.ConfigMarklId)
		total += int64(n)
		if err != nil {
			return total, errors.Wrap(err)
		}
	}

	n, err := io.WriteString(w, "! "+TypeTag+"\n")
	total += int64(n)
	if err != nil {
		return total, errors.Wrap(err)
	}

	return total, nil
}

// bodyWriter emits the NDJSON body of a receipt. WriteTo stops at the
// first encode/write failure; hyphence.Writer surfaces the error to
// the caller.
type bodyWriter struct {
	entries []Entry
}

func (bw *bodyWriter) WriteTo(w io.Writer) (int64, error) {
	var total int64

	for i := range bw.entries {
		line, err := encodeEntry(bw.entries[i])
		if err != nil {
			return total, err
		}

		n, err := w.Write(line)
		total += int64(n)
		if err != nil {
			return total, err
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
