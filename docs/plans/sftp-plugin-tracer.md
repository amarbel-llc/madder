# Tracer Bullet: SFTP Blob Store Behind a Subprocess Plugin (go-plugin-inspired)

## Context

`madder` currently has one out-of-process blob storage backend — SFTP — implemented in-process at `internal/foxtrot/blob_stores/store_remote_sftp.go` (788 lines). The user wants to scope the *thinnest possible* refactor that puts the SFTP store behind a subprocess plugin, modeled on HashiCorp's `go-plugin` pattern but without depending on it. The goal is to prove end-to-end that:

1. The host process can launch a child plugin binary with a versioned handshake.
2. A `domain_interfaces.BlobStore` adapter on the host can delegate writes/reads/`HasBlob` to the child via JSON-RPC over a unix-domain socket.
3. Streaming `io.Reader`/`io.Writer` survive the process boundary.
4. A typed error (`env_dir.ErrBlobMissing`) round-trips and is reconstructable on the host.

Everything else — full method coverage, error parity, all config variants, removal of the in-process path, performance, multi-store wiring — is explicitly deferred. The tracer is a one-day exercise that lights up the full path from `madder` CLI → host adapter → plugin subprocess → SFTP server.

## Decisions

- **Transport:** JSON-RPC over a unix-domain socket. No protobuf, no gRPC, no gob, no `hashicorp/go-plugin` dependency. Use stdlib `net/rpc/jsonrpc` (JSON-RPC 1.0 — sufficient for the tracer; upgrading to 2.0 is a future polish).
- **Inspiration, not dependency:** mirror go-plugin's lifecycle (magic-cookie handshake on stderr, transport coords on stdout, host kills child on exit, versioned protocol) but hand-roll it. No new third-party deps; `go.mod` stays clean.
- **Scope cuts:** only `TomlSFTPV0` (explicit host/port/user/password/private-key-path); only `HasBlob` + `MakeBlobReader` + `MakeBlobWriter` go over the wire; no compression/encryption (plugin uses `env_dir.DefaultConfig`, mirroring what `store_remote_sftp.go:264-270` does today); single hash type `sha256` (the actual `DefaultHashTypeId` per `internal/charlie/blob_store_configs/main.go:14`); `BlobReader.ReadAt`/`Seek` return "not supported in tracer".
- **Callsite:** new dedicated `madder sftp-plugin-smoketest` subcommand under `internal/india/commands/`. Zero blast radius on existing config/factory plumbing. The smoketest takes SFTP coords as flags, launches the plugin, writes one blob, reads it back, verifies bytes + digest, exits 0/nonzero.
- **No feature flag, no config variant.** The smoketest command is the *only* path that goes through the plugin. Production callsites are untouched.
- **Plugin discovery order:** `$MADDER_SFTP_PLUGIN_BIN` → sibling of `os.Executable()` → `exec.LookPath`.
- **Test fixture:** an in-process SSH+SFTP server binary (`cmd/madder-test-sftpd`, ~150 lines) using the already-vendored `pkg/sftp` + `golang.org/x/crypto/ssh`. No openssh-server, no container.

## Wire protocol

Frame: JSON-RPC 1.0 (`{"method":"...","params":[...],"id":N}` / `{"result":...,"error":null,"id":N}`). One TCP-style connection per host adapter; calls are sequential within a stream.

Handshake (at child startup, à la `go-plugin`):
- Child reads env `MADDER_PLUGIN_MAGIC_COOKIE`. If mismatched, exits 1 immediately. (Exact-match prevents accidental invocation outside the host.)
- Child binds a unix-domain socket at `$TMPDIR/madder-sftp-plugin-<pid>-<rand>.sock`.
- Child writes one line to stdout: `MADDER-PLUGIN|1|unix|<socket-path>|<sha256-of-listening-host>`. Host parses, dials, the rest is JSON-RPC.
- Child writes log lines to stderr; host pipes them to `ui.Err()`.

RPCs (all on a `BlobStore` Go service bound via `rpc.Server.RegisterName("BlobStore", ...)`):

| RPC | Args | Reply |
|---|---|---|
| `Initialize` | `{Host, Port, User, Password, PrivateKeyPath, RemotePath, KnownHostsFile string}` | `{}` |
| `HasBlob` | `{Id MarklIdWire}` | `{Ok bool}` |
| `OpenReader` | `{Id MarklIdWire}` | `{StreamID uint64, ErrCode string, ErrPath string}` |
| `ReadChunk` | `{StreamID uint64, Max int}` | `{Data []byte, EOF bool, FinalId MarklIdWire}` |
| `CloseReader` | `{StreamID uint64}` | `{}` |
| `OpenWriter` | `{}` | `{StreamID uint64}` |
| `WriteChunk` | `{StreamID uint64, Data []byte}` | `{}` |
| `CommitWriter` | `{StreamID uint64}` | `{Id MarklIdWire}` |

