# hyphence

I/O for hyphence (hyphen-fence) delimited format (metadata/content separation).

## Key Types

- `MetadataWriterTo`: Writer interface with metadata content check

## Format

Uses `---` as boundary between metadata and content sections.

## See also

- `docs/man.7/hyphence.md` — tutorial / reference manual.
- `docs/rfcs/0001-hyphence.md` — normative format specification (MUST/SHOULD/MAY).
- `testdata/rfc_vectors.txt` + `rfc_conformance_test.go` — RFC conformance test harness.
