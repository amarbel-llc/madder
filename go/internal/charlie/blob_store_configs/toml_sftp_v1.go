package blob_store_configs

import (
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
)

//go:generate tommy generate
type TomlSFTPV1 struct {
	// TODO replace the below with a url scheme
	Host           string `toml:"host"`
	Port           int    `toml:"port,omitempty"`
	User           string `toml:"user"`
	Password       string `toml:"password,omitempty"`
	PrivateKeyPath string `toml:"private-key-path,omitempty"`
	RemotePath     string `toml:"remote-path"`
	KnownHostsFile string `toml:"known-hosts-file,omitempty"`

	// InstanceId is the store's uuidv7 instance identity (FDR-0010),
	// minted once at creation inside EncodeWithDigest. Empty for a legacy
	// config or one upgraded in memory from V0.
	InstanceId markl.Id `toml:"instance-id,omitempty"`
}

func (*TomlSFTPV1) GetBlobStoreType() string {
	return "sftp"
}

func (blobStoreConfig *TomlSFTPV1) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.StringVar(
		&blobStoreConfig.Host,
		"host",
		blobStoreConfig.Host,
		"SFTP server hostname",
	)

	flagSet.IntVar(
		&blobStoreConfig.Port,
		"port",
		22,
		"SFTP server port",
	)

	flagSet.StringVar(
		&blobStoreConfig.User,
		"user",
		blobStoreConfig.User,
		"SFTP username",
	)

	flagSet.StringVar(
		&blobStoreConfig.Password,
		"password",
		blobStoreConfig.Password,
		"SFTP password",
	)

	flagSet.StringVar(
		&blobStoreConfig.PrivateKeyPath,
		"private-key-path",
		blobStoreConfig.PrivateKeyPath,
		"Path to SSH private key",
	)

	flagSet.StringVar(
		&blobStoreConfig.RemotePath,
		"remote-path",
		blobStoreConfig.RemotePath,
		"Remote path for blob storage",
	)

	flagSet.StringVar(
		&blobStoreConfig.KnownHostsFile,
		"known-hosts-file",
		blobStoreConfig.KnownHostsFile,
		"Path to SSH known_hosts file (default: ~/.ssh/known_hosts)",
	)
}

func (blobStoreConfig *TomlSFTPV1) GetHost() string {
	return blobStoreConfig.Host
}

func (blobStoreConfig *TomlSFTPV1) GetPort() int {
	if blobStoreConfig.Port == 0 {
		return 22
	}
	return blobStoreConfig.Port
}

func (blobStoreConfig *TomlSFTPV1) GetUser() string {
	return blobStoreConfig.User
}

func (blobStoreConfig *TomlSFTPV1) GetPassword() string {
	return blobStoreConfig.Password
}

func (blobStoreConfig *TomlSFTPV1) GetPrivateKeyPath() string {
	return blobStoreConfig.PrivateKeyPath
}

func (blobStoreConfig *TomlSFTPV1) GetRemotePath() string {
	return blobStoreConfig.RemotePath
}

func (blobStoreConfig *TomlSFTPV1) GetKnownHostsFile() string {
	return blobStoreConfig.KnownHostsFile
}

func (blobStoreConfig *TomlSFTPV1) GetInstanceId() markl.Id {
	return blobStoreConfig.InstanceId
}

func (blobStoreConfig *TomlSFTPV1) SetInstanceId(instanceId markl.Id) {
	blobStoreConfig.InstanceId = instanceId
}
