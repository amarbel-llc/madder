package blob_store_configs

import (
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
)

//go:generate tommy generate
type TomlWebDAVV0 struct {
	URL                   string `toml:"url"`
	User                  string `toml:"user,omitempty"`
	Password              string `toml:"password,omitempty"`
	BearerToken           string `toml:"bearer-token,omitempty"`
	TLSClientCertPath     string `toml:"tls-client-cert-path,omitempty"`
	TLSClientKeyPath      string `toml:"tls-client-key-path,omitempty"`
	TLSCAPath             string `toml:"tls-ca-path,omitempty"`
	TLSServerName         string `toml:"tls-server-name,omitempty"`
	TLSInsecureSkipVerify bool   `toml:"tls-insecure-skip-verify,omitempty"`
}

func (*TomlWebDAVV0) GetBlobStoreType() string {
	return "webdav"
}

func (blobStoreConfig *TomlWebDAVV0) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.StringVar(
		&blobStoreConfig.URL,
		"url",
		blobStoreConfig.URL,
		"WebDAV server URL (e.g. https://host/path/)",
	)

	flagSet.StringVar(
		&blobStoreConfig.User,
		"user",
		blobStoreConfig.User,
		"WebDAV username (basic auth)",
	)

	flagSet.StringVar(
		&blobStoreConfig.Password,
		"password",
		blobStoreConfig.Password,
		"WebDAV password (basic auth)",
	)

	flagSet.StringVar(
		&blobStoreConfig.BearerToken,
		"bearer-token",
		blobStoreConfig.BearerToken,
		"WebDAV bearer token (sent as 'Authorization: Bearer <token>')",
	)

	flagSet.StringVar(
		&blobStoreConfig.TLSClientCertPath,
		"tls-client-cert-path",
		blobStoreConfig.TLSClientCertPath,
		"path to TLS client certificate (PEM)",
	)

	flagSet.StringVar(
		&blobStoreConfig.TLSClientKeyPath,
		"tls-client-key-path",
		blobStoreConfig.TLSClientKeyPath,
		"path to TLS client private key (PEM)",
	)

	flagSet.StringVar(
		&blobStoreConfig.TLSCAPath,
		"tls-ca-path",
		blobStoreConfig.TLSCAPath,
		"path to TLS CA bundle (PEM); pins the server's expected CA",
	)

	flagSet.StringVar(
		&blobStoreConfig.TLSServerName,
		"tls-server-name",
		blobStoreConfig.TLSServerName,
		"override the TLS ServerName (SNI / certificate verification host)",
	)

	flagSet.BoolVar(
		&blobStoreConfig.TLSInsecureSkipVerify,
		"tls-insecure-skip-verify",
		blobStoreConfig.TLSInsecureSkipVerify,
		"skip server cert verification (DANGEROUS; debug-only)",
	)
}

func (blobStoreConfig *TomlWebDAVV0) GetURL() string {
	return blobStoreConfig.URL
}

func (blobStoreConfig *TomlWebDAVV0) GetUser() string {
	return blobStoreConfig.User
}

func (blobStoreConfig *TomlWebDAVV0) GetPassword() string {
	return blobStoreConfig.Password
}

func (blobStoreConfig *TomlWebDAVV0) GetBearerToken() string {
	return blobStoreConfig.BearerToken
}

func (blobStoreConfig *TomlWebDAVV0) GetTLSClientCertPath() string {
	return blobStoreConfig.TLSClientCertPath
}

func (blobStoreConfig *TomlWebDAVV0) GetTLSClientKeyPath() string {
	return blobStoreConfig.TLSClientKeyPath
}

func (blobStoreConfig *TomlWebDAVV0) GetTLSCAPath() string {
	return blobStoreConfig.TLSCAPath
}

func (blobStoreConfig *TomlWebDAVV0) GetTLSServerName() string {
	return blobStoreConfig.TLSServerName
}

func (blobStoreConfig *TomlWebDAVV0) GetTLSInsecureSkipVerify() bool {
	return blobStoreConfig.TLSInsecureSkipVerify
}
