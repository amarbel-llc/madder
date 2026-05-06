//go:build test

package commands_mcp

import (
	"net/url"
	"strings"
	"testing"
)

const validBlobDigest = "blake2b256-c5xgv9eyuv6g49mcwqks24gd3dh39w8220l0kl60qxt60rnt60lsc8fqv0"

func TestSplitURIQuery_NoQuery(t *testing.T) {
	base, q, err := splitURIQuery(uriBlobs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base != uriBlobs {
		t.Errorf("base = %q, want %q", base, uriBlobs)
	}
	if len(q) != 0 {
		t.Errorf("query = %v, want empty", q)
	}
}

func TestSplitURIQuery_WithQuery(t *testing.T) {
	uri := uriBlobs + "?limit=50&offset=200"
	base, q, err := splitURIQuery(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base != uriBlobs {
		t.Errorf("base = %q, want %q", base, uriBlobs)
	}
	if got := q.Get("limit"); got != "50" {
		t.Errorf("limit = %q, want %q", got, "50")
	}
	if got := q.Get("offset"); got != "200" {
		t.Errorf("offset = %q, want %q", got, "200")
	}
}

func TestSplitURIQuery_RejectsMalformedQuery(t *testing.T) {
	if _, _, err := splitURIQuery(uriBlobs + "?%zz"); err == nil {
		t.Fatal("expected error for malformed percent-escape, got nil")
	}
}

func TestPaginateDigests_DefaultLimit(t *testing.T) {
	digests := makeDigests(250)
	got := paginateDigests(digests, url.Values{})

	if got.Limit != defaultListLimit {
		t.Errorf("Limit = %d, want %d", got.Limit, defaultListLimit)
	}
	if got.Offset != 0 {
		t.Errorf("Offset = %d, want 0", got.Offset)
	}
	if got.Total != 250 {
		t.Errorf("Total = %d, want 250", got.Total)
	}
	if len(got.Blobs) != defaultListLimit {
		t.Errorf("len(Blobs) = %d, want %d", len(got.Blobs), defaultListLimit)
	}
	if got.Blobs[0].Digest != digests[0] {
		t.Errorf("first digest = %q, want %q", got.Blobs[0].Digest, digests[0])
	}
	if got.Blobs[0].URI != prefixBlob+digests[0] {
		t.Errorf("first uri = %q, want %q", got.Blobs[0].URI, prefixBlob+digests[0])
	}
}

func TestPaginateDigests_OffsetPastEnd(t *testing.T) {
	digests := makeDigests(10)
	q := url.Values{"offset": {"100"}}
	got := paginateDigests(digests, q)

	if got.Total != 10 {
		t.Errorf("Total = %d, want 10", got.Total)
	}
	if len(got.Blobs) != 0 {
		t.Errorf("len(Blobs) = %d, want 0", len(got.Blobs))
	}
}

func TestPaginateDigests_PartialPage(t *testing.T) {
	digests := makeDigests(5)
	q := url.Values{"offset": {"3"}, "limit": {"10"}}
	got := paginateDigests(digests, q)

	if got.Limit != 10 {
		t.Errorf("Limit = %d, want 10", got.Limit)
	}
	if len(got.Blobs) != 2 {
		t.Errorf("len(Blobs) = %d, want 2", len(got.Blobs))
	}
	if got.Blobs[0].Digest != digests[3] {
		t.Errorf("first digest = %q, want %q", got.Blobs[0].Digest, digests[3])
	}
}

func TestPaginateDigests_NegativeIgnored(t *testing.T) {
	digests := makeDigests(3)
	q := url.Values{"offset": {"-5"}, "limit": {"-1"}}
	got := paginateDigests(digests, q)

	if got.Offset != 0 {
		t.Errorf("Offset = %d, want 0", got.Offset)
	}
	if got.Limit != defaultListLimit {
		t.Errorf("Limit = %d, want default %d", got.Limit, defaultListLimit)
	}
}

func TestReadResource_RejectsBadStoreURI(t *testing.T) {
	p := &resourceProvider{}
	_, err := p.ReadResource(t.Context(), "madder://stores/")
	if err == nil {
		t.Fatal("expected error for trailing-slash stores URI, got nil")
	}
	if !strings.Contains(err.Error(), templateStoreBlobs) {
		t.Errorf("error %q does not mention the expected template", err.Error())
	}
}

func TestReadResource_RejectsUnknownURI(t *testing.T) {
	p := &resourceProvider{}
	_, err := p.ReadResource(t.Context(), "madder://other")
	if err == nil {
		t.Fatal("expected error for unknown URI, got nil")
	}
	if !strings.Contains(err.Error(), "unknown resource") {
		t.Errorf("error %q does not mention 'unknown resource'", err.Error())
	}
}

// makeDigests returns N synthetic digest strings in sorted order.
func makeDigests(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "sha256-digest" + zeroPad(i, 4)
	}
	return out
}

func zeroPad(n, width int) string {
	s := ""
	for i := 0; i < width; i++ {
		s = string(rune('0'+(n%10))) + s
		n /= 10
	}
	return s
}
