package inventory_archive

//go:generate dagnabit export

import (
	"crypto/sha256"
	"crypto/sha512"
	"hash"

	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"golang.org/x/crypto/blake2b"
)

const (
	DataFileMagic  = "MIAR"
	IndexFileMagic = "MIAX"
	CacheFileMagic = "MIAC"

	DataFileVersion  uint16 = 0
	IndexFileVersion uint16 = 0
	CacheFileVersion uint16 = 0

	DataFileExtension  = ".inventory_archive-v0"
	IndexFileExtension = ".inventory_archive_index-v0"
	CacheFileName      = "index_cache-v0"

	CompressionByteNone byte = 0
	CompressionByteGzip byte = 1
	CompressionByteZlib byte = 2
	CompressionByteZstd byte = 3

	FlagHasEncryption uint16 = 1 << 0
)

const (
	DataFileVersionV1  uint16 = 1
	IndexFileVersionV1 uint16 = 1
	CacheFileVersionV1 uint16 = 1

	DataFileExtensionV1  = ".inventory_archive-v1"
	IndexFileExtensionV1 = ".inventory_archive_index-v1"
	CacheFileNameV1      = "index_cache-v1"

	EntryTypeFull  byte = 0x00
	EntryTypeDelta byte = 0x01

	FlagHasDeltas         uint16 = 1 << 0
	FlagReservedCrossArch uint16 = 1 << 1
	FlagHasEncryptionV1   uint16 = 1 << 2
)

type DataEntry struct {
	Hash        []byte
	LogicalSize uint64
	StoredSize  uint64
	Data        []byte
	Offset      uint64
}

type IndexEntry struct {
	Hash       []byte
	PackOffset uint64
	StoredSize uint64
}

type CacheEntry struct {
	Hash            []byte
	ArchiveChecksum []byte
	Offset          uint64
	StoredSize      uint64
}

type DataEntryV1 struct {
	Hash        []byte
	EntryType   byte
	Encoding    byte
	LogicalSize uint64
	StoredSize  uint64 // For delta entries, this is the stored delta payload size
	Data        []byte
	Offset      uint64
	// Delta-specific fields (only set when EntryType == EntryTypeDelta)
	DeltaAlgorithm byte
	BaseHash       []byte
}

type IndexEntryV1 struct {
	Hash       []byte
	PackOffset uint64
	StoredSize uint64
	EntryType  byte
	BaseOffset uint64
}

type CacheEntryV1 struct {
	Hash            []byte
	ArchiveChecksum []byte
	Offset          uint64
	StoredSize      uint64
	EntryType       byte
	BaseOffset      uint64
}

var compressionRefToByteMap = map[string]byte{
	"madder-codec-none-v1@none": CompressionByteNone,
	"madder-codec-gzip-v1@gzip": CompressionByteGzip,
	"madder-codec-zlib-v1@zlib": CompressionByteZlib,
	"madder-codec-zstd-v1@zstd": CompressionByteZstd,
}

var byteToCompressionRefMap = map[byte]string{
	CompressionByteNone: "madder-codec-none-v1@none",
	CompressionByteGzip: "madder-codec-gzip-v1@gzip",
	CompressionByteZlib: "madder-codec-zlib-v1@zlib",
	CompressionByteZstd: "madder-codec-zstd-v1@zstd",
}

// CompressionRefToByte maps a plugin reference to the on-disk
// compression byte used in inventory_archive entries. Returns an
// error for plugin references this archive format doesn't know
// about (e.g. parametric variants like zstd-with-dict).
func CompressionRefToByte(ref string) (byte, error) {
	b, ok := compressionRefToByteMap[ref]
	if !ok {
		return 0, errors.Errorf("unsupported compression for inventory_archive: %q", ref)
	}
	return b, nil
}

// ByteToCompressionRef maps an on-disk inventory_archive compression
// byte back to a plugin reference. Used by data_reader to instantiate
// the correct decoder for each entry.
func ByteToCompressionRef(b byte) (string, error) {
	ref, ok := byteToCompressionRefMap[b]
	if !ok {
		return "", errors.Errorf("unknown compression byte: 0x%02x", b)
	}
	return ref, nil
}

type hashConstructor func() hash.Hash

var hashConstructors = map[string]hashConstructor{
	"sha256": sha256.New,
	"sha512": sha512.New,
	"blake2b256": func() hash.Hash {
		h, _ := blake2b.New256(nil)
		return h
	},
	"blake2b512": func() hash.Hash {
		h, _ := blake2b.New512(nil)
		return h
	},
}

var hashSizes = map[string]int{
	"sha256":     sha256.Size,
	"sha512":     sha512.Size,
	"blake2b256": blake2b.Size256,
	"blake2b512": blake2b.Size,
}

func newHashForFormat(formatId string) (hash.Hash, error) {
	constructor, ok := hashConstructors[formatId]
	if !ok {
		return nil, errors.Errorf("unsupported hash format: %q", formatId)
	}

	return constructor(), nil
}

func hashSizeForFormat(formatId string) (int, error) {
	size, ok := hashSizes[formatId]
	if !ok {
		return 0, errors.Errorf("unsupported hash format: %q", formatId)
	}

	return size, nil
}
