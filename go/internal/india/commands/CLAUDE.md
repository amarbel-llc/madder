# commands

Low-level "madder" CLI commands for blob and repository operations.

## Key Commands

- `cat`: Output blob contents by SHA, optionally with external utility processing
- `cat_ids`: Output object IDs
- `complete`: Shell completion support
- `fsck`: Filesystem consistency check
- `info_repo`: Repository information display
- `serve`: Long-lived admin daemon exposing the configured blob store(s)
  over an HTTP blob API (`GET`/`HEAD`/`PUT /blobs/<digest>`) bound to a
  unix socket (`--socket`). The cross-process coordination surface for
  clients that can't embed `go/pkgs` in-process; see the circus nix-cache
  backend (FDR-0007). Blob lookup reuses `blob_store_env.OpenBlob` /
  `HasBlobInAnyStore` (shared with the MCP server). Not MCP — MCP
  (`madder-mcp`) stays the stdio browse/traverse surface.

## Features

- Blob store operations with prefix SHA output option
- External utility piping for blob processing
- Uses command framework from kilo/command
