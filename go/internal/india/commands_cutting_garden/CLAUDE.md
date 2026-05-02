# commands_cutting_garden

`cutting-garden` CLI commands. The binary is a sibling of `madder` —
its own Utility namespace ($XDG_*_HOME/cutting-garden/...) but a
client of madder's blob-store machinery via the `command_components`
mixin's `BlobStoreParentUtility = "madder"` setting (see ADR 0007 if
landed, or the cutting-garden extraction plan otherwise).

## Subcommands

- `tree-capture`: walk a tree, write blobs + receipt
- `tree-restore`: materialize a tree from a receipt

Wire format and receipt schema documented in
`docs/man.7/tree-capture-receipt.md` and `docs/rfcs/0003-tree-capture-restore-rules.md`.
