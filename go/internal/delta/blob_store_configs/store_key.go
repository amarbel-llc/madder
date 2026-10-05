package blob_store_configs

import (
	"bytes"

	"code.linenisgreat.com/hyphence/go/hyphence"
	charlie_bsc "code.linenisgreat.com/madder/go/internal/charlie/blob_store_configs"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

// TypeTomlStoreKeyV1 is the hyphence type of a sealed-key store's
// `blob_store-key` sidecar (FDR 0011).
const TypeTomlStoreKeyV1 = "toml-blob_store_key-v1"

type (
	TomlStoreKeyV1     = charlie_bsc.TomlStoreKeyV1
	StoreKeyRecipients = charlie_bsc.StoreKeyRecipients
	StoreKeySealed     = charlie_bsc.StoreKeySealed
)

// storeKeyHeader is the whole hyphence metadata section of a sidecar: one
// type line and nothing else. The sidecar carries no digest line because
// it is mutable (a digest pin is what makes blob_store-config immutable),
// and it needs no integrity check of its own: the sealed document inside
// is authenticated by its header MAC.
var storeKeyHeader = []byte(
	hyphence.Boundary + "\n" +
		"! " + TypeTomlStoreKeyV1 + "\n" +
		hyphence.Boundary + "\n\n",
)

// EncodeStoreKey renders a sidecar as a hyphence document.
func EncodeStoreKey(storeKey TomlStoreKeyV1) ([]byte, error) {
	if storeKey.Sealed.Document == "" {
		return nil, errors.Errorf("store key sidecar has no sealed document")
	}

	doc, err := charlie_bsc.DecodeTomlStoreKeyV1(nil)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	*doc.Data() = storeKey

	body, err := doc.Encode()
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return append(bytes.Clone(storeKeyHeader), body...), nil
}

// DecodeStoreKey parses a sidecar. Anything but exactly the expected
// header is refused: a different type line means a different format, not
// a sidecar to be read leniently.
func DecodeStoreKey(raw []byte) (storeKey TomlStoreKeyV1, err error) {
	body, ok := bytes.CutPrefix(raw, storeKeyHeader)
	if !ok {
		err = errors.Errorf(
			"not a %s document: unexpected header",
			TypeTomlStoreKeyV1,
		)

		return storeKey, err
	}

	doc, err := charlie_bsc.DecodeTomlStoreKeyV1(body)
	if err != nil {
		err = errors.Wrap(err)
		return storeKey, err
	}

	storeKey = *doc.Data()

	if storeKey.Sealed.Document == "" {
		err = errors.Errorf("store key sidecar has no sealed document")
		return storeKey, err
	}

	return storeKey, err
}
