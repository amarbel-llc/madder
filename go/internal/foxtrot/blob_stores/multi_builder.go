package blob_stores

import (
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
)

// MultiBuilder constructs a Multi blob store with one of two modes:
// Mirror (broadcast writes across all child stores) or WriteThrough
// (single write store + N read stores, populated in Task 9). Each
// mode-selecting method (Mirror, WriteTo, ...) sets the mode field;
// Build() validates and returns the configured Multi.
type MultiBuilder struct {
	ctx          interfaces.ActiveContext
	mode         multiMode
	mirrorStores []BlobStoreInitialized
	writeStore   BlobStoreInitialized
	readStores   []BlobStoreInitialized
	readFill     bool
}

// NewMulti starts a builder bound to ctx. readFill defaults to true so
// the Task 9 WriteThrough path enables tee-during-read unless callers
// opt out via a future ReadFillOff method.
func NewMulti(ctx interfaces.ActiveContext) *MultiBuilder {
	return &MultiBuilder{ctx: ctx, readFill: true}
}

// Mirror configures the builder for broadcast-write mirror mode.
func (b *MultiBuilder) Mirror(stores ...BlobStoreInitialized) *MultiBuilder {
	b.mode = modeMirror
	b.mirrorStores = stores
	return b
}

// Build returns the configured Multi. Task 4 adds error paths; Task 9
// adds the modeWriteThrough branch.
func (b *MultiBuilder) Build() (Multi, error) {
	return Multi{
		ctx:         b.ctx,
		mode:        b.mode,
		childStores: b.mirrorStores,
	}, nil
}
