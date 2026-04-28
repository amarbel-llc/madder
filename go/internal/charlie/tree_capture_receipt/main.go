// Package tree_capture_receipt encodes and decodes the per-store-group
// receipt blob produced by `madder tree-capture` (and consumed by
// `madder tree-restore`).
//
// The receipt is a hyphence-wrapped NDJSON document: an optional
// store-hint metadata line plus a single type-tag line, followed by
// one JSON object per filesystem entry. Entries are sorted by
// (Root, Path) so equivalent inputs yield byte-identical receipts —
// which means equivalent inputs yield identical receipt blob IDs.
//
// The package follows the dodder horizontal-versioning convention:
// each wire-format version (currently only V1) is a self-contained
// data shape with its own Read/Write functions and entry struct
// (EntryV1). Future versions land as sibling files (v2.go, v2_io.go)
// without disturbing existing readers; the type-id discriminator on
// the hyphence metadata block is the dispatch key.
package tree_capture_receipt

// Blob is the lowest-common-denominator return type for a parsed
// receipt across all wire versions. A successful parse returns a
// concrete *V1 (or *V2, etc.); the dispatcher narrows by type-id.
type Blob any

// TypeTagV1 is the hyphence `! type-string` written at the top of
// every v1 receipt.
const TypeTagV1 = "madder-tree_capture-receipt-v1"
