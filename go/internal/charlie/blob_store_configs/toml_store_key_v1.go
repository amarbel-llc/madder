package blob_store_configs

import (
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

// StoreKeyRecipients records where a sealed store key's recipient set
// came from and what it was when the key was sealed.
//
//go:generate tommy generate
type StoreKeyRecipients struct {
	// Source is the pigpen the recipients were loaded from, as the
	// operator gave it: a path to a piggy-ids file in any of its forms
	// (which may itself be a pointer to a remotely hosted pigpen).
	Source string `toml:"source"`

	// Digest is the digest of piggy's canonical recipient-set bytes at
	// seal time. Re-resolving Source and comparing detects a changed
	// recipient set without opening anything.
	Digest markl.Id `toml:"digest"`
}

// StoreKeySealed holds the sealed pigpen-v1 document whose payload is the
// store key's secret half.
//
//go:generate tommy generate
type StoreKeySealed struct {
	Document string `toml:"document"`
}

// TomlStoreKeyV1 is the body of a sealed-key store's `blob_store-key`
// sidecar (FDR 0011). It sits next to `blob_store-config` and, unlike
// that config, is mutable: re-sealing to a changed recipient set rewrites
// it. It holds no secret in the clear, so it can live on a remote.
//
//go:generate tommy generate
type TomlStoreKeyV1 struct {
	Recipients StoreKeyRecipients `toml:"recipients"`
	Sealed     StoreKeySealed     `toml:"sealed"`
}

// Validate rejects a sidecar with no sealed document, except for the
// blank document the coder builds before filling it in for an encode.
func (storeKey TomlStoreKeyV1) Validate() error {
	if storeKey.Sealed.Document == "" &&
		storeKey.Recipients.Source == "" &&
		storeKey.Recipients.Digest.IsNull() {
		return nil
	}

	if storeKey.Sealed.Document == "" {
		return errors.Errorf("store key sidecar has no sealed document")
	}

	return nil
}
