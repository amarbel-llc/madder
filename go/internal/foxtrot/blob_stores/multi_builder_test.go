//go:build test

package blob_stores

import (
	"testing"
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
