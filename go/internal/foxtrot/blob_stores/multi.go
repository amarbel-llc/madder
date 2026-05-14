package blob_stores

import (
	"bytes"
	"fmt"
	"io"
	"iter"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_io"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
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
	writeStore  BlobStoreInitialized   // write-through mode (filled in Task 9)
	readStores  []BlobStoreInitialized // write-through mode (filled in Task 9)
	readFill    bool                   // write-through mode (filled in Task 9)
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
		// Task 9 wires write-through HasBlob across writeStore and
		// readStores. Until then, report no blobs to keep the contract
		// honest rather than silently iterating the wrong slice.
		return false
	}

	return false
}

func (parentStore Multi) MakeBlobReader(
	id domain_interfaces.MarklId,
) (domain_interfaces.BlobReader, error) {
	switch parentStore.mode {
	case modeMirror:
		for _, childStore := range parentStore.childStores {
			if childStore.HasBlob(id) {
				return childStore.MakeBlobReader(id)
			}
		}

		clonedId, _ := markl.Clone(id) //repool:owned

		return nil, blob_io.ErrBlobMissing{
			BlobId: clonedId,
		}

	case modeWriteThrough:
		// Task 9 wires write-through reads (writeStore-first then
		// readStores, with optional tee-during-read).
		clonedId, _ := markl.Clone(id) //repool:owned
		return nil, blob_io.ErrBlobMissing{
			BlobId: clonedId,
		}
	}

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
		// Task 9 wires write-through writes to writeStore only.
		return nil, errors.Errorf(
			"Multi: write-through MakeBlobWriter not yet implemented",
		)
	}

	return nil, errors.Errorf("Multi: unknown mode %d", parentStore.mode)
}

// GetBlobStoreDescription synthesizes a description that reflects the
// wrapper's mode and the identities of its children. Mirror produces
// "multi/mirror(<descA>,<descB>,...)"; WriteThrough produces a
// placeholder "multi/write-through(W=<desc>)" until Task 9 finalizes
// the format.
func (parentStore Multi) GetBlobStoreDescription() string {
	switch parentStore.mode {
	case modeMirror:
		ids := make([]string, 0, len(parentStore.childStores))
		for _, childStore := range parentStore.childStores {
			ids = append(ids, childStore.GetBlobStoreDescription())
		}
		return fmt.Sprintf("multi/mirror(%s)", strings.Join(ids, ","))

	case modeWriteThrough:
		// Task 9 finalizes the write-through description; this
		// placeholder keeps the BlobStore interface satisfied so the
		// wrapper is observable in fsck/list output until then.
		return fmt.Sprintf(
			"multi/write-through(W=%s)",
			parentStore.writeStore.GetBlobStoreDescription(),
		)
	}

	return ""
}

// GetDefaultHashType reports the wrapper's default hash type by
// delegating to the first child in Mirror mode (the canonical write
// hash for the mirror set) and to the single write store in
// WriteThrough mode.
func (parentStore Multi) GetDefaultHashType() domain_interfaces.FormatHash {
	switch parentStore.mode {
	case modeMirror:
		if len(parentStore.childStores) == 0 {
			return nil
		}
		return parentStore.childStores[0].GetDefaultHashType()

	case modeWriteThrough:
		return parentStore.writeStore.GetDefaultHashType()
	}

	return nil
}

// GetBlobStoreConfig delegates to the first child in Mirror mode and
// to the write store in WriteThrough mode. The wrapper has no config
// of its own — Multi is purely an orchestration layer.
func (parentStore Multi) GetBlobStoreConfig() domain_interfaces.BlobStoreConfig {
	switch parentStore.mode {
	case modeMirror:
		if len(parentStore.childStores) == 0 {
			return nil
		}
		return parentStore.childStores[0].GetBlobStoreConfig()

	case modeWriteThrough:
		return parentStore.writeStore.GetBlobStoreConfig()
	}

	return nil
}

// GetBlobIOWrapper delegates to the first child in Mirror mode and to
// the write store in WriteThrough mode. As with GetBlobStoreConfig,
// the wrapper itself has no IO-wrapper state.
func (parentStore Multi) GetBlobIOWrapper() domain_interfaces.BlobIOWrapper {
	switch parentStore.mode {
	case modeMirror:
		if len(parentStore.childStores) == 0 {
			return nil
		}
		return parentStore.childStores[0].GetBlobIOWrapper()

	case modeWriteThrough:
		return parentStore.writeStore.GetBlobIOWrapper()
	}

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
// stores (Task 9 wires the write-through paths — until then the slice
// is built from whatever the builder populated).
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
	if n, err = io.Copy(parentWriter, reader); err != nil {
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
			panic(err)
		}
	}

	return first
}
