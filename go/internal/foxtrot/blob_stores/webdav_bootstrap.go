package blob_stores

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/0/ids"
	"github.com/amarbel-llc/madder/go/internal/bravo/directory_layout"
	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ui"
)

// BootstrapWebdavRemoteConfig is the WebDAV analogue of WriteRemoteConfig
// for the fresh-bootstrap path: HEAD checks that no remote config
// exists, MKCOL ensures the base URL is a collection, and PUT writes
// a default TomlV3 config built from `discovered`. Unlike the SFTP
// counterpart it does not use a tmp + atomic-rename dance — init is
// single-threaded and the remote config file is not content-addressed,
// so a direct PUT is enough.
//
// Returns an error if a remote `blob_store-config` already exists at
// `<url>/blob_store-config`; callers that want overwrite semantics
// must DELETE first.
func BootstrapWebdavRemoteConfig(
	ctx interfaces.ActiveContext,
	uiPrinter ui.Printer,
	config blob_store_configs.ConfigWebDAV,
	discovered DiscoveredConfig,
) (err error) {
	// Validate before any HTTP work so init-webdav's
	// mutually-exclusive-auth check surfaces here too, not just at
	// store-construction time. Without this, init succeeds locally
	// and the inconsistency only manifests on the first write.
	if err = validateWebdavAuth(config); err != nil {
		return errors.Wrap(err)
	}

	httpClient, err := MakeHTTPClientForWebDAVConfig(ctx, uiPrinter, config)
	if err != nil {
		return errors.Wrap(err)
	}

	baseURL := strings.TrimRight(config.GetURL(), "/")
	configURL := baseURL + "/" + directory_layout.FileNameBlobStoreConfig

	uiPrinter.Printf("checking for existing remote config at %q...", configURL)

	headReq, err := http.NewRequestWithContext(ctx, http.MethodHead, configURL, nil)
	if err != nil {
		return errors.Wrap(err)
	}
	applyWebdavAuth(headReq, config)

	headResp, err := httpClient.Do(headReq)
	if err != nil {
		return errors.Wrapf(err, "HEAD %q", configURL)
	}
	headResp.Body.Close() //nolint:errcheck

	if headResp.StatusCode == http.StatusOK {
		return errors.Errorf(
			"remote blob_store-config already present at %q; "+
				"refusing to overwrite",
			configURL,
		)
	}
	if headResp.StatusCode != http.StatusNotFound {
		return errors.Errorf(
			"unexpected status %d checking remote config at %q",
			headResp.StatusCode, configURL,
		)
	}

	// MKCOL the base. 405 means it already exists; we don't validate
	// it's a collection here — the subsequent PUT will fail
	// distinguishably if the base is somehow a file.
	mkcolReq, err := http.NewRequestWithContext(ctx, methodMkcol, baseURL, nil)
	if err != nil {
		return errors.Wrap(err)
	}
	applyWebdavAuth(mkcolReq, config)

	mkcolResp, err := httpClient.Do(mkcolReq)
	if err != nil {
		return errors.Wrapf(err, "MKCOL %q", baseURL)
	}
	mkcolResp.Body.Close() //nolint:errcheck

	if mkcolResp.StatusCode/100 != 2 && mkcolResp.StatusCode != http.StatusMethodNotAllowed {
		return errors.Errorf(
			"MKCOL %q returned %d",
			baseURL, mkcolResp.StatusCode,
		)
	}

	configBlob := configFromDiscoveredConfig(discovered)
	typedConfig := &hyphence.TypedBlob[blob_store_configs.Config]{
		Type: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigVCurrent).TypeStruct,
		Blob: configBlob,
	}

	var encoded bytes.Buffer
	if _, err = blob_store_configs.Coder.EncodeTo(typedConfig, &encoded); err != nil {
		return errors.Wrapf(err, "failed to encode remote config")
	}

	uiPrinter.Printf("writing remote blob store config to %q...", configURL)

	putReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPut,
		configURL,
		bytes.NewReader(encoded.Bytes()),
	)
	if err != nil {
		return errors.Wrap(err)
	}
	applyWebdavAuth(putReq, config)
	putReq.ContentLength = int64(encoded.Len())

	putResp, err := httpClient.Do(putReq)
	if err != nil {
		return errors.Wrapf(err, "PUT %q", configURL)
	}
	defer putResp.Body.Close() //defer:err-checked

	if putResp.StatusCode/100 != 2 {
		return errors.Errorf(
			"PUT %q returned %d",
			configURL, putResp.StatusCode,
		)
	}

	uiPrinter.Printf("remote blob store config written successfully")
	return nil
}

// applyWebdavAuth applies the configured auth scheme to an outbound
// request. Supports basic-auth (user/password), bearer-token, and
// anonymous. TLS-client-cert auth is handled at http.Client
// construction time (in MakeHTTPClientForWebDAVConfig), not per
// request. Mutual exclusivity between basic / bearer / TLS-cert /
// anonymous is enforced at store construction.
func applyWebdavAuth(req *http.Request, config blob_store_configs.ConfigWebDAV) {
	if token := config.GetBearerToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		return
	}
	if user := config.GetUser(); user != "" {
		req.SetBasicAuth(user, config.GetPassword())
	}
}
