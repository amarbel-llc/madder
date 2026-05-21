package blob_store_configs

import (
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/values"
)

// TODO move to a config_common package
//
//go:generate tommy generate
type TomlUriV0 struct {
	Uri values.Uri `toml:"uri"`
}

func (config *TomlUriV0) SetFlagDefinitions(flagSet interfaces.CLIFlagDefinitions) {
	flagSet.Var(
		&config.Uri,
		"uri",
		"SFTP server hostname",
	)
}

func (config *TomlUriV0) GetUri() values.Uri {
	return config.Uri
}
