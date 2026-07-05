package sftp_probe

import (
	"testing"

	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/piggy/go/markl/pkgs/markl"
)

func TestEnumerateCandidates_NoKeys(t *testing.T) {
	layout := blob_stores.DiscoveredConfig{
		HashTypeId: "sha256",
		MultiHash:  false,
		Buckets:    []int{2},
	}
	got := EnumerateCandidates(layout, nil)
	if len(got) != 4 {
		t.Fatalf("want 4 candidates (1 enc x 4 comp); got %d", len(got))
	}
	wantLabels := map[string]bool{
		"none/none": false,
		"gzip/none": false,
		"zlib/none": false,
		"zstd/none": false,
	}
	for _, c := range got {
		if _, ok := wantLabels[c.Label]; !ok {
			t.Errorf("unexpected label %q", c.Label)
			continue
		}
		wantLabels[c.Label] = true
	}
	for label, seen := range wantLabels {
		if !seen {
			t.Errorf("missing candidate label %q", label)
		}
	}
}

func TestEnumerateCandidates_TwoKeys(t *testing.T) {
	layout := blob_stores.DiscoveredConfig{
		HashTypeId: "sha256",
		Buckets:    []int{2},
	}
	keyA := generateAgeKeyForTest(t)
	keyB := generateAgeKeyForTest(t)

	got := EnumerateCandidates(layout, []markl.Id{keyA, keyB})
	want := 4 * (1 + 2)
	if len(got) != want {
		t.Fatalf("want %d candidates; got %d", want, len(got))
	}

	wanted := []string{
		"none/none", "gzip/none", "zlib/none", "zstd/none",
		"none/age-key1", "gzip/age-key1", "zlib/age-key1", "zstd/age-key1",
		"none/age-key2", "gzip/age-key2", "zlib/age-key2", "zstd/age-key2",
	}
	seen := make(map[string]bool, len(got))
	for _, c := range got {
		seen[c.Label] = true
	}
	for _, w := range wanted {
		if !seen[w] {
			t.Errorf("missing candidate %q", w)
		}
	}
}

func TestEnumerateCandidates_StableOrder(t *testing.T) {
	layout := blob_stores.DiscoveredConfig{
		HashTypeId: "sha256",
		Buckets:    []int{2},
	}
	keyA := generateAgeKeyForTest(t)
	a := EnumerateCandidates(layout, []markl.Id{keyA})
	b := EnumerateCandidates(layout, []markl.Id{keyA})
	if len(a) != len(b) {
		t.Fatalf("len mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Label != b[i].Label {
			t.Errorf("position %d: %q vs %q", i, a[i].Label, b[i].Label)
		}
	}
}
