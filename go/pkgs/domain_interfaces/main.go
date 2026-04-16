package domain_interfaces

import (
	internal "github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
)

// Blob store types
type (
	BlobIOWrapper          = internal.BlobIOWrapper
	BlobIOWrapperGetter    = internal.BlobIOWrapperGetter
	ReadAtSeeker           = internal.ReadAtSeeker
	BlobReader             = internal.BlobReader
	BlobWriter             = internal.BlobWriter
	BlobReaderFactory      = internal.BlobReaderFactory
	BlobWriterFactory      = internal.BlobWriterFactory
	BlobAccess             = internal.BlobAccess
	NamedBlobAccess        = internal.NamedBlobAccess
	BlobStore              = internal.BlobStore
	BlobPool[BLOB any]     = internal.BlobPool[BLOB]
	SavedBlobFormatter     = internal.SavedBlobFormatter
	BlobForeignDigestAdder = internal.BlobForeignDigestAdder
)

// Markl types
type (
	MarklFormat       = internal.MarklFormat
	FormatHash        = internal.FormatHash
	MarklFormatGetter = internal.MarklFormatGetter
	Hash              = internal.Hash
	MarklId           = internal.MarklId
	MarklIdMutable    = internal.MarklIdMutable
	MarklIdGetter     = internal.MarklIdGetter
	DigestWriteMap    = internal.DigestWriteMap
)

// Config types
type (
	ConfigDryRunGetter    = internal.ConfigDryRunGetter
	ConfigDryRunSetter    = internal.ConfigDryRunSetter
	MutableConfigDryRun   = internal.MutableConfigDryRun
	Config                = internal.Config
	MutableConfig         = internal.MutableConfig
	CLIConfigProvider     = internal.CLIConfigProvider
	RepoCLIConfigProvider = internal.RepoCLIConfigProvider
)
