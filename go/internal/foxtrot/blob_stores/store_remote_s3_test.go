//go:build test

package blob_stores

import (
	stderrors "errors"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/smithy-go"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/piggy/go/pkgs/markl"
)

// TestS3MakeBlobWriter_SingleHashRejectsForeignType pins the #262
// loud-fail guard for S3: a single-hash store must reject a request for
// a hash type other than its configured one before any network call
// (the guard runs ahead of mover.initialize, so once is latched to skip
// the live-connection path).
func TestS3MakeBlobWriter_SingleHashRejectsForeignType(t *testing.T) {
	store := &remoteS3{
		remoteBlobStoreBase: remoteBlobStoreBase{
			id:              mustParseBlobStoreId(t, "s3-hashtest"),
			multiHash:       false,
			defaultHashType: markl.FormatHashSha256,
		},
	}
	store.once.Do(func() {}) // latch: initialize() will not run

	writer, err := store.MakeBlobWriter(markl.FormatHashBlake2b256)
	if writer != nil {
		t.Fatalf("MakeBlobWriter: got non-nil writer on rejected request")
	}
	if err == nil {
		t.Fatal("MakeBlobWriter: got nil error requesting a foreign hash type")
	}
	if !strings.Contains(err.Error(), "single-hash") {
		t.Errorf("error %q missing 'single-hash' anchor", err.Error())
	}
}

func mustParseBlobStoreId(t *testing.T, s string) scoped_id.Id {
	t.Helper()
	var id scoped_id.Id
	if err := id.Set(s); err != nil {
		t.Fatalf("scoped_id.Set(%q): %v", s, err)
	}
	return id
}

func TestS3JoinKey(t *testing.T) {
	for _, tc := range []struct {
		name, prefix, leaf, want string
	}{
		{"empty prefix", "", "blob_store-config", "blob_store-config"},
		{"trailing slash prefix", "blobs/", "blob_store-config", "blobs/blob_store-config"},
		{"nested prefix", "a/b/c", "x", "a/b/c/x"},
		{"leading slash leaf", "blobs", "/x", "blobs/x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := s3JoinKey(tc.prefix, tc.leaf); got != tc.want {
				t.Errorf("s3JoinKey(%q, %q) = %q; want %q",
					tc.prefix, tc.leaf, got, tc.want)
			}
		})
	}
}

func TestIsS3NotFound(t *testing.T) {
	apiErr := &smithy.GenericAPIError{Code: "NotFound", Message: "head not found"}
	if !isS3NotFound(apiErr) {
		t.Errorf("smithy NotFound should be recognized")
	}

	other := &smithy.GenericAPIError{Code: "AccessDenied"}
	if isS3NotFound(other) {
		t.Errorf("AccessDenied should not be a NotFound")
	}

	if isS3NotFound(nil) {
		t.Errorf("nil should not be a NotFound")
	}

	wrapped := stderrors.Join(stderrors.New("ctx"), apiErr)
	if !isS3NotFound(wrapped) {
		t.Errorf("wrapped NotFound should still be recognized")
	}
}

func TestKeyForMarklId_SingleHash(t *testing.T) {
	hashType, err := markl.GetFormatHashOrError(blob_store_configs.DefaultHashTypeId)
	if err != nil {
		t.Fatalf("GetFormatHashOrError: %v", err)
	}
	id, repool := hashType.GetBlobId()
	defer repool()

	// 64 hex chars (sha256-shaped). Use a fixed predictable hex via
	// SetHexStringFromRelPath so the test asserts on the exact key.
	const hex = "deadbeefcafe0123456789abcdef0123456789abcdef0123456789abcdef0011"
	if err := markl.SetHexStringFromRelPath(id, "de/ad/"+hex[4:]); err != nil {
		t.Fatalf("SetHexStringFromRelPath: %v", err)
	}

	store := &remoteS3{
		remoteBlobStoreBase: remoteBlobStoreBase{
			buckets:   []int{2},
			multiHash: false,
		},
		config: &blob_store_configs.TomlS3V0{Prefix: "blobs"},
	}

	got := store.keyForMarklId(id)
	want := "blobs/de/" + hex[2:]
	if got != want {
		t.Errorf("keyForMarklId single-hash buckets=[2] = %q; want %q", got, want)
	}
}

func TestKeyForMarklId_MultiHash(t *testing.T) {
	hashType, err := markl.GetFormatHashOrError(blob_store_configs.DefaultHashTypeId)
	if err != nil {
		t.Fatalf("GetFormatHashOrError: %v", err)
	}
	id, repool := hashType.GetBlobId()
	defer repool()

	const hex = "deadbeefcafe0123456789abcdef0123456789abcdef0123456789abcdef0011"
	if err := markl.SetHexStringFromRelPath(id, "de/ad/"+hex[4:]); err != nil {
		t.Fatalf("SetHexStringFromRelPath: %v", err)
	}

	store := &remoteS3{
		remoteBlobStoreBase: remoteBlobStoreBase{
			buckets:   []int{2},
			multiHash: true,
		},
		config: &blob_store_configs.TomlS3V0{Prefix: ""},
	}

	got := store.keyForMarklId(id)
	want := id.GetMarklFormat().GetMarklFormatId() + "/de/" + hex[2:]
	if got != want {
		t.Errorf("keyForMarklId multi-hash empty-prefix = %q; want %q", got, want)
	}
}

