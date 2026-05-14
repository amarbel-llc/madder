package blob_stores

import (
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// modeConfused is the poison-mode marker set when two mutually
// exclusive mode-selecting methods are called on the same builder
// (e.g. Mirror followed by Read). Build() detects this and returns a
// mode-confusion error via the default branch in its switch.
const modeConfused multiMode = -1

// MultiBuilder constructs a Multi blob store with one of two modes:
// Mirror (broadcast writes across all child stores) or WriteThrough
// (single write store + N read stores). Each mode-selecting method
// (Mirror, WriteTo, ...) sets the mode field; Build() validates and
// returns the configured Multi.
type MultiBuilder struct {
	ctx          interfaces.ActiveContext
	mode         multiMode
	mirrorStores []BlobStoreInitialized
	writeStore   BlobStoreInitialized
	readStores   []BlobStoreInitialized
	readFill     bool
}

// NewMulti starts a builder bound to ctx. readFill defaults to true so
// the WriteThrough path enables tee-during-read (Task 10) unless
// callers opt out via ReadFill(false).
func NewMulti(ctx interfaces.ActiveContext) *MultiBuilder {
	return &MultiBuilder{ctx: ctx, readFill: true}
}

// Mirror configures the builder for broadcast-write mirror mode. If
// any non-Mirror mode-selecting method has already been called, the
// builder enters the confused-mode state and Build() will fail.
func (b *MultiBuilder) Mirror(stores ...BlobStoreInitialized) *MultiBuilder {
	if b.mode == modeUnset || b.mode == modeMirror {
		b.mode = modeMirror
	} else {
		b.mode = modeConfused
	}
	b.mirrorStores = append(b.mirrorStores, stores...)
	return b
}

// WriteTo configures the builder for write-through mode with store as
// the single write target. Multiple WriteTo calls overwrite the prior
// write store; mixing with Mirror enters confused-mode.
func (b *MultiBuilder) WriteTo(store BlobStoreInitialized) *MultiBuilder {
	if b.mode == modeUnset || b.mode == modeWriteThrough {
		b.mode = modeWriteThrough
	} else {
		b.mode = modeConfused
	}
	b.writeStore = store
	return b
}

// Read appends read-only stores for write-through mode. Calling Read
// in any non-write-through context enters confused-mode.
func (b *MultiBuilder) Read(stores ...BlobStoreInitialized) *MultiBuilder {
	if b.mode != modeWriteThrough {
		b.mode = modeConfused
	}
	b.readStores = append(b.readStores, stores...)
	return b
}

// ReadFill toggles tee-during-read for write-through mode. Calling it
// outside write-through (e.g. after Mirror) enters confused-mode.
func (b *MultiBuilder) ReadFill(enabled bool) *MultiBuilder {
	if b.mode != modeWriteThrough {
		b.mode = modeConfused
	}
	b.readFill = enabled
	return b
}

// Build returns the configured Multi. It validates that exactly one
// mode was selected, that the mode's required stores are present, and
// (for write-through) that the write store is not also in the read
// list.
func (b *MultiBuilder) Build() (Multi, error) {
	switch b.mode {
	case modeMirror:
		if len(b.mirrorStores) == 0 {
			return Multi{}, errors.Errorf("Mirror: no stores given")
		}
		return Multi{
			ctx:         b.ctx,
			mode:        modeMirror,
			childStores: b.mirrorStores,
		}, nil

	case modeWriteThrough:
		// A zero-value BlobStoreInitialized has BlobStore == nil; treat
		// that as "no write store given".
		if b.writeStore.BlobStore == nil {
			return Multi{}, errors.Errorf("WriteTo: no write store given")
		}
		for _, r := range b.readStores {
			if sameStore(r, b.writeStore) {
				return Multi{}, errors.Errorf(
					"write store %q also appears in read list",
					storeIdForError(b.writeStore),
				)
			}
		}
		return Multi{
			ctx:        b.ctx,
			mode:       modeWriteThrough,
			writeStore: b.writeStore,
			readStores: b.readStores,
			readFill:   b.readFill,
		}, nil

	case modeUnset:
		return Multi{}, errors.Errorf(
			"MultiBuilder: no mode selected; call Mirror or WriteTo",
		)

	default:
		return Multi{}, errors.Errorf(
			"MultiBuilder: mode confusion; only one of .Mirror or .WriteTo is allowed",
		)
	}
}

// sameStore reports whether two BlobStoreInitialized values refer to
// the same underlying store. Production code uses Path.GetId() as the
// stable key (see blob_stores/main.go's BlobStoreMap construction); we
// prefer that when both paths are set, and fall back to BlobStore
// interface-value equality otherwise. The fallback keeps the
// comparison meaningful for test fixtures that don't populate Path.
func sameStore(a, b BlobStoreInitialized) bool {
	if a.Path != nil && b.Path != nil {
		return a.Path.GetId().String() == b.Path.GetId().String()
	}
	return a.BlobStore == b.BlobStore
}

// storeIdForError renders a best-effort identifier for use in error
// messages. Falls back to the empty string when no Path is set; the
// error text is still legible because the surrounding context names
// the role (write store / read store).
func storeIdForError(s BlobStoreInitialized) string {
	if s.Path == nil {
		return ""
	}
	return s.Path.GetId().String()
}
