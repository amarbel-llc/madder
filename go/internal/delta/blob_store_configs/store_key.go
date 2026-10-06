package blob_store_configs

import (
	"bytes"
	"encoding/base64"
	"strings"

	"code.linenisgreat.com/hyphence/go/hyphence"
	charlie_bsc "code.linenisgreat.com/madder/go/internal/charlie/blob_store_configs"
	"code.linenisgreat.com/madder/go/internal/charlie/store_key"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

// The hyphence types of a sealed-key store's `blob_store-key` sidecar (FDR
// 0011). V2 is what madder writes. V1, which recorded only a local path
// and a recipient-set digest, is still read.
const (
	TypeTomlStoreKeyV1 = "toml-blob_store_key-v1"
	TypeTomlStoreKeyV2 = "toml-blob_store_key-v2"
)

type (
	TomlStoreKeyV2     = charlie_bsc.TomlStoreKeyV2
	StoreKeyRecipients = charlie_bsc.StoreKeyRecipientsV2
	StoreKeySealed     = charlie_bsc.StoreKeySealedV2

	// StoreKey is the current sidecar; older versions decode into it. In
	// memory Sealed.Document is the sealed pigpen document itself; only
	// EncodeStoreKey and DecodeStoreKey see its on-disk base64.
	StoreKey = TomlStoreKeyV2
)

// sealedDocumentLineLength wraps the base64 of the sealed document, so the
// sidecar stays readable and diffable.
const sealedDocumentLineLength = 64

func encodeSealedDocument(document string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(document))

	var wrapped strings.Builder

	for len(encoded) > sealedDocumentLineLength {
		wrapped.WriteString(encoded[:sealedDocumentLineLength])
		wrapped.WriteByte('\n')
		encoded = encoded[sealedDocumentLineLength:]
	}

	wrapped.WriteString(encoded)
	wrapped.WriteByte('\n')

	return wrapped.String()
}

func decodeSealedDocument(encoded string) (string, error) {
	document, err := base64.StdEncoding.DecodeString(
		strings.Join(strings.Fields(encoded), ""),
	)
	if err != nil {
		return "", errors.Wrapf(err, "sealed document is not base64")
	}

	return string(document), nil
}

// storeKeyHeader is the whole hyphence metadata section of a sidecar: one
// type line and nothing else. The sidecar carries no digest line because
// it is mutable (a digest pin is what makes blob_store-config immutable),
// and it needs no integrity check of its own: the sealed document inside
// is authenticated by its header MAC.
func storeKeyHeader(typeId string) []byte {
	return []byte(
		hyphence.Boundary + "\n" +
			"! " + typeId + "\n" +
			hyphence.Boundary + "\n\n",
	)
}

// StoreKeySource is the pigpen source a sidecar records.
func StoreKeySource(storeKey StoreKey) store_key.Source {
	return store_key.Source{
		Kind:    storeKey.Recipients.SourceKind,
		Locator: storeKey.Recipients.Source,
		Pigpen:  storeKey.Recipients.Pigpen,
	}
}

// MakeStoreKey builds the sidecar for a store key sealed to recipients,
// which were resolved from source. A path source gets a snapshot of the
// recipients embedded, so the sidecar says who the pigpen named when the
// key was sealed; the other kinds already carry their own document.
func MakeStoreKey(
	source store_key.Source,
	recipients []markl.Id,
	sealed []byte,
) (storeKey StoreKey, err error) {
	pigpen := source.Pigpen

	if source.Kind == store_key.SourceKindPath {
		if pigpen, err = store_key.Snapshot(recipients); err != nil {
			return storeKey, errors.Wrap(err)
		}
	}

	return StoreKey{
		Recipients: StoreKeyRecipients{
			SourceKind: source.Kind,
			Source:     source.Locator,
			Pigpen:     pigpen,
		},
		Sealed: StoreKeySealed{Document: string(sealed)},
	}, nil
}

// EncodeStoreKey renders a sidecar as a hyphence document.
func EncodeStoreKey(storeKey StoreKey) ([]byte, error) {
	if storeKey.Sealed.Document == "" {
		return nil, errors.Errorf("store key sidecar has no sealed document")
	}

	if storeKey.Recipients.SourceKind == "" {
		return nil, errors.Errorf("store key sidecar has no source_kind")
	}

	doc, err := charlie_bsc.DecodeTomlStoreKeyV2(nil)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	onDisk := storeKey
	onDisk.Sealed.Document = encodeSealedDocument(storeKey.Sealed.Document)

	*doc.Data() = onDisk

	body, err := doc.Encode()
	if err != nil {
		return nil, errors.Wrap(err)
	}

	encoded := append(storeKeyHeader(TypeTomlStoreKeyV2), body...)

	// The sidecar is the store's only key. Refuse to hand back bytes that
	// do not read back as exactly what was asked for, rather than find out
	// when the store is next opened.
	readBack, err := DecodeStoreKey(encoded)
	if err != nil {
		return nil, errors.Wrapf(err, "store key sidecar does not read back")
	}

	if readBack != storeKey {
		return nil, errors.Errorf(
			"store key sidecar does not read back as written; refusing to use it",
		)
	}

	return encoded, nil
}

// DecodeStoreKey parses a sidecar of either version. Anything but exactly
// an expected header is refused: a different type line means a different
// format, not a sidecar to be read leniently.
func DecodeStoreKey(raw []byte) (storeKey StoreKey, err error) {
	if body, ok := bytes.CutPrefix(raw, storeKeyHeader(TypeTomlStoreKeyV2)); ok {
		doc, err := charlie_bsc.DecodeTomlStoreKeyV2(body)
		if err != nil {
			return storeKey, errors.Wrap(err)
		}

		storeKey = *doc.Data()

		if storeKey.Sealed.Document, err = decodeSealedDocument(
			storeKey.Sealed.Document,
		); err != nil {
			return storeKey, err
		}
	} else if body, ok := bytes.CutPrefix(raw, storeKeyHeader(TypeTomlStoreKeyV1)); ok {
		doc, err := charlie_bsc.DecodeTomlStoreKeyV1(body)
		if err != nil {
			return storeKey, errors.Wrap(err)
		}

		// V1 only knew a local path. Its digest is dropped: the sealed
		// document's own recipient list says the same thing.
		storeKey = StoreKey{
			Recipients: StoreKeyRecipients{
				SourceKind: "path",
				Source:     doc.Data().Recipients.Source,
			},
			// V1 wrote the sealed document straight into a TOML string.
			Sealed: StoreKeySealed{Document: doc.Data().Sealed.Document},
		}
	} else {
		return storeKey, errors.Errorf(
			"not a %s or %s document: unexpected header",
			TypeTomlStoreKeyV2,
			TypeTomlStoreKeyV1,
		)
	}

	if storeKey.Sealed.Document == "" {
		return storeKey, errors.Errorf("store key sidecar has no sealed document")
	}

	return storeKey, nil
}
