package blob_store_configs

import (
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

// StoreKeyRecipientsV2 records where a sealed store key's recipients come
// from. There is no digest: the sealed document lists the recipients it is
// sealed to, so a changed pigpen is detected by comparing against that.
//
//go:generate tommy generate
type StoreKeyRecipientsV2 struct {
	// SourceKind is "path", "papi" or "embedded" (store_key.SourceKind*).
	SourceKind string `toml:"source_kind"`

	// Source locates the pigpen: a local file path for "path", a PAPI
	// identity domain for "papi". Absent for "embedded".
	Source string `toml:"source,omitempty"`

	// Pigpen is a pigpen document: the pigpen itself for "embedded", the
	// pointer to the published pigpen for "papi", and for "path" a snapshot
	// of the recipients at seal time.
	Pigpen string `toml:"pigpen,omitempty,multiline"`
}

// StoreKeySealedV2 holds the sealed pigpen-v1 document whose payload is
// the store key's secret half. A sealed document ends in raw ciphertext
// bytes, which a TOML string cannot hold, so on disk Document is the
// base64 of the document, wrapped; the coder in delta/blob_store_configs
// converts, and in memory it is the document itself.
//
//go:generate tommy generate
type StoreKeySealedV2 struct {
	Document string `toml:"document,multiline"`
}

// TomlStoreKeyV2 is the body of a sealed-key store's `blob_store-key`
// sidecar (FDR 0011). It sits next to `blob_store-config` and, unlike
// that config, is mutable: re-sealing to a changed recipient set rewrites
// it. It holds no secret in the clear, so it can live on a remote.
//
//go:generate tommy generate
type TomlStoreKeyV2 struct {
	Recipients StoreKeyRecipientsV2 `toml:"recipients"`
	Sealed     StoreKeySealedV2     `toml:"sealed"`
}

// Validate rejects a sidecar with no sealed document, except for the
// blank document the coder builds before filling it in for an encode.
func (storeKey TomlStoreKeyV2) Validate() error {
	if storeKey == (TomlStoreKeyV2{}) {
		return nil
	}

	if storeKey.Sealed.Document == "" {
		return errors.Errorf("store key sidecar has no sealed document")
	}

	return nil
}
