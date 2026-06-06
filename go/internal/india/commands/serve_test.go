package commands

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
)

// validDigest is a real blake2b256 markl id (the example from the MCP
// server's docs); it parses and round-trips through markl.Id.Set.
const validDigest = "blake2b256-c5xgv9eyuv6g49mcwqks24gd3dh39w8220l0kl60qxt60rnt60lsc8fqv0"

type nopSeekCloser struct{ *strings.Reader }

func (nopSeekCloser) Close() error { return nil }

// fakeBlobSource serves in-memory blobs keyed by digest string. A nil
// entry value distinct from "absent" is not needed: missing key → not
// found.
type fakeBlobSource struct {
	blobs map[string]string
	err   error // when non-nil, Open reports a backend error for any id
}

func (f fakeBlobSource) Open(
	id domain_interfaces.MarklId,
) (io.ReadSeekCloser, bool, error) {
	if f.err != nil {
		return nil, true, f.err
	}
	data, ok := f.blobs[id.String()]
	if !ok {
		return nil, false, nil
	}
	return nopSeekCloser{strings.NewReader(data)}, true, nil
}

func newTestServer(src blobSource) *httptest.Server {
	return httptest.NewServer((&blobServer{source: src}).mux())
}

func TestServeBlobFound(t *testing.T) {
	const body = "hello, clear text\n"
	srv := newTestServer(fakeBlobSource{blobs: map[string]string{validDigest: body}})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/blobs/" + validDigest)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if string(got) != body {
		t.Fatalf("body = %q, want %q", got, body)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable", cc)
	}
	if etag := resp.Header.Get("ETag"); etag != `"`+validDigest+`"` {
		t.Errorf("ETag = %q, want digest-derived", etag)
	}
}

func TestServeBlobNotFound(t *testing.T) {
	srv := newTestServer(fakeBlobSource{blobs: map[string]string{}})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/blobs/" + validDigest)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestServeInvalidDigest(t *testing.T) {
	srv := newTestServer(fakeBlobSource{})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/blobs/not-a-real-digest")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestServeEmptyAndNestedDigest(t *testing.T) {
	srv := newTestServer(fakeBlobSource{})
	defer srv.Close()

	for _, path := range []string{"/blobs/", "/blobs/" + validDigest + "/extra"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, resp.StatusCode)
		}
	}
}

func TestServeMethodNotAllowed(t *testing.T) {
	srv := newTestServer(fakeBlobSource{blobs: map[string]string{validDigest: "x"}})
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/blobs/"+validDigest, "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); allow != "GET, HEAD" {
		t.Errorf("Allow = %q, want \"GET, HEAD\"", allow)
	}
}

func TestServeHead(t *testing.T) {
	const body = "head-me"
	srv := newTestServer(fakeBlobSource{blobs: map[string]string{validDigest: body}})
	defer srv.Close()

	resp, err := http.Head(srv.URL + "/blobs/" + validDigest)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if len(got) != 0 {
		t.Errorf("HEAD returned body %q, want empty", got)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want sniffed text/plain", ct)
	}
}

func TestServeBackendError(t *testing.T) {
	srv := newTestServer(fakeBlobSource{err: errors.Errorf("store offline")})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/blobs/" + validDigest)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
}

func TestServeHealthz(t *testing.T) {
	srv := newTestServer(fakeBlobSource{})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(got)) != "ok" {
		t.Errorf("body = %q, want ok", got)
	}
}
