package blob_store_configs

import (
	"bufio"
	"bytes"
	"io"

	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
)

// DigestPurpose is the markl purpose stamped on the @ line of every
// migrated blob_store-config. See FDR-0008.
const DigestPurpose = markl.PurposeBlobStoreConfigDigestV1

// DigestHash is the hash family used to compute the body digest.
// Phase 1 hard-codes blake2b256.
var DigestHash = markl.FormatHashBlake2b256

// EncodeWithDigest renders typedConfig to w with a populated BlobDigest
// covering the body bytes. It is the only sanctioned write path for
// blob_store-config files after FDR-0008 Phase 1.
//
// Mechanism: render the body to a scratch buffer via the inner Blob
// coder, hash those bytes, stamp typedConfig.BlobDigest, then re-render
// through the full Coder so the @ line + metadata wrap surround the
// same body bytes. The double-encode keeps the hash input well-defined
// (body bytes only) and avoids reworking the hyphence coder's
// metadata-first emission order.
func EncodeWithDigest(
	typedConfig *TypedConfig,
	w io.Writer,
) (n int64, err error) {
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

	tmpId, idRepool := hash.GetMarklId()
	defer idRepool()
	if err = tmpId.SetPurposeId(DigestPurpose); err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	if err = typedConfig.BlobDigest.SetDigest(tmpId); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	if n, err = Coder.EncodeTo(typedConfig, w); err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	return n, err
}
