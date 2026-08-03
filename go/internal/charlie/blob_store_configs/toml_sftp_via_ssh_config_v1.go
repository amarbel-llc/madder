package blob_store_configs

import (
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
)

//go:generate tommy generate
type TomlSFTPViaSSHConfigV1 struct {
	TomlUriV0
	KnownHostsFile string `toml:"known-hosts-file,omitempty"`

	// InstanceId is the store's uuidv7 instance identity (FDR-0010),
	// minted once at creation inside EncodeWithDigest. Empty for a legacy
	// config or one upgraded in memory from V0.
	InstanceId markl.Id `toml:"instance-id,omitempty"`
}

func (TomlSFTPViaSSHConfigV1) GetBlobStoreType() string {
	return "sftp"
}

func (config *TomlSFTPViaSSHConfigV1) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	config.TomlUriV0.SetFlagDefinitions(flagSet)

	flagSet.StringVar(
		&config.KnownHostsFile,
		"known-hosts-file",
		config.KnownHostsFile,
		"Path to SSH known_hosts file (default: ~/.ssh/known_hosts)",
	)
}

func (config TomlSFTPViaSSHConfigV1) GetKnownHostsFile() string {
	return config.KnownHostsFile
}

func (config TomlSFTPViaSSHConfigV1) GetRemotePath() string {
	uri := config.TomlUriV0.GetUri()
	return uri.GetUrl().Path
}

func (config TomlSFTPViaSSHConfigV1) GetInstanceId() markl.Id {
	return config.InstanceId
}

func (config *TomlSFTPViaSSHConfigV1) SetInstanceId(instanceId markl.Id) {
	config.InstanceId = instanceId
}
