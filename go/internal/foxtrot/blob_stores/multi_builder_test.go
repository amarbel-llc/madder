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
