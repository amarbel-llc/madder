package blob_store_configs

import (
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
)

//go:generate tommy generate
type TomlWebDAVV0 struct {
	URL      string `toml:"url"`
	User     string `toml:"user,omitempty"`
	Password string `toml:"password,omitempty"`
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
