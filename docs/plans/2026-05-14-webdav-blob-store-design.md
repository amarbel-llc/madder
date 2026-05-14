# Plan — WebDAV-backed blob store for madder

## Context

Madder currently supports four blob store backends — local hash-bucketed,
inventory archive, pointer, and SFTP — but no HTTP-based remote. WebDAV
is the natural next addition: it covers the gap of "I want a remote store
but my server speaks HTTPS, not SSH" (Nextcloud, Apache `mod_dav`, nginx
WebDAV, rclone serve webdav, hosted providers like Box / Fastmail). It
preserves madder's content-addressing invariants because every per-blob
operation (HEAD / GET / PUT / MOVE / MKCOL) maps cleanly onto the
hash-bucket path scheme the local and SFTP stores already use.

**Prior art:** dodder (madder's parent project, extracted April 2026) has
a `go/internal/0/webdav` package that was *not* lifted out with the rest
of the blob-store machinery. No WebDAV code, dep, or doc currently lives
in madder. ADR 0005 (remote-driven SFTP blob stores) is the architectural
template — WebDAV will be a Mode-B remote-driven store, identically to
SFTP: local config holds transport only, remote `blob_store-config` holds
hash / buckets / compression / encryption.

**Scope decisions:**
- Auth in v0: basic, bearer, anonymous, **and** TLS client cert.
- Ship `madder-test-webdav-server` binary + `webdav.bats` up front, matching
  the SFTP harness.
- Single `madder init-webdav` (URL flag), no SSH-config-style second flavor.

## Approach

Mirror the SFTP precedent at every layer. The store is a new
`remoteWebdav` implementation behind the existing `BlobStore` interface,
the local config carries transport only (ADR 0005), and the remote
`blob_store-config` is the source of truth for blob-store properties.
Use stdlib `net/http` for the client (no external WebDAV client lib —
roll a ~50-line PROPFIND XML parser with `encoding/xml`). Use
`golang.org/x/net/webdav` for the test-server binary only.

**Ship in two PRs** to keep review surface manageable:

- **PR 1 — Transport core.** Config type, ids registration, coder
  registration, factory wiring, `remoteWebdav` with basic-auth and
  anonymous, plaintext test server, `init-webdav` bootstrap, bats
  coverage of write/cat/has/list/fsck.
- **PR 2 — Auth and TLS expansion.** Bearer-token and TLS client cert,
  `tls-*` config knobs, TLS in the test server, bats per auth mode.

Encryption support comes for free — it lives in the remote `blob_store-config`,
not the local transport.

## Files to add / modify

### New files (PR 1)

1. **`go/internal/charlie/blob_store_configs/toml_webdav_v0.go`** +
   tommy-generated companion. Transport-only struct:
   ```go
   type TomlWebDAVV0 struct {
       URL         string `toml:"url"`
       User        string `toml:"user,omitempty"`
       Password    string `toml:"password,omitempty"`
       // (Bearer + TLS fields added in PR 2)
   }
   ```
   `GetBlobStoreType()` returns `"webdav"`. Implements a new
   `ConfigWebDAV` interface (parallel to `ConfigSFTPRemotePath` —
   defined alongside in `charlie/blob_store_configs/main.go`).
2. **`go/internal/foxtrot/blob_stores/store_remote_webdav.go`** —
   `remoteWebdav` struct mirroring `remoteSftp`
   (`go/internal/foxtrot/blob_stores/store_remote_sftp.go:32-85`). Key
   methods:
   - `initializeOnce()` — sync.Once with sticky `initErr` re-panic
     (verbatim from SFTP at lines 161-170).
   - `initialize()` — builds `http.Client` via injected
     `httpClientInitializer func() (*http.Client, error)`, calls
     `readRemoteConfig()`, MKCOLs the path tree.
   - `readRemoteConfig()` — `GET <url>/blob_store-config`, decode via
     `blob_store_configs.Coder.DecodeFrom`. Re-uses every assertion in
     `store_remote_sftp.go:213-250` (hash type, buckets, IO wrapper).
   - `HasBlob` — `HEAD <bucket-path>`, populate blob cache on 200.
   - `MakeBlobReader` — `GET`, wrap with `webdavReader`
     (decompression/decryption layer identical to `sftpReader` at
     lines 909-981).
   - `MakeBlobWriter` — PUT to `tmp_<rand>`, then MOVE to final on
     `Close()`. **Atomicity fallback**: on MOVE failure (403/409/507),
     HEAD the target; if present, treat as duplicate-write success and
     DELETE the temp. Mirror `store_remote_sftp.go:765-789`.
   - `AllBlobs` — recursive PROPFIND walker (`Depth: infinity` where
     supported, fallback `Depth: 1` per directory). Reuse
     `shouldSkipBlobWalkEntry` from `store_remote_sftp.go:460-463`.
   - mmap returns `ok=false`.
   - Write observer wiring identical to `sftpMover.emitWriteEvent`
     (lines 626-647).
3. **`go/cmd/madder-test-webdav-server/main.go`** — RFC 0001
   plugin-cookie handshake (model on
   `go/cmd/madder-test-sftp-server/main.go`). Serves
   `webdav.Handler{Prefix: "/", FileSystem: webdav.Dir(tmpdir), LockSystem: webdav.NewMemLS()}`
   on an ephemeral port; prints `http://addr:port/` to stdout; shuts
   down on stdin EOF. PR 1 ships plaintext; PR 2 layers
   `httptest.NewTLSServer` self-signed cert into the handshake.
4. **`go/internal/foxtrot/blob_stores/store_remote_webdav_test.go`** —
   mirror `store_remote_sftp_test.go`: write-observer event capture
   (`recordingObserver`), init-failure panic semantics.
5. **`webdav.bats`** (same path as the existing SFTP bats; check
   `go/internal/india/commands/main_test.go` or root-level bats) —
   covers golden paths plus the risk-area scenarios listed below.

### Modified files (PR 1)

6. **`go/internal/0/ids/types_builtin.go:6-17`** — add
   `TypeTomlBlobStoreConfigWebdavV0 = "!toml-blob_store_config_webdav-v0"`,
   **and** register it in the `init()` loop at line 28.
7. **`go/internal/delta/blob_store_configs/main.go`** — re-export
   `TomlWebDAVV0`, `ConfigWebDAV`, `DecodeTomlWebDAVV0`; add to
   `TypeStructForConfig` switch at line 136; add compile-time
   assertions `_ ConfigWebDAV = &TomlWebDAVV0{}` and `_ ConfigMutable = &TomlWebDAVV0{}`
   in the block at line 83.
8. **`go/internal/delta/blob_store_configs/coding.go`** — register a
   `CoderTommy` map entry under `TypeTomlBlobStoreConfigWebdavV0`,
   parallel to the SFTP entry at lines 89-110.
9. **`go/internal/foxtrot/blob_stores/main.go:275`** — add
   `case blob_store_configs.ConfigWebDAV:` in the `MakeBlobStore`
   switch. Constructor signature is `makeWebdavStore(ctx, printer, id, config, httpClientInitializer, observer)` —
   **don't reuse** `ConfigSFTPUri` / `ConfigSFTPConfigExplicit`; their
   factory carries `sshClientInitializer` which is meaningless here.
10. **`go/internal/charlie/blob_store_configs/key_values.go`** — add a
    branch for `ConfigWebDAV` so `madder info-repo <store> url` /
    `user` work. **Must drop `password`** (and `bearer-token` /
    `tls-client-key-path` in PR 2) — secrets do not surface via this
    map. Pin with a unit test.
11. **`go/internal/india/commands/init.go`** — register `init-webdav`
    using `TomlWebDAVV0`. Add a `ensureRemoteConfigExists` analogue
    that PUTs a default `TomlV3` config to `<url>/blob_store-config`
    when missing, mirroring lines 437-507. Encryption flag handling
    follows the same pattern (lands on `Init`, written into remote
    config). `-discover` is **not** supported in v0 — only the
    fresh-bootstrap path. Document the omission in the man page.
12. **`docs/man.7/blob-store.md:90`** — add `## WebDAV` section parallel
    to `## SFTP`. Document supported auth modes, the URL form, the
    "no `-discover` in v0" caveat, and the same ADR 0005 reference
    block that SFTP gets.

### PR 2 deltas

- Add `BearerToken`, `TLSClientCertPath`, `TLSClientKeyPath`, `TLSCAPath`,
  `TLSServerName`, `TLSInsecureSkipVerify` fields to `TomlWebDAVV0`.
- Validation: exactly one of `{password, bearer-token, tls-client-cert-path,
  anonymous}` may be set — error in `makeWebdavStore` before HTTP client
  build, surfaced via `ContextCancelWithBadRequestError` from `Init`.
  Don't merge auth schemes; RFC 7235 makes that ambiguous.
- TLS in test server: `httptest.NewTLSServer`, emit `cert=<path>` in
  handshake line so bats helper passes `-tls-ca-path`.
- One bats scenario per auth mode.

## Dependencies

- **No new runtime client dep.** Stdlib `net/http` + ~50 lines of
  `encoding/xml` for PROPFIND parsing. Reject `github.com/studio-b12/gowebdav`
  — its `Walk` doesn't expose `Depth: infinity` cleanly, and the dep
  weight isn't justified for the handful of methods we actually need.
- **`golang.org/x/net/webdav`** added under `go.mod` for the test
  server only.

## Protocol-specific decisions (resolved)

- **MKCOL races.** WebDAV returns **405 Method Not Allowed** when the
  resource already exists. Treat 405 as success **only after** a
  follow-up PROPFIND `Depth: 0` confirms `<resourcetype><collection/></resourcetype>`.
  If the existing resource is a file, hard-fail — that's a real conflict.
- **MOVE atomicity.** Always issue with `Overwrite: F`. On 412 / 409 /
  507, HEAD the target; if present, duplicate-write — DELETE the temp
  and treat as success. Never set `Overwrite: T` (would clobber a
  concurrent writer's blob and violate CAS invariants).
- **URL normalization.** Strip and re-append trailing slash once at
  config-load time; bats verifies both forms work.
- **Walker filtering.** Skip `blob_store-config` and `tmp_*` (reuse
  `shouldSkipBlobWalkEntry`).

## Reuse references

- Lazy init + sticky-error re-panic: `store_remote_sftp.go:161-170`.
- Remote config decode and hash/bucket/IO assertions:
  `store_remote_sftp.go:172-260`.
- Hash-bucket path math: `blob_io.MakeHashBucketPathFromMerkleId` (used
  at `store_remote_sftp.go:355-362`).
- Write observer plumbing: `sftpMover.emitWriteEvent`
  (`store_remote_sftp.go:626-647`).
- Atomic-move-with-stat-fallback pattern: `store_remote_sftp.go:765-789`.
- Multi-hash bucket walker shape: `allBlobsMultiHash`
  (`store_remote_sftp.go:414-450`).
- Decompression / decryption reader pipeline: `sftpReader`
  (`store_remote_sftp.go:909-981`).
- Init bootstrap of remote `blob_store-config`:
  `Init.ensureRemoteConfigExists` (`init.go:437-507`).
- `DiscoveredConfig` / `WriteRemoteConfig` helpers already exist in
  `foxtrot/blob_stores` and are transport-agnostic — pass the WebDAV
  client into a thin shim that satisfies the same write contract.

## Verification

End-to-end checks before each PR ships:

1. **Build clean** — `cd go && go build ./...` from repo root. Both
   `madder` and `madder-test-webdav-server` produce.
2. **Unit tests** — `go test ./internal/foxtrot/blob_stores/...` plus
   the new `key_values` secret-redaction test. Confirms observer events,
   panic-on-init-failure semantics, walker-skip filtering.
3. **bats end-to-end** — run `webdav.bats`. Required scenarios:
   - Init + fresh-bootstrap remote config.
   - Round-trip: write → has → cat for one blob.
   - List: `madder cat-ids` over a store with multiple blobs.
   - Fsck on a populated store.
   - **Concurrent same-blob writes** — two parallel `madder write` of
     the same bytes (mirrors `concurrent_write_test.go`).
   - **Concurrent different blobs into uncreated bucket** —
     exercises MKCOL race resolution.
   - **Duplicate-write fallback** — write same blob twice, second
     observes MOVE-to-existing path.
   - **Empty `AllBlobs`** — fresh store yields no phantom entries.
   - **`blob_store-config` / `tmp_*` filtered** from list output.
   - **URL with and without trailing slash** — both work.
   - **Large blob (>10 MB)** — chunked PUT + PROPFIND
     `getcontentlength` parsing.
   - **Auth failure** — wrong basic-auth password produces a typed
     panic via `initErr`, surfaces as a clean error, not a context
     cancellation. (Matches `TestSftpInitializeOnce_PanicsOnInitFailure`.)
4. **Manual smoke** — point a real `madder init-webdav` at a local
   `rclone serve webdav` or `apache2 + mod_dav` instance; `madder write`
   + `madder cat` round-trip.
5. **PR 2 only** — bats per auth mode (basic, bearer, TLS cert,
   anonymous); TLS handshake failure when `-tls-insecure-skip-verify`
   is off and the server cert is self-signed.

## Out of scope (follow-ups)

- `init-webdav -discover` for adopting an existing remote layout.
- Server-side ETag-based collision verification (the WebDAV analogue of
  `MADDER_VERIFY_ON_COLLISION`).
- `init-webdav-from-netrc` flavor for keeping credentials out of the
  TOML config file.
- Local cache of the remote `blob_store-config` (already filed as a
  separate follow-up from ADR 0005).
