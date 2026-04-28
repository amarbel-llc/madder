package tree_capture_receipt

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strconv"
	"strings"

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

// ReadV1 parses a hyphence-wrapped v1 receipt from r. The receipt's
// type-id MUST match TypeTagV1; an unknown type-id is an error rather
// than silent fallback (callers needing dispatch should peek the
// metadata via the dispatcher in store.go).
func ReadV1(r io.Reader) (V1, error) {
	var v1 V1

	mr := &v1MetadataReader{}
	br := &v1BodyReader{}

	hr := hyphence.Reader{
		RequireMetadata: true,
		Metadata:        mr,
		Blob:            br,
	}

	if _, err := hr.ReadFrom(r); err != nil {
		return v1, errors.Wrap(err)
	}

	if mr.typeTag != TypeTagV1 {
		return v1, errors.ErrorWithStackf(
			"tree_capture_receipt: expected type-tag %q, got %q",
			TypeTagV1, mr.typeTag,
		)
	}

	v1.Hint = mr.hint
	v1.Entries = br.entries

	return v1, nil
}

// v1MetadataReader implements io.ReaderFrom for the metadata block
// piped from hyphence.Reader. Recognizes the `! <type>` line and the
// optional `- store/<id> < <markl-id>` line.
type v1MetadataReader struct {
	typeTag string
	hint    *StoreHint
}

func (mr *v1MetadataReader) ReadFrom(r io.Reader) (int64, error) {
	var total int64
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Text()
		total += int64(len(line) + 1)

		switch {
		case strings.HasPrefix(line, "! "):
			mr.typeTag = strings.TrimPrefix(line, "! ")

		case strings.HasPrefix(line, "- store/"):
			rest := strings.TrimPrefix(line, "- store/")
			sep := " < "
			i := strings.Index(rest, sep)
			if i < 0 {
				return total, errors.ErrorWithStackf(
					"tree_capture_receipt: malformed store-hint line %q", line,
				)
			}
			mr.hint = &StoreHint{
				StoreId:       rest[:i],
				ConfigMarklId: rest[i+len(sep):],
			}

		default:
			// Per RFC 0003 §Producer Rules and hyphence(7), unrecognized
			// metadata lines MUST be tolerated. Skip silently.
		}
	}

	if err := scanner.Err(); err != nil {
		return total, errors.Wrap(err)
	}

	return total, nil
}

// v1BodyReader implements io.ReaderFrom for the NDJSON body piped
// from hyphence.Reader. Thin wrapper around decodeV1Body so ReadV1
// (single-version path) and the dispatcher in store.go (which buffers
// the body before knowing the version) share decode logic.
type v1BodyReader struct {
	entries []EntryV1
}

func (br *v1BodyReader) ReadFrom(r io.Reader) (int64, error) {
	entries, n, err := decodeV1Body(r)
	br.entries = entries
	return n, err
}

// decodeV1Body parses the NDJSON body of a v1 receipt. Each non-empty
// line is decoded into recordV1 and promoted to EntryV1.
func decodeV1Body(r io.Reader) ([]EntryV1, int64, error) {
	var entries []EntryV1
	var total int64
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Bytes()
		total += int64(len(line) + 1)

		if len(line) == 0 {
			continue
		}

		var rec recordV1
		if err := json.Unmarshal(line, &rec); err != nil {
			return entries, total, errors.Wrapf(err, "tree_capture_receipt: decode body line: %q", line)
		}

		entry, err := decodeV1Record(rec)
		if err != nil {
			return entries, total, errors.Wrap(err)
		}

		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return entries, total, errors.Wrap(err)
	}

	return entries, total, nil
}

func decodeV1Record(rec recordV1) (EntryV1, error) {
	mode, err := strconv.ParseUint(rec.Mode, 8, 32)
	if err != nil {
		return EntryV1{}, errors.Wrapf(err, "tree_capture_receipt: parse mode %q", rec.Mode)
	}

	entry := EntryV1{
		Path: rec.Path,
		Root: rec.Root,
		Type: rec.Type,
		Mode: fs.FileMode(mode),
	}

	switch rec.Type {
	case TypeFile:
		entry.Size = rec.Size
		entry.BlobId = rec.BlobId

	case TypeSymlink:
		entry.Target = rec.Target

	case TypeDir, TypeOther:
		// no extra fields

	default:
		return EntryV1{}, errors.ErrorWithStackf(
			"tree_capture_receipt: unknown entry type %q (path=%q root=%q)",
			rec.Type, rec.Path, rec.Root,
		)
	}

	return entry, nil
}