`MarklIdWire = {FormatId string; Bytes []byte}`. Marshalled with helpers in a new `markl_wire.go` using `markl.GetFormatHashOrError(formatId)` + the existing `MarklId` constructors at `/home/user/madder/go/internal/0/domain_interfaces/markl.go:40-67`.

Typed-error round-trip: `OpenReader` returns `ErrCode = "blob-missing"` + `ErrPath` instead of a generic JSON-RPC error string. Host's `MakeBlobReader` reconstructs `env_dir.ErrBlobMissing{BlobId: id, Path: ErrPath}`. All other failures map to a plain wrapped string.

Chunk size: 64 KiB on `ReadChunk` and `WriteChunk`. Tracer-tolerable; reduces JSON+base64 overhead.

## New files

| Path | Purpose |
|---|---|
| `go/internal/golf/blob_store_plugin/handshake.go` | Magic-cookie + protocol-version constants; handshake-line format helpers |
| `go/internal/golf/blob_store_plugin/wire.go` | RPC arg/reply structs; `MarklIdWire` + helpers |
| `go/internal/golf/blob_store_plugin/client.go` | Host-side adapter implementing `domain_interfaces.BlobStore`; chunk-based `BlobReader`/`BlobWriter` impls |
| `go/internal/golf/blob_store_plugin/server.go` | Child-side `BlobStore` RPC service wrapping a real `domain_interfaces.BlobStore` |
| `go/internal/golf/blob_store_plugin/launcher.go` | `LaunchPlugin(ctx, ui, config) (BlobStore, func() error, error)`: discovers binary, spawns child, parses handshake line, dials socket, calls `Initialize`, returns adapter + close func |
| `go/cmd/madder-sftp-plugin/main.go` | Plugin binary entrypoint: validates cookie, listens on unix socket, prints handshake, serves JSON-RPC; on `Initialize` builds a `remoteSftp` via the existing factory and delegates |
| `go/cmd/madder-test-sftpd/main.go` | Test-only SSH+SFTP fixture: ed25519 host key, password auth, in-memory `sftp.Server` rooted at a flag-provided dir, writes coords + known_hosts line on startup |
| `go/internal/india/commands/sftp_plugin_smoketest.go` | New `sftp-plugin-smoketest` subcommand: launches plugin, writes a known blob, reads it back, asserts byte equality + digest match |
| `zz-tests_bats/sftp_plugin.bats` | E2E bats test wiring fixture + smoketest |
| `zz-tests_bats/lib/sftp_fixture.bash` | Bash helper to start/stop `madder-test-sftpd` and emit coords |

## Existing files to modify

| Path | Change |
|---|---|
| `go/internal/foxtrot/blob_stores/store_remote_sftp.go` | Add an exported wrapper `MakeSftpStoreForPlugin(ctx, ui, config, sshClientInitializer) (domain_interfaces.BlobStore, error)` that delegates to the existing package-private `makeSftpStore` at `:57`. The plugin process imports `foxtrot/blob_stores` and uses this wrapper — we are putting a process boundary *in front of* the existing implementation, not rewriting it |
| `go/internal/foxtrot/blob_stores/util_ssh.go` | Re-export `MakeSSHClientForExplicitConfig` (already exported at `:84`) — no change, just confirming the plugin can call it |
| `justfile` | Add `build-sftp-plugin`: `cd go && go build -o {{justfile_directory()}}/.tmp/bin/ ./cmd/madder-sftp-plugin ./cmd/madder-test-sftpd` |

`go.mod` is **unchanged**. The plan deliberately uses only stdlib (`net/rpc/jsonrpc`, `os/exec`, `net`) plus what's already vendored (`pkg/sftp`, `golang.org/x/crypto/ssh`).

## Implementation order

Each step compiles and is independently verifiable.

