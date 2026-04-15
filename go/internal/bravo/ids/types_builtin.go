package ids

import "fmt"

const (
	TypeTomlBlobStoreConfigSftpExplicitV0           = "!toml-blob_store_config_sftp-explicit-v0"
	TypeTomlBlobStoreConfigSftpViaSSHConfigV0       = "!toml-blob_store_config_sftp-ssh_config-v0"
	TypeTomlBlobStoreConfigV1                       = "!toml-blob_store_config-v1"
	TypeTomlBlobStoreConfigV2                       = "!toml-blob_store_config-v2"
	TypeTomlBlobStoreConfigV3                       = "!toml-blob_store_config-v3"
	TypeTomlBlobStoreConfigPointerV0                = "!toml-blob_store_config-pointer-v0"
	TypeTomlBlobStoreConfigInventoryArchiveV0       = "!toml-blob_store_config-inventory_archive-v0"
	TypeTomlBlobStoreConfigInventoryArchiveV1       = "!toml-blob_store_config-inventory_archive-v1"
	TypeTomlBlobStoreConfigInventoryArchiveV2       = "!toml-blob_store_config-inventory_archive-v2"
	TypeTomlBlobStoreConfigInventoryArchiveVCurrent = TypeTomlBlobStoreConfigInventoryArchiveV2
	TypeTomlBlobStoreConfigVCurrent                 = TypeTomlBlobStoreConfigV3
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
		TypeTomlBlobStoreConfigPointerV0,
		TypeTomlBlobStoreConfigInventoryArchiveV0,
		TypeTomlBlobStoreConfigInventoryArchiveV1,
		TypeTomlBlobStoreConfigInventoryArchiveV2,
		TypeTomlBlobStoreConfigSftpExplicitV0,
		TypeTomlBlobStoreConfigSftpViaSSHConfigV0,
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
