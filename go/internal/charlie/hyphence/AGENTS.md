# hyphence

I/O for hyphence (hyphen-fence) delimited format (metadata/content separation).

## Key Types

- `MetadataWriterTo`: Writer interface with metadata content check

## Format

Uses `---` as boundary between metadata and content sections.

## Format-only document model

Slice 1 of the `hyphence` CLI added a format-only document model
that sits next to the type-aware Coder/Reader machinery and shares
the package's `Reader` boundary scanner. These types operate on
RFC 0001 syntax only — no body decoding, no type-tag dispatch.

- `Document` / `MetadataLine`: structured representation of a
  parsed metadata section. Bodies are never buffered.
- `MetadataStreamer`: passthrough metadata consumer used by
  `hyphence meta`.
- `MetadataBuilder`: parses metadata lines into a `Document` for
  `hyphence format`.
- `MetadataValidator`: strict per-line RFC 0001 checker used by
  `hyphence validate`.
- `FormatBodyEmitter`: blob consumer for `hyphence format` —
  emits canonicalized metadata then streams body bytes.
- `Canonicalize`: sort metadata per RFC §Canonical Line Order.
- Sentinels: `ErrMalformedMetadataLine`, `ErrInvalidPrefix`,
  `ErrInlineBodyWithAtReference`.

## See also

- `docs/man.7/hyphence.md` — tutorial / reference manual.
- `docs/rfcs/0001-hyphence.md` — normative format specification (MUST/SHOULD/MAY).
- `testdata/rfc_vectors.txt` + `rfc_conformance_test.go` — RFC conformance test harness.
