package blob_store_configs

import (
	"code.linenisgreat.com/madder/go/internal/0/ids"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
)

//go:generate tommy generate
type TomlSFTPViaSSHConfigV0 struct {
	TomlUriV0
	KnownHostsFile string `toml:"known-hosts-file,omitempty"`
}

func (TomlSFTPViaSSHConfigV0) GetBlobStoreType() string {
	return "sftp"
}

func (config *TomlSFTPViaSSHConfigV0) SetFlagDefinitions(
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

func (config TomlSFTPViaSSHConfigV0) GetKnownHostsFile() string {
	return config.KnownHostsFile
}

func (config TomlSFTPViaSSHConfigV0) GetRemotePath() string {
	uri := config.TomlUriV0.GetUri()
	return uri.GetUrl().Path
}

// Upgrade migrates a V0 sftp-via-ssh-config to V1 (adds the FDR-0010
// instance id). It does NOT mint — upgrade runs on read, and lazy-minting
// is forbidden — so the upgraded V1 carries an empty InstanceId until the
// store is copy-migrated.
func (config TomlSFTPViaSSHConfigV0) Upgrade() (Config, ids.TypeStruct) {
	upgraded := &TomlSFTPViaSSHConfigV1{
		TomlUriV0:      config.TomlUriV0,
		KnownHostsFile: config.KnownHostsFile,
	}

	return upgraded, ids.GetOrPanic(
		ids.TypeTomlBlobStoreConfigSftpViaSSHConfigV1,
	).TypeStruct
}