func TestIsRemoteConfigAlreadyExists(t *testing.T) {
	err := errS3RemoteConfigExists{Bucket: "b", Key: "k"}
	if !IsRemoteConfigAlreadyExists(err) {
		t.Errorf("expected errS3RemoteConfigExists to be recognized")
	}

	other := &smithy.GenericAPIError{Code: "AccessDenied"}
	if IsRemoteConfigAlreadyExists(other) {
		t.Errorf("AccessDenied should not be recognized as RemoteConfigAlreadyExists")
	}

	if IsRemoteConfigAlreadyExists(nil) {
		t.Errorf("nil should not be recognized as RemoteConfigAlreadyExists")
	}

	wrapped := stderrors.Join(stderrors.New("ctx"), errS3RemoteConfigExists{Bucket: "b", Key: "k"})
	if !IsRemoteConfigAlreadyExists(wrapped) {
		t.Errorf("wrapped errS3RemoteConfigExists should still be recognized")
	}
}

// TestS3MoverEmitWriteEvent_FiresOnceWithWrittenOp mirrors the
// TestSftpMoverEmitWriteEvent_FiresOnceWithWrittenOp pin: the S3
// mover must call observer.OnBlobPublished exactly once per
// successful upload with op = "written".
func TestS3MoverEmitWriteEvent_FiresOnceWithWrittenOp(t *testing.T) {
	id := mustParseBlobStoreId(t, "s3-default")
	observer := &recordingObserver{}

	store := &remoteS3{
		remoteBlobStoreBase: remoteBlobStoreBase{
			id:       id,
			observer: observer,
		},
	}
	mover := &s3Mover{store: store}

	mover.emitWriteEvent(domain_interfaces.BlobWriteOpWritten, 12345)

	if len(observer.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(observer.events))
	}
	if observer.events[0].Size != 12345 {
		t.Errorf("Size = %d, want 12345", observer.events[0].Size)
	}
	if observer.events[0].StoreId != "s3-default" {
		t.Errorf("StoreId = %q, want s3-default", observer.events[0].StoreId)
	}
}

func TestS3MoverEmitWriteEvent_NilObserverIsNoop(t *testing.T) {
	id := mustParseBlobStoreId(t, "s3-default")
	store := &remoteS3{
		remoteBlobStoreBase: remoteBlobStoreBase{id: id, observer: nil},
	}
	mover := &s3Mover{store: store}
	mover.emitWriteEvent(domain_interfaces.BlobWriteOpWritten, 1) // must not panic
}

// Sanity that the standard HTTP NotFound shape (404) is not by itself
// enough — we rely on the SDK to fold 404s into typed errors. This
// test documents the assumption.
func TestIsS3NotFound_RawHttpStatusNotEnough(t *testing.T) {
	// A bare *http.Response with status 404 isn't an APIError —
	// callers always see SDK-typed errors. Confirm we don't
	// accidentally match anything that just contains "404".
	err := stderrors.New("HTTP 404")
	if isS3NotFound(err) {
		t.Errorf("plain string 404 should not be NotFound")
	}
	_ = http.StatusNotFound
}

// TestValidateS3Auth_RejectsSessionTokenWithoutAccessKey pins the
// single S3 auth-state invariant: SessionToken is a temporary
// credential that requires an AccessKeyId. Surfacing this at init
// time means the user gets a clear error instead of an opaque AWS
// SDK credential-chain failure on first request.
func TestValidateS3Auth_RejectsSessionTokenWithoutAccessKey(t *testing.T) {
	config := &blob_store_configs.TomlS3V0{
		Bucket:       "test",
		SessionToken: "session-token-secret",
	}
	err := ValidateS3Auth(config)
	if err == nil {
		t.Fatal("session-token without access-key-id validated; want rejection")
	}
}

// TestValidateS3Auth_AcceptsValidShapes covers the auth-mode
// configurations the SDK's default credential chain handles cleanly.
func TestValidateS3Auth_AcceptsValidShapes(t *testing.T) {
	cases := []struct {
		name   string
		config blob_store_configs.TomlS3V0
	}{
		{"anonymous", blob_store_configs.TomlS3V0{Bucket: "b"}},
		{
			"static-creds",
			blob_store_configs.TomlS3V0{
				Bucket:          "b",
				AccessKeyId:     "AKIA0000",
				SecretAccessKey: "secret",
			},
		},
		{
			"static-creds + session-token",
			blob_store_configs.TomlS3V0{
				Bucket:          "b",
				AccessKeyId:     "AKIA0000",
				SecretAccessKey: "secret",
				SessionToken:    "sess",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateS3Auth(&tc.config); err != nil {
				t.Errorf("%s rejected: %v", tc.name, err)
			}
		})
	}
}
