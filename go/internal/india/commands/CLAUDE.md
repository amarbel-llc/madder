# commands

Low-level "madder" CLI commands for blob and repository operations.

## Key Commands

- `cat`: Output blob contents by SHA, optionally with external utility processing
- `cat_ids`: Output object IDs
- `complete`: Shell completion support
- `fsck`: Filesystem consistency check
- `info_repo`: Repository information display
- `serve` (`serve.go`): Read-only HTTP server. `GET /blobs/<markl-digest>`
  streams a blob's clear-text bytes (MakeBlobReader already decompresses/
  decrypts), searching the default store then the rest like `cat`; `GET
  /healthz` is a liveness probe. The local reader is forward-only (Seek
  errors), so it streams via the reader's `WriteTo` and sniffs content-type
  from the leading bytes rather than using `http.ServeContent`. Responses are
  marked immutable (content address ⇒ stable bytes). This is the HTTP sibling
  of `madder-mcp serve` (MCP over stdio, `commands_mcp/`); it is meant to sit
  behind a reverse proxy that adds TLS/CORS/auth (e.g. linenisgreat's API
  fronting `/blobs`). Bind address via `-addr` (default `localhost:8079`).

## Features

- Blob store operations with prefix SHA output option
- External utility piping for blob processing
- Uses command framework from kilo/command
