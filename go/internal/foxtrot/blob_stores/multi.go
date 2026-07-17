package blob_stores

import (
	"bytes"
	"fmt"
	"io"
	"iter"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_io"
	"github.com/amarbel-llc/piggy/go/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

type multiMode int

const (
	modeUnset multiMode = iota
	modeMirror
	modeWriteThrough
)

type Multi struct {
	ctx         interfaces.ActiveContext
	mode        multiMode
	childStores []BlobStoreInitialized // mirror mode
	writeStore  BlobStoreInitialized   // write-through mode
	readStores  []BlobStoreInitialized // write-through mode
	readFill    bool                   // write-through mode (Task 10 wires the tee)
}

var _ domain_interfaces.BlobStore = Multi{}

func (parentStore Multi) HasBlob(id domain_interfaces.MarklId) bool {
	switch parentStore.mode {
	case modeMirror:
		for _, childStore := range parentStore.childStores {
			if childStore.HasBlob(id) {
				return true
			}
		}
		return false

	case modeWriteThrough:
		if parentStore.writeStore.HasBlob(id) {
			return true
		}
		for _, readStore := range parentStore.readStores {
			if readStore.HasBlob(id) {
				return true
			}
		}
		return false
	}

	// unreachable: Build() rejects any mode outside the switch arms above.
	return false
}

func (parentStore Multi) MakeBlobReader(
	id domain_interfaces.MarklId,
) (domain_interfaces.BlobReader, error) {
	switch parentStore.mode {
	case modeMirror:
		for _, childStore := range parentStore.childStores {
			if !childStore.HasBlob(id) {
				continue
			}
			reader, err := childStore.MakeBlobReader(id)
			if err != nil {
				// #209: a child that hands back an unavailability
				// error after claiming HasBlob (or whose HasBlob
				// itself probe-failed and silently returned false
				// — see TODO below) must not short-circuit the
				// fallback walk. Treat as miss-equivalent and try
				// the next sibling.
				if blob_io.IsBlobStoreUnavailable(err) {
					continue
				}
				return nil, err
			}
			return reader, nil
		}

		clonedId, _ := markl.Clone(id) //repool:owned

		return nil, blob_io.ErrBlobMissing{
			BlobId: clonedId,
		}

	case modeWriteThrough:
		if parentStore.writeStore.HasBlob(id) {
			reader, err := parentStore.writeStore.MakeBlobReader(id)
			if err != nil {
				// Same #209 treatment for the write store: if its
				// MakeBlobReader fails because the backend is
				// unreachable, fall through to read sources rather
				// than hard-failing the read.
				if !blob_io.IsBlobStoreUnavailable(err) {
					return nil, err
				}
			} else {
				return reader, nil
			}
		}
		for _, readStore := range parentStore.readStores {
			if !readStore.HasBlob(id) {
				continue
			}
			reader, err := readStore.MakeBlobReader(id)
			if err != nil {
				if blob_io.IsBlobStoreUnavailable(err) {
					continue
				}
				return nil, err
			}
			if !parentStore.readFill {
				return reader, nil
			}
			writer, werr := parentStore.writeStore.MakeBlobWriter(
				parentStore.writeStore.GetDefaultHashType(),
			)
			if werr != nil {
				return reader, nil
			}
			return newTeeBlobReader(
				parentStore.ctx,
				reader,
				writer,
				id,
				parentStore.writeStore.BlobStore,
			), nil
		}

		clonedId, _ := markl.Clone(id) //repool:owned
		return nil, blob_io.ErrBlobMissing{
			BlobId: clonedId,
		}
	}

	// unreachable: Build() rejects any mode outside the switch arms above.
	return nil, errors.Errorf("Multi: unknown mode %d", parentStore.mode)
}

func (parentStore Multi) MakeBlobWriter(
	marklHashType domain_interfaces.FormatHash,
) (domain_interfaces.BlobWriter, error) {
	switch parentStore.mode {
	case modeMirror:
		writers := make([]io.Writer, len(parentStore.childStores))

		multiWriter := multiStoreBlobWriter{
			blobWriters: make(
				[]domain_interfaces.BlobWriter,
				len(parentStore.childStores),
			),
		}

		for i, childStore := range parentStore.childStores {
			var err error

			if multiWriter.blobWriters[i], err = childStore.MakeBlobWriter(
				marklHashType,
			); err != nil {
				err = errors.Wrap(err)
				return nil, err
			}

			writers[i] = multiWriter.blobWriters[i]
		}

		multiWriter.Writer = io.MultiWriter(writers...)

		return multiWriter, nil

	case modeWriteThrough:
		return parentStore.writeStore.MakeBlobWriter(marklHashType)
	}

	// unreachable: Build() rejects any mode outside the switch arms above.
	return nil, errors.Errorf("Multi: unknown mode %d", parentStore.mode)
}

// GetBlobStoreDescription synthesizes a description that reflects the
// wrapper's mode and the identities of its children. Mirror produces
// "multi/mirror(<descA>,<descB>,...)"; WriteThrough names the write
// store as W= and each read source as R=, in source order, e.g.
// "multi/write-through(W=local, R=remoteA, R=remoteB)".
func (parentStore Multi) GetBlobStoreDescription() string {
	switch parentStore.mode {
	case modeMirror:
		ids := make([]string, 0, len(parentStore.childStores))
		for _, childStore := range parentStore.childStores {
			ids = append(ids, childStore.GetBlobStoreDescription())
		}
		return fmt.Sprintf("multi/mirror(%s)", strings.Join(ids, ","))

	case modeWriteThrough:
		parts := make([]string, 0, 1+len(parentStore.readStores))
		parts = append(
			parts,
			fmt.Sprintf("W=%s", parentStore.writeStore.GetBlobStoreDescription()),
		)
		for _, readStore := range parentStore.readStores {
			parts = append(
				parts,
				fmt.Sprintf("R=%s", readStore.GetBlobStoreDescription()),
			)
		}
		return fmt.Sprintf("multi/write-through(%s)", strings.Join(parts, ", "))
	}

	// unreachable: Build() rejects any mode outside the switch arms above.
	return ""
}

// GetDefaultHashType reports the wrapper's default hash type by
// delegating to the first child in Mirror mode (the canonical write
// hash for the mirror set) and to the single write store in
// WriteThrough mode.
func (parentStore Multi) GetDefaultHashType() domain_interfaces.FormatHash {
	switch parentStore.mode {
	case modeMirror:
		// unreachable empty-Mirror guard: Build() rejects empty Mirror
		// via "Mirror: no stores given".
		if len(parentStore.childStores) == 0 {
			return nil
		}
		return parentStore.childStores[0].GetDefaultHashType()

	case modeWriteThrough:
		return parentStore.writeStore.GetDefaultHashType()
	}

	// unreachable: Build() rejects any mode outside the switch arms above.
	return nil
}

// GetBlobStoreConfig delegates to the first child in Mirror mode and
// to the write store in WriteThrough mode. The wrapper has no config
// of its own — Multi is purely an orchestration layer.
func (parentStore Multi) GetBlobStoreConfig() domain_interfaces.BlobStoreConfig {
	switch parentStore.mode {
	case modeMirror:
		// unreachable empty-Mirror guard: Build() rejects empty Mirror.
		if len(parentStore.childStores) == 0 {
			return nil
		}
		return parentStore.childStores[0].GetBlobStoreConfig()

	case modeWriteThrough:
		return parentStore.writeStore.GetBlobStoreConfig()
	}

	// unreachable: Build() rejects any mode outside the switch arms above.
	return nil
}

// GetBlobIOWrapper delegates to the first child in Mirror mode and to
// the write store in WriteThrough mode. As with GetBlobStoreConfig,
// the wrapper itself has no IO-wrapper state.
func (parentStore Multi) GetBlobIOWrapper() domain_interfaces.BlobIOWrapper {
	switch parentStore.mode {
	case modeMirror:
		// unreachable empty-Mirror guard: Build() rejects empty Mirror.
		if len(parentStore.childStores) == 0 {
			return nil
		}
		return parentStore.childStores[0].GetBlobIOWrapper()

	case modeWriteThrough:
		return parentStore.writeStore.GetBlobIOWrapper()
	}

	// unreachable: Build() rejects any mode outside the switch arms above.
	return nil
}

// AllBlobs N-way merges its children's ordered AllBlobs sequences.
// Each child satisfies the BlobStore contract: ids arrive in ascending
// lex byte order of the MarklId raw bytes within a given hash format.
// At every step the merge picks the lexicographic minimum across all
// live heads (by format-id then raw bytes) and yields it once; every
// head that compares equal to the minimum is advanced, which dedupes
// same-hash ids that appear in multiple children.
//
// Cross-hash digests have distinct format-ids and therefore never
// compare as equal; they pass through as separate entries. Errors
// surface as nil id + non-nil err pairs, matching the SeqError
// contract; an erroring head is advanced past the error and the merge
// continues.
func (parentStore Multi) AllBlobs() interfaces.SeqError[domain_interfaces.MarklId] {
	sources := parentStore.allBlobSources()

	return func(yield func(domain_interfaces.MarklId, error) bool) {
		type head struct {
			id   domain_interfaces.MarklId
			err  error
			ok   bool
			next func() (domain_interfaces.MarklId, error, bool)
			stop func()
		}

		heads := make([]head, len(sources))

		for i, source := range sources {
			next, stop := iter.Pull2(source.AllBlobs())
			id, err, ok := next()
			heads[i] = head{
				id:   id,
				err:  err,
				ok:   ok,
				next: next,
				stop: stop,
			}
		}

		defer func() {
			for _, h := range heads {
				h.stop()
			}
		}()

		for {
			// Surface any pending errors first, advancing the head past
			// each error before moving on to id comparison. Also advance
			// past any (nil id, nil err) emission from a misbehaving
			// producer; leaving such a head pinned would block the merge
			// or, worse, surface a nil id to callers (panic on String).
			for i, h := range heads {
				if !h.ok {
					continue
				}

				if h.err != nil {
					if !yield(nil, h.err) {
						return
					}

					id, err, ok := heads[i].next()
					heads[i].id = id
					heads[i].err = err
					heads[i].ok = ok
					continue
				}

				if h.id == nil {
					id, err, ok := heads[i].next()
					heads[i].id = id
					heads[i].err = err
					heads[i].ok = ok
				}
			}

			// Find lexicographic minimum across live error-free heads.
			// The h.id == nil guard is belt-and-suspenders: the pass
			// above advances past nil-id heads, but keep the check here
			// so a future refactor can't accidentally feed nil into
			// compareMarklIds.
			minIdx := -1

			for i, h := range heads {
				if !h.ok || h.err != nil || h.id == nil {
					continue
				}

				if minIdx == -1 || compareMarklIds(h.id, heads[minIdx].id) < 0 {
					minIdx = i
				}
			}

			if minIdx == -1 {
				return
			}

			minId := heads[minIdx].id

			if !yield(minId, nil) {
				return
			}

			// Advance every head matching the minimum so same-hash
			// duplicates collapse to a single yield. Same nil-id
			// defensive guard as the min-finder loop above.
			for i, h := range heads {
				if !h.ok || h.err != nil || h.id == nil {
					continue
				}

				if compareMarklIds(h.id, minId) == 0 {
					id, err, ok := heads[i].next()
					heads[i].id = id
					heads[i].err = err
					heads[i].ok = ok
				}
			}
		}
	}
}

// compareMarklIds orders MarklIds first by their hash-format id and
// then by their raw bytes. Two ids that share both format and bytes
// compare as 0 — the merge in AllBlobs treats those as duplicates and
// collapses them. Cross-hash ids always differ in format-id, so they
// never compare equal and pass through the merge separately.
func compareMarklIds(a, b domain_interfaces.MarklId) int {
	aFormat, bFormat := "", ""

	if af := a.GetMarklFormat(); af != nil {
		aFormat = af.GetMarklFormatId()
	}

	if bf := b.GetMarklFormat(); bf != nil {
		bFormat = bf.GetMarklFormatId()
	}

	if cmp := strings.Compare(aFormat, bFormat); cmp != 0 {
		return cmp
	}

	return bytes.Compare(a.GetBytes(), b.GetBytes())
}

// allBlobSources returns the ordered set of children whose AllBlobs
// sequences participate in the N-way merge. Mirror mode merges every
// child; WriteThrough mode merges the write store followed by the read
// stores.
func (parentStore Multi) allBlobSources() []BlobStoreInitialized {
	switch parentStore.mode {
	case modeMirror:
		return parentStore.childStores

	case modeWriteThrough:
		sources := make(
			[]BlobStoreInitialized,
			0,
			1+len(parentStore.readStores),
		)
		sources = append(sources, parentStore.writeStore)
		sources = append(sources, parentStore.readStores...)
		return sources
	}

	// unreachable: Build() rejects any mode outside the switch arms above.
	return nil
}

type multiStoreBlobWriter struct {
	io.Writer
	blobWriters []domain_interfaces.BlobWriter
}

var _ domain_interfaces.BlobWriter = multiStoreBlobWriter{}

func (parentWriter multiStoreBlobWriter) ReadFrom(
	reader io.Reader,
) (n int64, err error) {
	if n, err = io.Copy(parentWriter.Writer, reader); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	return n, err
}

func (parentWriter multiStoreBlobWriter) Close() error {
	for _, childWriter := range parentWriter.blobWriters {
		if err := childWriter.Close(); err != nil {
			err = errors.Wrap(err)
			return err
		}
	}

	return nil
}

func (parentWriter multiStoreBlobWriter) GetMarklId() (first domain_interfaces.MarklId) {
	for _, childWriter := range parentWriter.blobWriters {
		next := childWriter.GetMarklId()

		if first == nil {
			first = next
		} else if err := markl.AssertEqual(first, next); err != nil {
			// unreachable in normal use: every child consumed the same
			// bytes via io.MultiWriter and was created with the same
			// hash type, so their MarklIds must agree. A mismatch is a
			// contract violation we'd rather crash on than silently
			// hide.
			panic(err)
		}
	}

	return first
}
