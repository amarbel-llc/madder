package blob_stores

import (
	"io"
	"os"
	"path/filepath"
	"sync"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/bravo/directory_layout"
	"code.linenisgreat.com/madder/go/internal/charlie/store_key"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/piggy/go/pkgs/pigpen"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/ui"
)

// sealedKeyEncryption is the encryption id of a sealed-key store (FDR
// 0011). It presents as the store's public key everywhere an id is
// displayed or compared, but hands out ONE store_key IO wrapper for the
// life of the store, so the sealed key is opened at most once per process
// however many blobs are read. A plain markl.Id would build a fresh
// wrapper, and so a fresh unseal, for every blob.
type sealedKeyEncryption struct {
	markl.Id
	wrapper interfaces.IOWrapper
}

var _ domain_interfaces.MarklId = sealedKeyEncryption{}

func (encryption sealedKeyEncryption) GetIOWrapper() (interfaces.IOWrapper, error) {
	return encryption.wrapper, nil
}

// makeSealedKeyEncryption builds the encryption id for the sealed-key
// store storeId, whose sidecar is fetched by load. Nothing is read or
// opened here: load runs at most once, on the first blob read or write.
func makeSealedKeyEncryption(
	storeId string,
	config blob_store_configs.ConfigSealedKey,
	load func() ([]byte, error),
) (encryption sealedKeyEncryption, err error) {
	if holder := config.GetKeyCustodyHolder(); holder != blob_store_configs.KeyHolderProcess {
		err = errors.Errorf("unsupported key-custody holder %q", holder)
		return encryption, err
	}

	encryption.Id = config.GetStorePublicKey()

	loadSidecar := sync.OnceValues(func() (blob_store_configs.StoreKey, error) {
		raw, err := load()
		if err != nil {
			return blob_store_configs.StoreKey{}, err
		}

		return blob_store_configs.DecodeStoreKey(raw)
	})

	var unsealing interfaces.IOWrapper

	if unsealing, err = store_key.MakeIOWrapper(
		encryption.Id,
		func() ([]byte, error) {
			sidecar, err := loadSidecar()
			if err != nil {
				return nil, err
			}

			return []byte(sidecar.Sealed.Document), nil
		},
		store_key.AgentOpener,
	); err != nil {
		err = errors.Wrap(err)
		return encryption, err
	}

	encryption.wrapper = &driftWarningIOWrapper{
		IOWrapper: unsealing,
		warnOnce: sync.OnceFunc(func() {
			warnOnRecipientDrift(storeId, loadSidecar)
		}),
	}

	return encryption, err
}

// driftWarningIOWrapper runs the recipient-drift check the first time the
// store is actually read from or written to, so commands that only list or
// describe stores never touch the pigpen.
type driftWarningIOWrapper struct {
	interfaces.IOWrapper
	warnOnce func()
}

func (wrapper *driftWarningIOWrapper) WrapReader(r io.Reader) (io.ReadCloser, error) {
	wrapper.warnOnce()
	return wrapper.IOWrapper.WrapReader(r)
}

func (wrapper *driftWarningIOWrapper) WrapWriter(w io.Writer) (io.WriteCloser, error) {
	wrapper.warnOnce()
	return wrapper.IOWrapper.WrapWriter(w)
}

// warnOnRecipientDrift re-reads the pigpen the store key was sealed
// against and warns on stderr when its recipient set is no longer the
// sealed one. It only ever warns: a changed, missing or unresolvable
// pigpen must not make a store unreadable or block a write, and madder
// never re-seals on its own (FDR 0011 "Drift").
func warnOnRecipientDrift(
	storeId string,
	loadSidecar func() (blob_store_configs.StoreKey, error),
) {
	printer := ui.MakePrefixPrinter(ui.Err(), "# (blob_store: "+storeId+") ")

	sidecar, err := loadSidecar()
	if err != nil {
		// A write would otherwise succeed silently against a store whose
		// key cannot be found; a read reports the same failure itself.
		printer.Printf("warning: cannot read the store key sidecar: %s", err)
		return
	}

	sealed, err := store_key.SealedRecipients([]byte(sidecar.Sealed.Document))
	if err != nil {
		printer.Printf("warning: cannot read the store key sidecar: %s", err)
		return
	}

	// Not live: a pigpen behind a pointer is compared from this machine's
	// cache, so ordinary commands make no network request once it is warm.
	// `key-status` refreshes it.
	current, err := blob_store_configs.StoreKeySource(sidecar).Recipients(false)
	if err != nil {
		printer.Printf(
			"warning: cannot re-read the pigpen the store key was sealed "+
				"to; proceeding with the sealed recipient set: %s",
			err,
		)

		return
	}

	if pigpen.SameRecipientSet(current, sealed) {
		return
	}

	printer.Printf(
		"warning: recipient set has changed since the store key was "+
			"sealed; run `madder key-status %s`, then `madder key-reseal %s`",
		storeId,
		storeId,
	)
}

// StoreKeySidecarStore is implemented by the store types that can be
// sealed-key stores (FDR 0011): access to the mutable blob_store-key
// sidecar next to the store's config.
type StoreKeySidecarStore interface {
	ReadStoreKeySidecar() ([]byte, error)
	// WriteStoreKeySidecar replaces the sidecar. It must never leave the
	// store without one.
	WriteStoreKeySidecar([]byte) error
}

// localStoreKeySidecarPath is where a local sealed-key store keeps its
// sidecar: next to blob_store-config, at the store's base path.
func localStoreKeySidecarPath(basePath string) string {
	return filepath.Join(basePath, directory_layout.FileNameBlobStoreKey)
}

func loadLocalStoreKeySidecar(basePath string) func() ([]byte, error) {
	return func() ([]byte, error) {
		return os.ReadFile(localStoreKeySidecarPath(basePath))
	}
}

// WriteLocalStoreKeySidecar replaces a local sealed-key store's sidecar
// atomically: a reader sees the old document or the new one, never a
// partial write. Unlike blob_store-config the sidecar stays writable,
// since re-sealing rewrites it.
func WriteLocalStoreKeySidecar(path string, sidecar []byte) (err error) {
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return errors.Wrap(err)
	}

	defer func() {
		if err != nil {
			_ = os.Remove(temp.Name())
		}
	}()

	if _, err = temp.Write(sidecar); err != nil {
		_ = temp.Close()
		return errors.Wrap(err)
	}

	if err = temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		return errors.Wrap(err)
	}

	// The sidecar is the store's only key: make sure its bytes are on disk
	// before the rename makes them the sidecar.
	if err = temp.Sync(); err != nil {
		_ = temp.Close()
		return errors.Wrap(err)
	}

	if err = temp.Close(); err != nil {
		return errors.Wrap(err)
	}

	if err = os.Rename(temp.Name(), path); err != nil {
		return errors.Wrap(err)
	}

	return nil
}
