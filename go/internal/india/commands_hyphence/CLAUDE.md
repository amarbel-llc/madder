# commands_hyphence

`hyphence` CLI commands. The binary is a sibling of `madder`,
`madder-cache`, and `cutting-garden` — its own utility identity for
CLI purposes, with no blob-store integration. Format-only tooling
for on-disk hyphence documents per RFC 0001.

## Subcommands

- `validate`: strict RFC 0001 conformance check
- `meta`: print metadata section verbatim
- `body`: print body section verbatim
- `format`: re-emit canonicalized per RFC §Canonical Line Order

Wire format documented in `docs/rfcs/0001-hyphence.md` and
`docs/man.7/hyphence.md`. Library used: `go/internal/charlie/hyphence/`
(`Document`, `MetadataStreamer`, `MetadataBuilder`,
`MetadataValidator`, `FormatBodyEmitter`, `Canonicalize`).
