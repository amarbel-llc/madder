package commands

import (
	"fmt"

	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_stores"
	"code.linenisgreat.com/madder/go/internal/futility"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/ui"
)

// ensureWebdavRemoteConfigExists is the WebDAV analogue of
// ensureRemoteConfigExists: bootstraps a default TomlV3 remote
// blob_store-config at <url>/blob_store-config when missing. ADR 0005
// reserves the local TomlWebDAVV0 for transport (URL + auth), so
// hash/buckets/compression/encryption land in the remote config that
// this helper writes.
//
// With a sealedKeyConfig (-pigpen, FDR 0011) it writes that config and
// its blob_store-key sidecar instead.
//
// Returns false when the request was cancelled with an error so the
// caller can stop short of writing the local pointer config.
func (cmd *Init) ensureWebdavRemoteConfigExists(
	req futility.Request,
	blobStoreId scoped_id.Id,
	webdavConfig blob_store_configs.ConfigWebDAV,
	sealedKeyConfig *blob_store_configs.TypedConfig,
	sealedKeySidecar []byte,
) bool {
	printer := ui.MakePrefixPrinter(
		ui.Err(),
		fmt.Sprintf("# (blob_store: %s) ", blobStoreId),
	)

	var err error

	if sealedKeyConfig != nil {
		err = blob_stores.BootstrapWebdavSealedKeyConfig(
			req,
			printer,
			webdavConfig,
			sealedKeyConfig,
			sealedKeySidecar,
		)
	} else {
		err = blob_stores.BootstrapWebdavRemoteConfig(
			req,
			printer,
			webdavConfig,
			blob_stores.DiscoveredConfig{
				HashTypeId: string(blob_store_configs.HashTypeDefault),
				Buckets:    blob_store_configs.DefaultHashBuckets,
				Encryption: cmd.encryption,
				// Fresh stores created by init-webdav are modern multi-hash
				// stores. Mirrors the SFTP init flow's MultiHash: true to
				// avoid stamping fresh stores as legacy single-hash.
				MultiHash: true,
			},
		)
	}

	if err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
		return false
	}

	return true
}
