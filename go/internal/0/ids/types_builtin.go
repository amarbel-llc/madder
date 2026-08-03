package ids

import "fmt"

const (
	TypeTomlBlobStoreConfigSftpExplicitV0           = "!toml-blob_store_config_sftp-explicit-v0"
	TypeTomlBlobStoreConfigSftpExplicitV1           = "!toml-blob_store_config_sftp-explicit-v1"
	TypeTomlBlobStoreConfigSftpViaSSHConfigV0       = "!toml-blob_store_config_sftp-ssh_config-v0"
	TypeTomlBlobStoreConfigSftpViaSSHConfigV1       = "!toml-blob_store_config_sftp-ssh_config-v1"
	TypeTomlBlobStoreConfigWebdavV0                 = "!toml-blob_store_config_webdav-v0"
	TypeTomlBlobStoreConfigWebdavV1                 = "!toml-blob_store_config_webdav-v1"
	TypeTomlBlobStoreConfigS3V0                     = "!toml-blob_store_config_s3-v0"
	TypeTomlBlobStoreConfigS3V1                     = "!toml-blob_store_config_s3-v1"
	TypeTomlBlobStoreConfigV1                       = "!toml-blob_store_config-v1"
	TypeTomlBlobStoreConfigV2                       = "!toml-blob_store_config-v2"
	TypeTomlBlobStoreConfigV3                       = "!toml-blob_store_config-v3"
	TypeTomlBlobStoreConfigV4                       = "!toml-blob_store_config-v4"
	TypeTomlBlobStoreConfigPointerV0                = "!toml-blob_store_config-pointer-v0"
	TypeTomlBlobStoreConfigPointerV1                = "!toml-blob_store_config-pointer-v1"
	TypeTomlBlobStoreConfigPointerV2                = "!toml-blob_store_config-pointer-v2"
	TypeTomlBlobStoreConfigInventoryArchiveV0       = "!toml-blob_store_config-inventory_archive-v0"
	TypeTomlBlobStoreConfigInventoryArchiveV1       = "!toml-blob_store_config-inventory_archive-v1"
	TypeTomlBlobStoreConfigInventoryArchiveV2       = "!toml-blob_store_config-inventory_archive-v2"
	TypeTomlBlobStoreConfigInventoryArchiveV3       = "!toml-blob_store_config-inventory_archive-v3"
	TypeTomlBlobStoreConfigInventoryArchiveVCurrent = TypeTomlBlobStoreConfigInventoryArchiveV3
	TypeTomlBlobStoreConfigVCurrent                 = TypeTomlBlobStoreConfigV4
	TypeTomlBlobStoreConfigMultiV0                  = "!toml-blob_store_config-multi-v0"
	TypeTomlBlobStoreConfigMultiV1                  = "!toml-blob_store_config-multi-v1"
)

type BuiltinType struct {
	TypeStruct
}

var allMap map[TypeStruct]BuiltinType

func init() {
	allMap = make(map[TypeStruct]BuiltinType)

	for _, tipeString := range []string{
		TypeTomlBlobStoreConfigV1,
		TypeTomlBlobStoreConfigV2,
		TypeTomlBlobStoreConfigV3,
		TypeTomlBlobStoreConfigV4,
		TypeTomlBlobStoreConfigPointerV0,
		TypeTomlBlobStoreConfigPointerV1,
		TypeTomlBlobStoreConfigPointerV2,
		TypeTomlBlobStoreConfigInventoryArchiveV0,
		TypeTomlBlobStoreConfigInventoryArchiveV1,
		TypeTomlBlobStoreConfigInventoryArchiveV2,
		TypeTomlBlobStoreConfigInventoryArchiveV3,
		TypeTomlBlobStoreConfigSftpExplicitV0,
		TypeTomlBlobStoreConfigSftpExplicitV1,
		TypeTomlBlobStoreConfigSftpViaSSHConfigV0,
		TypeTomlBlobStoreConfigSftpViaSSHConfigV1,
		TypeTomlBlobStoreConfigWebdavV0,
		TypeTomlBlobStoreConfigWebdavV1,
		TypeTomlBlobStoreConfigS3V0,
		TypeTomlBlobStoreConfigS3V1,
		TypeTomlBlobStoreConfigMultiV0,
		TypeTomlBlobStoreConfigMultiV1,
	} {
		ts := MustTypeStruct(tipeString)
		allMap[ts] = BuiltinType{TypeStruct: ts}
	}
}

func GetOrPanic(idString string) BuiltinType {
	tipe := MustTypeStruct(idString)
	bt, ok := allMap[tipe]

	if !ok {
		panic(fmt.Sprintf("no builtin type found for %q", tipe))
	}

	return bt
}
