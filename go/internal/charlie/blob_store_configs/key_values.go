package blob_store_configs

import (
	"fmt"
	"sort"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
)

// ConfigKeyValues returns a map of TOML-tag-named keys to string-formatted
// values for the given config. The set of keys depends on which interfaces the
// config implements.
func ConfigKeyValues(config Config) map[string]string {
	keyValues := make(map[string]string)

	keyValues["blob-store-type"] = config.GetBlobStoreType()

	if configHashType, ok := config.(ConfigHashType); ok {
		keyValues["hash_type-id"] = configHashType.GetDefaultHashTypeId()
		keyValues["supports-multi-hash"] = fmt.Sprint(
			configHashType.SupportsMultiHash(),
		)
	}

	if blobIOWrapper, ok := config.(domain_interfaces.BlobIOWrapper); ok {
		keyValues["encryption"] = blobIOWrapper.GetBlobEncryption().
			StringWithFormat()
	}

	if configCompressionType, ok := config.(ConfigCompressionType); ok {
		keyValues["compression-type"] = configCompressionType.GetCompressionType()
	}

	if configLocal, ok := config.(ConfigLocalHashBucketed); ok {
		keyValues["hash_buckets"] = fmt.Sprint(
			configLocal.GetHashBuckets(),
		)
	}

	if configArchive, ok := config.(ConfigInventoryArchive); ok {
		keyValues["loose-blob-store-id"] = configArchive.
			GetLooseBlobStoreId().String()
		keyValues["max-pack-size"] = fmt.Sprint(
			configArchive.GetMaxPackSize(),
		)
	}

	if configDelta, ok := config.(DeltaConfigImmutable); ok {
		keyValues["delta.enabled"] = fmt.Sprint(
			configDelta.GetDeltaEnabled(),
		)
		keyValues["delta.algorithm"] = configDelta.GetDeltaAlgorithm()
		keyValues["delta.min-blob-size"] = fmt.Sprint(
			configDelta.GetDeltaMinBlobSize(),
		)
		keyValues["delta.max-blob-size"] = fmt.Sprint(
			configDelta.GetDeltaMaxBlobSize(),
		)
		keyValues["delta.size-ratio"] = fmt.Sprint(
			configDelta.GetDeltaSizeRatio(),
		)
	}

	if configSFTP, ok := config.(ConfigSFTPConfigExplicit); ok {
		keyValues["host"] = configSFTP.GetHost()
		keyValues["port"] = fmt.Sprint(configSFTP.GetPort())
		keyValues["user"] = configSFTP.GetUser()
		keyValues["private-key-path"] = configSFTP.GetPrivateKeyPath()
		keyValues["remote-path"] = configSFTP.GetRemotePath()
	} else if configSFTPRemote, ok := config.(ConfigSFTPRemotePath); ok {
		keyValues["remote-path"] = configSFTPRemote.GetRemotePath()
	}

	// Per the WebDAV design and ADR 0005: surface URL, user, and the
	// non-secret TLS material (cert path + CA path + server-name +
	// insecure-skip-verify); NEVER surface password, bearer-token, or
	// tls-client-key-path. Redaction is unit-pinned.
	if configWebDAV, ok := config.(ConfigWebDAV); ok {
		keyValues["url"] = configWebDAV.GetURL()
		keyValues["user"] = configWebDAV.GetUser()
		if v := configWebDAV.GetTLSClientCertPath(); v != "" {
			keyValues["tls-client-cert-path"] = v
		}
		if v := configWebDAV.GetTLSCAPath(); v != "" {
			keyValues["tls-ca-path"] = v
		}
		if v := configWebDAV.GetTLSServerName(); v != "" {
			keyValues["tls-server-name"] = v
		}
		if configWebDAV.GetTLSInsecureSkipVerify() {
			keyValues["tls-insecure-skip-verify"] = "true"
		}
	}

	return keyValues
}

// ConfigKeyNames returns sorted key names available for a config.
func ConfigKeyNames(config Config) []string {
	keyValues := ConfigKeyValues(config)
	keys := make([]string, 0, len(keyValues))

	for key := range keyValues {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}
