# commands_cutting_garden

`cutting-garden` CLI commands. The binary is a sibling of `madder` —
its own Utility identity for CLI purposes, but a client of madder's
blob-store machinery via the `command_components` mixin's
`BlobStoreXDGScope = "madder"` setting (see the cutting-garden
extraction plan).

## Subcommands

- `capture`: walk a tree, write blobs + receipt
- `restore`: materialize a tree from a receipt

Wire format and receipt schema documented in
`docs/man.7/capture-receipt.md` and `docs/rfcs/0003-capture-restore-rules.md`.
