package tree_capture_receipt

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// HeaderV1 is the literal byte sequence that precedes the NDJSON body
// in every v1 receipt when no StoreHint is present. Derived from the
// hyphence boundary + type-tag line; exposed for tests that strip it
// to inspect just the body.
const HeaderV1 = hyphence.Boundary + "\n" +
	"! " + TypeTagV1 + "\n" +
	hyphence.Boundary + "\n\n"

// WriteV1 serializes entries as a hyphence-wrapped v1 receipt to w.
// Equivalent to WriteV1WithHint(w, entries, nil).
func WriteV1(w io.Writer, entries []EntryV1) (int64, error) {
	return WriteV1WithHint(w, entries, nil)
}

// WriteV1WithHint serializes entries as a hyphence-wrapped v1 receipt
// to w, optionally prefixing a store-hint metadata line per RFC 0003
// §Producer Rules §Receipt Metadata: Store Hint. Entries are sorted in
// place by (Root, Path) before encoding so two captures of the same
// tree produce byte-identical output. Returns the number of bytes
// written.
func WriteV1WithHint(w io.Writer, entries []EntryV1, hint *StoreHint) (int64, error) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Root != entries[j].Root {
			return entries[i].Root < entries[j].Root
		}
		return entries[i].Path < entries[j].Path
	})

	hw := hyphence.Writer{
		Metadata: &v1MetadataWriter{hint: hint},
		Blob:     &v1BodyWriter{entries: entries},
	}

	n, err := hw.WriteTo(w)
	if err != nil {
		return n, errors.Wrap(err)
	}

	return n, nil
}

// v1MetadataWriter emits the receipt's hyphence metadata block: an
// optional store-hint line followed by the type line. hyphence.Writer
// wraps this output in `---\n` boundaries.
type v1MetadataWriter struct {
	hint *StoreHint
}

func (mw *v1MetadataWriter) WriteTo(w io.Writer) (int64, error) {
	var total int64

	if mw.hint != nil {
		n, err := fmt.Fprintf(w, "- store/%s < %s\n", mw.hint.StoreId, mw.hint.ConfigMarklId)
		total += int64(n)
		if err != nil {
			return total, errors.Wrap(err)
		}
	}

	n, err := io.WriteString(w, "! "+TypeTagV1+"\n")
	total += int64(n)
	if err != nil {
		return total, errors.Wrap(err)
	}

	return total, nil
}

// v1BodyWriter emits the NDJSON body of a v1 receipt. WriteTo stops at
// the first encode/write failure; hyphence.Writer surfaces the error
// to the caller.
type v1BodyWriter struct {
	entries []EntryV1
}

func (bw *v1BodyWriter) WriteTo(w io.Writer) (int64, error) {
	var total int64

	for i := range bw.entries {
		line, err := encodeV1Entry(bw.entries[i])
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

func encodeV1Entry(e EntryV1) ([]byte, error) {
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
