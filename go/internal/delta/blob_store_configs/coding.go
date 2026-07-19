package blob_store_configs

import (
	"code.linenisgreat.com/hyphence/go/hyphence"
	"code.linenisgreat.com/madder/go/internal/0/ids"
	charlie_bsc "code.linenisgreat.com/madder/go/internal/charlie/blob_store_configs"
	"github.com/amarbel-llc/piggy/go/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

var Coder = hyphence.CoderToTypedBlob[ids.TypeStruct, *ids.TypeStruct, markl.Id, *markl.Id, Config]{
	Metadata: hyphence.TypedMetadataCoder[ids.TypeStruct, *ids.TypeStruct, markl.Id, *markl.Id, Config]{},
	Blob: hyphence.CoderTypeMapWithoutType[ids.TypeStruct, *ids.TypeStruct, markl.Id, *markl.Id, Config](
		map[string]interfaces.CoderBufferedReadWriter[*Config]{
			ids.TypeTomlBlobStoreConfigV1: hyphence.CoderTommy[
				Config,
				*Config,
			]{
				Decode: func(b []byte) (Config, error) {
					doc, err := charlie_bsc.DecodeTomlLocalHashBucketedV1(b)
					if err != nil {
						return nil, err
					}
					return doc.Data(), nil
				},
				Encode: func(cfg Config) ([]byte, error) {
					doc, err := charlie_bsc.DecodeTomlLocalHashBucketedV1(nil)
					if err != nil {
						return nil, err
					}
					switch v := cfg.(type) {
					case *TomlLocalHashBucketedV1:
						*doc.Data() = *v
					case TomlLocalHashBucketedV1:
						*doc.Data() = v
					}
					return doc.Encode()
				},
			},
			ids.TypeTomlBlobStoreConfigV2: hyphence.CoderTommy[
				Config,
				*Config,
			]{
				Decode: func(b []byte) (Config, error) {
					doc, err := charlie_bsc.DecodeTomlLocalHashBucketedV2(b)
					if err != nil {
						return nil, err
					}
					return doc.Data(), nil
				},
				Encode: func(cfg Config) ([]byte, error) {
					doc, err := charlie_bsc.DecodeTomlLocalHashBucketedV2(nil)
					if err != nil {
						return nil, err
					}
					switch v := cfg.(type) {
					case *TomlLocalHashBucketedV2:
						*doc.Data() = *v
					case TomlLocalHashBucketedV2:
						*doc.Data() = v
					}
					return doc.Encode()
				},
			},
			ids.TypeTomlBlobStoreConfigV3: hyphence.CoderTommy[
				Config,
				*Config,
			]{
				Decode: func(b []byte) (Config, error) {
					doc, err := charlie_bsc.DecodeTomlV3(b)
					if err != nil {
						return nil, err
					}
					return doc.Data(), nil
				},
				Encode: func(cfg Config) ([]byte, error) {
					doc, err := charlie_bsc.DecodeTomlV3(nil)
					if err != nil {
						return nil, err
					}
					switch v := cfg.(type) {
					case *TomlV3:
						*doc.Data() = *v
					case TomlV3:
						*doc.Data() = v
					}
					return doc.Encode()
				},
			},
			ids.TypeTomlBlobStoreConfigSftpExplicitV0: hyphence.CoderTommy[
				Config,
				*Config,
			]{
				Decode: func(b []byte) (Config, error) {
					doc, err := charlie_bsc.DecodeTomlSFTPV0(b)
					if err != nil {
						return nil, err
					}
					return doc.Data(), nil
				},
				Encode: func(cfg Config) ([]byte, error) {
					doc, err := charlie_bsc.DecodeTomlSFTPV0(nil)
					if err != nil {
						return nil, err
					}
					if v, ok := cfg.(*TomlSFTPV0); ok {
						*doc.Data() = *v
					}
					return doc.Encode()
				},
			},
			ids.TypeTomlBlobStoreConfigWebdavV0: hyphence.CoderTommy[
				Config,
				*Config,
			]{
				Decode: func(b []byte) (Config, error) {
					doc, err := charlie_bsc.DecodeTomlWebDAVV0(b)
					if err != nil {
						return nil, err
					}
					return doc.Data(), nil
				},
				Encode: func(cfg Config) ([]byte, error) {
					doc, err := charlie_bsc.DecodeTomlWebDAVV0(nil)
					if err != nil {
						return nil, err
					}
					if v, ok := cfg.(*TomlWebDAVV0); ok {
						*doc.Data() = *v
					}
					return doc.Encode()
				},
			},
			ids.TypeTomlBlobStoreConfigS3V0: hyphence.CoderTommy[
				Config,
				*Config,
			]{
				Decode: func(b []byte) (Config, error) {
					doc, err := charlie_bsc.DecodeTomlS3V0(b)
					if err != nil {
						return nil, err
					}
					return doc.Data(), nil
				},
				Encode: func(cfg Config) ([]byte, error) {
					doc, err := charlie_bsc.DecodeTomlS3V0(nil)
					if err != nil {
						return nil, err
					}
					if v, ok := cfg.(*TomlS3V0); ok {
						*doc.Data() = *v
					}
					return doc.Encode()
				},
			},
			ids.TypeTomlBlobStoreConfigSftpViaSSHConfigV0: hyphence.CoderTommy[
				Config,
				*Config,
			]{
				Decode: func(b []byte) (Config, error) {
					doc, err := charlie_bsc.DecodeTomlSFTPViaSSHConfigV0(b)
					if err != nil {
						return nil, err
					}
					return doc.Data(), nil
				},
				Encode: func(cfg Config) ([]byte, error) {
					doc, err := charlie_bsc.DecodeTomlSFTPViaSSHConfigV0(nil)
					if err != nil {
						return nil, err
					}
					if v, ok := cfg.(*TomlSFTPViaSSHConfigV0); ok {
						*doc.Data() = *v
					}
					return doc.Encode()
				},
			},
			ids.TypeTomlBlobStoreConfigPointerV0: hyphence.CoderTommy[
				Config,
				*Config,
			]{
				Decode: func(b []byte) (Config, error) {
					doc, err := charlie_bsc.DecodeTomlPointerV0(b)
					if err != nil {
						return nil, err
					}
					return doc.Data(), nil
				},
				Encode: func(cfg Config) ([]byte, error) {
					doc, err := charlie_bsc.DecodeTomlPointerV0(nil)
					if err != nil {
						return nil, err
					}
					switch v := cfg.(type) {
					case *TomlPointerV0:
						*doc.Data() = *v
					case TomlPointerV0:
						*doc.Data() = v
					}
					return doc.Encode()
				},
			},
			ids.TypeTomlBlobStoreConfigPointerV1: hyphence.CoderTommy[
				Config,
				*Config,
			]{
				Decode: func(b []byte) (Config, error) {
					doc, err := charlie_bsc.DecodeTomlPointerV1(b)
					if err != nil {
						return nil, err
					}
					return doc.Data(), nil
				},
				Encode: func(cfg Config) ([]byte, error) {
					doc, err := charlie_bsc.DecodeTomlPointerV1(nil)
					if err != nil {
						return nil, err
					}
					switch v := cfg.(type) {
					case *TomlPointerV1:
						*doc.Data() = *v
					case TomlPointerV1:
						*doc.Data() = v
					}
					return doc.Encode()
				},
			},
			ids.TypeTomlBlobStoreConfigMultiV0: hyphence.CoderTommy[
				Config,
				*Config,
			]{
				// FDR-0009: the generated DecodeTomlMultiV0 calls
				// TomlMultiV0.Validate() internally, so a hand-edited
				// config with a bare (non-digest-bearing) reference fails
				// to decode here — no extra Validate() call is needed.
				Decode: func(b []byte) (Config, error) {
					doc, err := charlie_bsc.DecodeTomlMultiV0(b)
					if err != nil {
						return nil, err
					}
					return doc.Data(), nil
				},
				Encode: func(cfg Config) ([]byte, error) {
					doc, err := charlie_bsc.DecodeTomlMultiV0(nil)
					if err != nil {
						return nil, err
					}
					switch v := cfg.(type) {
					case *TomlMultiV0:
						*doc.Data() = *v
					case TomlMultiV0:
						*doc.Data() = v
					}
					return doc.Encode()
				},
			},
			ids.TypeTomlBlobStoreConfigInventoryArchiveV0: hyphence.CoderTommy[
				Config,
				*Config,
			]{
				Decode: func(b []byte) (Config, error) {
					doc, err := charlie_bsc.DecodeTomlInventoryArchiveV0(b)
					if err != nil {
						return nil, err
					}
					return doc.Data(), nil
				},
				Encode: func(cfg Config) ([]byte, error) {
					doc, err := charlie_bsc.DecodeTomlInventoryArchiveV0(nil)
					if err != nil {
						return nil, err
					}
					switch v := cfg.(type) {
					case *TomlInventoryArchiveV0:
						*doc.Data() = *v
					case TomlInventoryArchiveV0:
						*doc.Data() = v
					}
					return doc.Encode()
				},
			},
			ids.TypeTomlBlobStoreConfigInventoryArchiveV1: hyphence.CoderTommy[
				Config,
				*Config,
			]{
				Decode: func(b []byte) (Config, error) {
					doc, err := charlie_bsc.DecodeTomlInventoryArchiveV1(b)
					if err != nil {
						return nil, err
					}
					return doc.Data(), nil
				},
				Encode: func(cfg Config) ([]byte, error) {
					doc, err := charlie_bsc.DecodeTomlInventoryArchiveV1(nil)
					if err != nil {
						return nil, err
					}
					switch v := cfg.(type) {
					case *TomlInventoryArchiveV1:
						*doc.Data() = *v
					case TomlInventoryArchiveV1:
						*doc.Data() = v
					}
					return doc.Encode()
				},
			},
			ids.TypeTomlBlobStoreConfigInventoryArchiveV2: hyphence.CoderTommy[
				Config,
				*Config,
			]{
				Decode: func(b []byte) (Config, error) {
					doc, err := charlie_bsc.DecodeTomlInventoryArchiveV2(b)
					if err != nil {
						return nil, err
					}
					return doc.Data(), nil
				},
				Encode: func(cfg Config) ([]byte, error) {
					doc, err := charlie_bsc.DecodeTomlInventoryArchiveV2(nil)
					if err != nil {
						return nil, err
					}
					switch v := cfg.(type) {
					case *TomlInventoryArchiveV2:
						*doc.Data() = *v
					case TomlInventoryArchiveV2:
						*doc.Data() = v
					}
					return doc.Encode()
				},
			},
		},
	),
}
