# blob_store_configs

Configuration types and interfaces for blob storage backends.

## Key Types

- `Config`: Base interface for blob store configurations
- `ConfigMutable`: Writable configuration interface
- `ConfigLocalHashBucketed`: Configuration for hash-bucketed local storage
- `ConfigSFTPUri`, `ConfigSFTPConfigExplicit`: SFTP remote storage
  configurations
- `TypedConfig`, `TypedMutableConfig`: Type-safe config wrappers

## Versions

Each store config type is independently versioned; the newest version of
each is what `Default()` and the `init-*` commands write. Every current
version carries a uuid `instance-id` in the config body (FDR-0010), minted
once at creation by `EncodeWithDigest`. The set:

- Local hash-bucketed: `TomlLocalHashBucketedV1`, `TomlLocalHashBucketedV2`,
  `TomlV3`, `TomlV4` (current)
- Inventory-archive: `TomlInventoryArchiveV0`–`V3` (current: V3)
- SFTP: `TomlSFTPV0`/`V1` (current: V1); `TomlSFTPViaSSHConfigV0`/`V1`
  (current: V1)
- S3: `TomlS3V0`/`V1` (current: V1)
- WebDAV: `TomlWebDAVV0`/`V1` (current: V1)
- Multi: `TomlMultiV0`/`V1` (current: V1)
- Pointer: `TomlPointerV0`/`V1`/`V2` (current: V2)
- `TomlUriV0`: an embedded value type (used by the sftp-ssh config), not a
  standalone store — it carries no type-id and no instance-id.

## Features

- Default hash type: BLAKE2b-256
- Hash bucketing with configurable depth (default: 2-char buckets)
- Compression and encryption support via interfaces
- Internal file locking support
