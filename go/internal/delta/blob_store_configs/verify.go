package blob_store_configs

import (
	"bufio"
	"bytes"
	"io"

	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
)

// DecodeAndVerify decodes a blob_store-config from r and, if its
// metadata carries a BlobDigest, re-hashes the body bytes and asserts
// the digests match via markl.AssertEqual. A config with no @ line is
// trusted silently (pre-FDR-0008 back-compat). A mismatch returns
// markl.ErrNotEqual carrying both digests.
//
// Implementation: buffer the whole input, run Coder.DecodeFrom on the
// buffered bytes (populates BlobDigest from the metadata), then
// re-encode the decoded body via the inner Blob coder and hash that.
// Buffering the whole config is acceptable because blob_store-config
// files are bounded in size (KB-scale, not MB).
//
// Determinism: the re-encode approach relies on every inner Blob coder
// producing byte-identical output for a given Config value. The TOML
// encoders in go/internal/charlie/blob_store_configs/toml_*.go satisfy
// this today (sorted-key emission via Tommy codegen). If a future
// encoder introduces non-determinism, switch to teeing the raw
// post-boundary bytes through a hasher during the original decode.
func DecodeAndVerify(
	typedConfig *TypedConfig,
	r io.Reader,
) (n int64, err error) {
	all, err := io.ReadAll(r)
	if err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	n = int64(len(all))

	if _, err = Coder.DecodeFrom(typedConfig, bytes.NewReader(all)); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	if typedConfig.BlobDigest.IsNull() {
		return n, err
	}

	var bodyBuf bytes.Buffer
	bw := bufio.NewWriter(&bodyBuf)
	if _, err = Coder.Blob.EncodeTo(typedConfig, bw); err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	if err = bw.Flush(); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	hash, hashRepool := DigestHash.Get()
	defer hashRepool()
	if _, err = hash.Write(bodyBuf.Bytes()); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	// Use a stack-allocated Id (rather than the pooled one returned by
	// hash.GetMarklId) so the bytes survive being stored in ErrNotEqual
	// — a pooled Id would be repooled on this function's return and
	// inspecting the error after the fact would see cleared bytes.
	var computed markl.Id
	if err = computed.SetMarklId(
		DigestHash.GetMarklFormatId(),
		hash.Sum(nil),
	); err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	if err = computed.SetPurposeId(DigestPurpose); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	if err = markl.AssertEqual(&typedConfig.BlobDigest, &computed); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	return n, err
}
