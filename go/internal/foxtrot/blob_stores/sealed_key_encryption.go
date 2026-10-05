package blob_stores

import (
	"os"
	"path/filepath"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/bravo/directory_layout"
	"code.linenisgreat.com/madder/go/internal/charlie/store_key"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
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

// makeSealedKeyEncryption builds the encryption id for a sealed-key store
// whose sidecar is fetched by load. Nothing is read or opened here: load
// runs on the first blob read.
func makeSealedKeyEncryption(
	config blob_store_configs.ConfigSealedKey,
	load func() ([]byte, error),
) (encryption sealedKeyEncryption, err error) {
	if holder := config.GetKeyCustodyHolder(); holder != blob_store_configs.KeyHolderProcess {
		err = errors.Errorf("unsupported key-custody holder %q", holder)
		return encryption, err
	}

	encryption.Id = config.GetStorePublicKey()

	if encryption.wrapper, err = store_key.MakeIOWrapper(
		encryption.Id,
		func() ([]byte, error) {
			raw, err := load()
			if err != nil {
				return nil, err
			}

			sidecar, err := blob_store_configs.DecodeStoreKey(raw)
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

	return encryption, err
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
