package blob_store_configs

import (
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
)

//go:generate tommy generate
type TomlS3V1 struct {
	Endpoint           string `toml:"endpoint,omitempty"`
	Region             string `toml:"region,omitempty"`
	Bucket             string `toml:"bucket"`
	Prefix             string `toml:"prefix,omitempty"`
	AccessKeyId        string `toml:"access-key-id,omitempty"`
	SecretAccessKey    string `toml:"secret-access-key,omitempty"`
	SessionToken       string `toml:"session-token,omitempty"`
	UsePathStyle       bool   `toml:"use-path-style,omitempty"`
	InsecureSkipVerify bool   `toml:"insecure-skip-tls-verify,omitempty"`

	// InstanceId is the store's uuidv7 instance identity (FDR-0010),
	// minted once at creation inside EncodeWithDigest. Empty for a legacy
	// config or one upgraded in memory from V0.
	InstanceId markl.Id `toml:"instance-id,omitempty"`
}

func (*TomlS3V1) GetBlobStoreType() string {
	return "s3"
}

func (blobStoreConfig *TomlS3V1) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.StringVar(
		&blobStoreConfig.Endpoint,
		"endpoint",
		blobStoreConfig.Endpoint,
		"S3 endpoint URL (empty = AWS default resolution; "+
			"e.g. http://localhost:9000 for MinIO)",
	)

	flagSet.StringVar(
		&blobStoreConfig.Region,
		"region",
		blobStoreConfig.Region,
		"AWS region (e.g. us-east-1)",
	)

	flagSet.StringVar(
		&blobStoreConfig.Bucket,
		"bucket",
		blobStoreConfig.Bucket,
		"S3 bucket name",
	)

	flagSet.StringVar(
		&blobStoreConfig.Prefix,
		"prefix",
		blobStoreConfig.Prefix,
		"Key prefix inside the bucket (no leading slash)",
	)

	flagSet.StringVar(
		&blobStoreConfig.AccessKeyId,
		"access-key-id",
		blobStoreConfig.AccessKeyId,
		"AWS access key id",
	)

	flagSet.StringVar(
		&blobStoreConfig.SecretAccessKey,
		"secret-access-key",
		blobStoreConfig.SecretAccessKey,
		"AWS secret access key",
	)

	flagSet.StringVar(
		&blobStoreConfig.SessionToken,
		"session-token",
		blobStoreConfig.SessionToken,
		"AWS session token (for temporary STS credentials)",
	)

	flagSet.BoolVar(
		&blobStoreConfig.UsePathStyle,
		"use-path-style",
		blobStoreConfig.UsePathStyle,
		"Use path-style addressing (required for MinIO/Ceph/localhost)",
	)

	flagSet.BoolVar(
		&blobStoreConfig.InsecureSkipVerify,
		"insecure-skip-tls-verify",
		blobStoreConfig.InsecureSkipVerify,
		"Skip TLS certificate verification (development only)",
	)
}

func (blobStoreConfig *TomlS3V1) GetEndpoint() string {
	return blobStoreConfig.Endpoint
}

func (blobStoreConfig *TomlS3V1) GetRegion() string {
	return blobStoreConfig.Region
}

func (blobStoreConfig *TomlS3V1) GetBucket() string {
	return blobStoreConfig.Bucket
}

func (blobStoreConfig *TomlS3V1) GetPrefix() string {
	return blobStoreConfig.Prefix
}

func (blobStoreConfig *TomlS3V1) GetAccessKeyId() string {
	return blobStoreConfig.AccessKeyId
}

func (blobStoreConfig *TomlS3V1) GetSecretAccessKey() string {
	return blobStoreConfig.SecretAccessKey
}

func (blobStoreConfig *TomlS3V1) GetSessionToken() string {
	return blobStoreConfig.SessionToken
}

func (blobStoreConfig *TomlS3V1) GetUsePathStyle() bool {
	return blobStoreConfig.UsePathStyle
}

func (blobStoreConfig *TomlS3V1) GetInsecureSkipVerify() bool {
	return blobStoreConfig.InsecureSkipVerify
}

func (blobStoreConfig *TomlS3V1) GetInstanceId() markl.Id {
	return blobStoreConfig.InstanceId
}

func (blobStoreConfig *TomlS3V1) SetInstanceId(instanceId markl.Id) {
	blobStoreConfig.InstanceId = instanceId
}
