package blob_store_configs

import (
	"bufio"
	"bytes"
	"io"

	"code.linenisgreat.com/hyphence/go/hyphence"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
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
// Mechanism: encode the body to a scratch buffer via the inner Blob
// coder, hash those bytes, stamp typedConfig.BlobDigest with the
// resulting markl-id, then assemble the on-disk output as
// `Boundary + metadata + Boundary + blank + bodyBuf` — the on-disk
// body is the exact byte sequence that was hashed. This avoids any
// dependency on the inner coder being deterministic across two calls
// (e.g. randomized encryption-key generation in the
// inventory_archive variants).
func EncodeWithDigest(
	typedConfig *TypedConfig,
	w io.Writer,
) (n int64, err error) {
	var bodyBuf bytes.Buffer
	bodyWriter := bufio.NewWriter(&bodyBuf)
	if _, err = Coder.Blob.EncodeTo(typedConfig, bodyWriter); err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	if err = bodyWriter.Flush(); err != nil {
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

	out := bufio.NewWriter(w)
	defer errors.DeferredFlusher(&err, out)

	var n1 int
	var n2 int64

	if n1, err = out.WriteString(hyphence.Boundary + "\n"); err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	n += int64(n1)

	if n2, err = Coder.Metadata.EncodeTo(typedConfig, out); err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	n += n2

	if n1, err = out.WriteString(hyphence.Boundary + "\n\n"); err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	n += int64(n1)

	if n1, err = out.Write(bodyBuf.Bytes()); err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	n += int64(n1)

	return n, err
}