1. **Wire types + handshake constants.** Write `wire.go` + `handshake.go` + a tiny round-trip unit test for `MarklIdWire`. *Verify:* `go test ./internal/golf/blob_store_plugin/...`.
2. **Stub plugin binary.** Write `cmd/madder-sftp-plugin/main.go` and `server.go` with a stub backend (HasBlob always true, OpenReader streams `"hello\n"`, OpenWriter accepts then returns a fixed digest). Stand up handshake + JSON-RPC server. *Verify:* run binary by hand, send a JSON-RPC line over the socket, see a reply.
3. **Host adapter.** Write `client.go` implementing `domain_interfaces.BlobStore` against the wire RPCs. `pluginBlobReader.Read` calls `ReadChunk` with a small local buffer; `WriteTo` loops until EOF; `Close` calls `CloseReader`. `pluginBlobWriter.Write` calls `WriteChunk`; `Close` calls `CommitWriter` and stashes the returned `MarklId`; `GetMarklId` returns the stashed value (mirroring the in-process behavior at `store_remote_sftp.go:605-611`).
4. **Launcher.** Write `launcher.go`: discovers the binary (3-tier lookup), `os/exec.Cmd` with stderr piped to `ui.Err()`, reads the handshake line from stdout, dials the socket, calls `Initialize`. Returns the adapter + a `Close` func that closes the RPC client and `Process.Kill`s the child if it doesn't exit on socket close. Wire `ctx.After(closeFn)`.
5. **End-to-end with stub.** Write `sftp_plugin_smoketest.go` (host-side CLI command). Run `madder sftp-plugin-smoketest` against the stub server; asserts write-then-read round-trip on canned data. *This is the tracer landing — at this point we have full IPC working without any SFTP.*
6. **Replace stub with real SFTP on the plugin side.** In `server.go`, on `Initialize`, build a `TomlSFTPV0` from the wire config, build the SSH-client initializer via `MakeSSHClientForExplicitConfig`, call `MakeSftpStoreForPlugin`, store the result. `HasBlob`/`OpenReader`/`OpenWriter` delegate to it. Map `env_dir.ErrBlobMissing` → `OpenReader.Reply{ErrCode:"blob-missing", ErrPath:...}`.
7. **Test SFTP fixture binary.** Write `cmd/madder-test-sftpd/main.go`. Generates ed25519 host key, listens on `127.0.0.1:0`, accepts password auth, spawns `sftp.Server` rooted at a temp dir, writes `host:port` + known_hosts line on startup, exits on SIGTERM.
8. **Bats E2E.** `zz-tests_bats/sftp_plugin.bats` boots the fixture, seeds `<remote-path>/blob.toml` (the `readRemoteConfig` path at `store_remote_sftp.go:113-184` requires it), runs the smoketest, asserts success + a `plugin-pid=<n>` log line proving subprocess separation.
9. **Negative test.** Add a smoketest mode (`-expect-missing`) that calls `MakeBlobReader(unknownDigest)` and asserts the returned error round-trips as `env_dir.ErrBlobMissing` with the right `Path`. *This is the single highest-signal verification — typed errors crossing a process boundary is the load-bearing nontrivial bit.*

## Files to read before starting

- `/home/user/madder/go/internal/0/domain_interfaces/blob_store.go` — interface contract we must implement on the host adapter (lines 26-67)
- `/home/user/madder/go/internal/0/domain_interfaces/markl.go:40-67` — `MarklId` interface for wire conversion
- `/home/user/madder/go/internal/foxtrot/blob_stores/store_remote_sftp.go:57-83, 264-270, 459-688` — factory we'll wrap; default `env_dir.Config`; mover/writer for the digest-on-Close pattern
- `/home/user/madder/go/internal/foxtrot/blob_stores/util_ssh.go:84-145` — `MakeSSHClientForExplicitConfig` we'll call from the plugin
- `/home/user/madder/go/internal/echo/env_dir/errors.go` — `ErrBlobMissing` shape that must round-trip
- `/home/user/madder/go/internal/charlie/blob_store_configs/toml_sftp_v0.go` — exact field set the wire `Config` mirrors
- `/home/user/madder/go/internal/india/commands/has.go` (or any small command file) — pattern for command registration; the new smoketest follows this
- `/home/user/madder/go/internal/charlie/blob_store_configs/main.go:14` — confirm `DefaultHashTypeId = HashTypeSha256` (fix vs. plan-agent's earlier blake2b guess)

## Verification

End-to-end definition of done:

1. `cd go && go build ./...` — clean.
2. `just build-sftp-plugin` — both new binaries build.
3. `cd go && go test ./internal/golf/blob_store_plugin/...` — wire-types unit test green.
4. **Tracer smoketest (positive):** `MADDER_SFTP_PLUGIN_BIN=$PWD/.tmp/bin/madder-sftp-plugin madder sftp-plugin-smoketest -host=127.0.0.1 -port=$PORT -user=t -password=t -remote-path=/store -known-hosts-file=$KNOWN` writes 14 bytes, reads them back, prints `ok` and `plugin-pid=<N>`.
5. **Tracer smoketest (negative):** same as (4) with `-expect-missing` and a synthesized random digest. Host asserts `errors.As(err, &env_dir.ErrBlobMissing{})` returns true and `.Path` matches the bucketed remote path.
6. `pgrep -f madder-sftp-plugin` after smoketest exits returns nothing — child process reaped.
7. `bats zz-tests_bats/sftp_plugin.bats` green.

If (4), (5), and (6) all pass, the tracer has proven the pattern: subprocess plugin, JSON-RPC IPC, streaming reads/writes, typed-error round-trip, lifecycle. Everything left is incremental.

## What this tracer deliberately does NOT prove (deferred)

- `TomlSFTPViaSSHConfigV0` (ssh_config + agent forwarding into subprocess).
- Concurrent reader/writer streams (stream IDs are in the protocol but only one is exercised).
- `AllBlobs`, `GetBlobIOWrapper`, `ReadAt`, `Seek`, full hash-type negotiation.
- Compression/encryption.
- Cancellation propagation from `interfaces.ActiveContext` (host kills the child via `ctx.After`, but mid-stream cancel during a `ReadChunk` isn't tested).
- Performance: extra hop per chunk + JSON+base64 overhead is not benchmarked.
- Replacing the in-process SFTP path; both coexist.

These are all tractable extensions once the tracer is green.
