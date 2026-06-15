//go:build test

package blob_stores

import (
	"testing"

	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/madder/go/internal/bravo/directory_layout"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
)

// TestBuilder_Mirror_HappyPath is the Task 3 tracer test: NewMulti(ctx)
// .Mirror(a, b).Build() returns a Multi whose mode is modeMirror and
// whose childStores are the two stubs passed in. Stores are wrapped in
// BlobStoreInitialized because the builder's Mirror() takes that type;
// the embedded BlobStore field is enough — the rest of ConfigNamed is
// zero-valued.
func TestBuilder_Mirror_HappyPath(t *testing.T) {
	ctx := &spyActiveContext{}
	storeA := BlobStoreInitialized{BlobStore: &stubBlobStore{}}
	storeB := BlobStoreInitialized{BlobStore: &stubBlobStore{}}

	m, err := NewMulti(ctx).Mirror(storeA, storeB).Build()
	if err != nil {
		t.Fatalf("Build: unexpected error: %v", err)
	}
	if m.mode != modeMirror {
		t.Fatalf("mode: got %v, want modeMirror", m.mode)
	}
	if len(m.childStores) != 2 {
		t.Fatalf("childStores: got %d, want 2", len(m.childStores))
	}
}

func TestBuilder_Build_EmptyStores(t *testing.T) {
	_, err := NewMulti(&spyActiveContext{}).Build()
	if err == nil {
		t.Fatal("expected error for empty/no-mode-selected; got nil")
	}
}

func TestBuilder_Build_ModeConfusion_MirrorThenRead(t *testing.T) {
	s := BlobStoreInitialized{BlobStore: &stubBlobStore{}}
	_, err := NewMulti(&spyActiveContext{}).Mirror(s).Read(s).Build()
	if err == nil {
		t.Fatal("expected mode-confusion error; got nil")
	}
}

func TestBuilder_Build_ModeConfusion_WriteToThenMirror(t *testing.T) {
	s := BlobStoreInitialized{BlobStore: &stubBlobStore{}}
	_, err := NewMulti(&spyActiveContext{}).WriteTo(s).Mirror(s).Build()
	if err == nil {
		t.Fatal("expected mode-confusion error; got nil")
	}
}

func TestBuilder_Build_WriteStoreInReadList(t *testing.T) {
	s := BlobStoreInitialized{BlobStore: &stubBlobStore{}}
	_, err := NewMulti(&spyActiveContext{}).WriteTo(s).Read(s).Build()
	if err == nil {
		t.Fatal("expected error for write-store-also-in-read-list; got nil")
	}
}

func TestBuilder_Build_ReadFillAfterMirror(t *testing.T) {
	s := BlobStoreInitialized{BlobStore: &stubBlobStore{}}
	_, err := NewMulti(&spyActiveContext{}).Mirror(s).ReadFill(false).Build()
	if err == nil {
		t.Fatal("expected error for ReadFill after Mirror; got nil")
	}
}

// TestBuilder_Build_WriteToTwice_SameStore pins the rule decided in
// #182's carry-forward: calling WriteTo more than once must surface an
// error at Build(), even when the second call passes the same store.
// Before this rule, the second call silently overwrote the prior write
// store with no signal to the caller.
func TestBuilder_Build_WriteToTwice_SameStore(t *testing.T) {
	s := BlobStoreInitialized{BlobStore: &stubBlobStore{}}
	_, err := NewMulti(&spyActiveContext{}).WriteTo(s).WriteTo(s).Build()
	if err == nil {
		t.Fatal("expected error for WriteTo called twice (same store); got nil")
	}
}

// TestBuilder_Build_WriteToTwice_DifferentStores exercises the more
// dangerous variant: a helper pre-configures WriteTo(a), then a caller
// chains WriteTo(b). Silent overwrite would drop a's plumbing without
// warning; Build() must reject the configuration.
func TestBuilder_Build_WriteToTwice_DifferentStores(t *testing.T) {
	a := BlobStoreInitialized{BlobStore: &stubBlobStore{}}
	b := BlobStoreInitialized{BlobStore: &stubBlobStore{}}
	_, err := NewMulti(&spyActiveContext{}).WriteTo(a).WriteTo(b).Build()
	if err == nil {
		t.Fatal("expected error for WriteTo called twice (different stores); got nil")
	}
}

