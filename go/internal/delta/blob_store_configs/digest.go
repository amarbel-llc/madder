package blob_store_configs

import (
	"bufio"
	"bytes"
	"io"

	"code.linenisgreat.com/hyphence/go/hyphence"
	"code.linenisgreat.com/madder/go/internal/charlie/markl_registrations"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
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
	// FDR-0010: EncodeWithDigest is the sole sanctioned config write path, so
	// it is the single funnel where a store's uuidv7 instance identity is
	// minted. Mint only for a config that carries an instance-id field
	// (mintable) and has not been minted yet (empty id) — i.e. a store being
	// created. A re-encode of an already-minted config preserves its id
	// (non-empty → skipped), and legacy configs are not mintable, so neither
	// re-mints. Because the id lives in the config BODY, the digest computed
	// below inherits its entropy and becomes instance-unique. Reads decode;
	// they never reach this write path, so nothing is ever lazy-minted.
	if mintable, ok := typedConfig.Blob.(ConfigInstanceIdMintable); ok {
		if len(mintable.GetInstanceId().GetBytes()) == 0 {
			var instanceId markl.Id

			if instanceId, err = markl_registrations.MintInstanceId(); err != nil {
				err = errors.Wrap(err)
				return n, err
			}

			mintable.SetInstanceId(instanceId)
		}
	}

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
