package blob_stores

import (
	"net/http"

	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ui"
)

// MakeHTTPClientForWebDAVConfig builds an http.Client for a WebDAV
// blob-store config. v0 supports basic auth and anonymous; bearer
// and TLS-client-cert support land in a follow-up commit.
func MakeHTTPClientForWebDAVConfig(
	_ interfaces.ActiveContext,
	_ ui.Printer,
	_ blob_store_configs.ConfigWebDAV,
) (*http.Client, error) {
	return &http.Client{}, nil
}