// TestBuilder_Build_MirrorWithNoStores pins the "Mirror: no stores
// given" branch — Mirror() with no varargs sets the mode to modeMirror
// but doesn't populate childStores. Build() must reject the empty set.
func TestBuilder_Build_MirrorWithNoStores(t *testing.T) {
	_, err := NewMulti(&spyActiveContext{}).Mirror().Build()
	if err == nil {
		t.Fatal("expected error for Mirror with no stores; got nil")
	}
}

// TestBuilder_Build_WriteToWithNoStore exercises the
// "WriteTo: no write store given" branch via a zero-value
// BlobStoreInitialized. The builder sets mode=modeWriteThrough but
// writeStore.BlobStore stays nil, so Build() must reject.
func TestBuilder_Build_WriteToWithNoStore(t *testing.T) {
	_, err := NewMulti(&spyActiveContext{}).
		WriteTo(BlobStoreInitialized{}).
		Build()
	if err == nil {
		t.Fatal("expected error for WriteTo with zero-value store; got nil")
	}
}

// TestBuilder_WriteTo_AfterPoison_IsNoop pins that once the builder is
// in modeConfused (e.g. Read() called before WriteTo), a subsequent
// WriteTo short-circuits and preserves the first violation. Build()
// must still return the mode-confusion error.
func TestBuilder_WriteTo_AfterPoison_IsNoop(t *testing.T) {
	s := BlobStoreInitialized{BlobStore: &stubBlobStore{}}
	_, err := NewMulti(&spyActiveContext{}).
		Read(s).    // poisons to modeConfused (Read outside write-through)
		WriteTo(s). // short-circuits, mode stays modeConfused
		Build()
	if err == nil {
		t.Fatal("expected mode-confusion error; got nil")
	}
}

// TestBuilder_WriteTo_AfterMirrorWithStores pins the WriteTo "default"
// branch: when Mirror has populated mirrorStores, a follow-up WriteTo
// sets mode=modeConfused. (Distinct from the Read-then-WriteTo path
// above, which uses the modeConfused short-circuit at the top of
// WriteTo's switch.)
func TestBuilder_WriteTo_AfterMirrorWithStores(t *testing.T) {
	s := BlobStoreInitialized{BlobStore: &stubBlobStore{}}
	_, err := NewMulti(&spyActiveContext{}).
		Mirror(s). // mode=modeMirror, mirrorStores=[s]
		WriteTo(s).
		Build()
	if err == nil {
		t.Fatal("expected mode-confusion error; got nil")
	}
}

// TestBuilder_Build_WriteStoreInReadList_WithPaths pins that
// sameStore's id-based comparison fires when both stores carry a
// Path: two BlobStoreInitialized values sharing the same Path id
// are treated as the same store regardless of the embedded
// BlobStore interface value, so Build() must reject.
func TestBuilder_Build_WriteStoreInReadList_WithPaths(t *testing.T) {
	id := scoped_id.Make("dup-id")
	path := directory_layout.MakeBlobStorePath(id, "/base", "/config")

	// Two BlobStore interface values that differ — same Path id, distinct
	// underlying stubs. sameStore must collapse them.
	writeStore := BlobStoreInitialized{
		ConfigNamed: blob_store_configs.ConfigNamed{Path: path},
		BlobStore:   &stubBlobStore{},
	}
	readStore := BlobStoreInitialized{
		ConfigNamed: blob_store_configs.ConfigNamed{Path: path},
		BlobStore:   &stubBlobStore{},
	}

	_, err := NewMulti(&spyActiveContext{}).
		WriteTo(writeStore).
		Read(readStore).
		Build()
	if err == nil {
		t.Fatal("expected duplicate-store error; got nil")
	}
}
