# madder-store: a Rust reader/writer for sealed-key local stores

Status: design, awaiting confirmation from piggy/live-sequoia (2026-10-25).
Consumer: **pigsty**, a Flutter + flutter_rust_bridge password vault
(Linux and Android) that keeps hyphence entry documents and attachments
as blobs in a madder store whose key is sealed to a pigpen recipient set
(FDR 0011). pigsty lives in its own repo and depends on this crate and on
piggy's `crates/piggy-pigpen`. Layering rule (Sasha, 2026-10-25):
`dewey -> piggy -> madder`, so madder may depend on piggy's crates; piggy
never depends on madder.

Every claim below is labelled: **V** observed in this session, **R** read
from code or docs, **I** inferred.

## Scope

In scope, byte-compatible with the Go implementation:

1. Open a store directory: parse `blob_store-config`
   (`toml-blob_store_config-v5`) and verify its `@` body digest; parse
   `blob_store-key` (v2, and v1 for reading) including the recorded
   recipient source (`path` / `papi` / `embedded`).
2. Unseal the store key through an injected `piggy_pigpen::EcdhOracle`
   and/or software `X25519Identity` values. The crate never talks to a
   card or agent.
3. Read a blob by markl id: bucket path, age decrypt, decompress, verify
   the digest.
4. Write a blob: compress, age-encrypt to the store public key, publish
   atomically and durably per blob-store(7) CONCURRENCY AND DURABILITY,
   return the markl id. `has` and `ids` (list).
5. Report the sealed recipient set and the recorded source without an
   agent (the read half of `key-status`).

Out of scope: `key-reseal`, `config-gen`, every remote store type,
inventory archives, pointer and multi stores, the drift check against a
re-resolved pigpen (needs `pigpen-resolver-*` binaries that do not exist
on Android), verify-on-collision (needs the key on the write path; see
Decisions). `init` of a new store is a **decision**, below.

## On-disk facts the crate implements

All from the Go code in this repo (R) unless marked.

- Files at the store base: `blob_store-config` (immutable, digest-pinned)
  and `blob_store-key` (mutable sidecar). Blob enumeration skips exactly
  these two names plus the legacy `dodder-blob_store-config`
  (`directory_layout.IsBlobStoreConfigFileName`).
- Config file: hyphence header `---\n! toml-blob_store_config-v5\n@
  <digest>\n---\n\n` then a TOML body. The `@` digest is
  `madder-blob_store-config-digest-v1@blake2b256-<blech32>` over the body
  bytes, where the body starts after the LAST `---\n\n` in the file
  (`DecodeAndVerify`). A config with no `@` line is trusted (legacy).
- V5 body fields (`TomlV5`): `hash_buckets` (int array, default `[2]`),
  `hash_type-id` (`blake2b256` default, or `sha256`), `encryption` (exactly
  one `piggy-recipient-v1@age_x25519_pub-…`), `compression-type` (`zstd`
  default; also `none`, `gzip`, `zlib`), `verify-on-collision`,
  `single_hash` (absent = false), `instance-id` (a `uuidv7-…` markl id,
  carried opaquely: piggy-markl does not know the `uuidv7` format),
  `[key-custody] holder = "process"`. Any other holder is refused.
- Sidecar: exactly the header `---\n! toml-blob_store_key-v2\n---\n\n`
  (or `-v1`) then TOML: `[recipients] source_kind, source, pigpen`
  (multiline string) and `[sealed] document` = base64 of the sealed
  pigpen-v1 document, wrapped at 64 columns; whitespace is stripped before
  decoding. V1 holds the document verbatim and only a path source.
- The sealed payload is the 32-byte X25519 scalar. After opening, the
  derived public key MUST equal the config's `encryption` id, else the
  sidecar belongs to another store (`store_key.ioWrapper.unseal`).
- Blob path: `<base>/<hash-format-id>/<bucket dirs>/<rest>` when
  `single_hash` is false (the default), else `<base>/<bucket dirs>/<rest>`;
  the digest is lower-case hex, cut into `hash_buckets` characters per
  directory (`files.MakeHashBucketPath`).
- Blob id text: bare `blake2b256-<blech32>` / `sha256-<blech32>`, no
  purpose (hash ids carry none). V: `madder write -format tap` prints
  them in column 4 (`write_blob_id` in zz-tests_bats/lib/common.bash).
- Blob bytes: plaintext -> compression -> age -> file (`blob_io.NewWriter`:
  the compressor wraps the encrypter). Age is the standard binary v1
  format from `filippo.io/age`, one X25519 recipient stanza, never
  armored (`startsWithAgeHeader` keys on `age-encryption.org/v1\n`).
- Publish (`blob_io/mover.go`): write to a temp file on the same
  filesystem; `Close` the encrypt+compress chain; `fsync` the file;
  `chmod 0444`; `mkdir -p` the bucket dir; `link(2)` temp -> final;
  `EEXIST` is benign (same digest, same bytes: ADR 0002); unlink the
  temp; `fsync` the final file's directory. `EXDEV` is an error.

## Crate shape

Name: `madder-store`, at `crates/madder-store/`, with a root
`Cargo.toml` workspace (`members = ["crates/madder-store"]`) so the
eng-rust template's clippy lane and rustfmt apply unchanged. Library
plus one small binary target, `madder-store-cli` (write / cat / has /
ids / key-status / mint-recipient), used only by the bats interop lane
and ad-hoc checks; it is not shipped.

Modules, each with its own tests:

- `hyphence`: the exact-header parser both files need (boundary lines,
  `! ` and `@ ` metadata lines, body after `---\n\n`). Not a general
  hyphence implementation; hyphence framing is hyphence's lane.
- `config`: V5 parse via the `toml` crate, digest verification with
  `blake2`, the holder and single-public-key checks.
- `sidecar`: V2/V1 parse, base64 unwrap, `piggy_pigpen::Document::parse`,
  `sealed_recipients()`, `source()`.
- `blob_id`: `BlobId` over `piggy_markl::Id` restricted to the two hash
  formats; hex <-> bucket path.
- `age_x25519`: age v1 encrypt-to-one-recipient and decrypt-with-one-
  identity, streaming (64 KiB STREAM chunks). See Decision 1.
- `codec`: `none` and `zstd`. See Decision 2. `gzip`/`zlib` return
  `Unsupported` (flate2 is pure Rust and could follow; pigsty does not
  need it).
- `publish`: the temp + link + fsync sequence above, behind a small
  `Fs` trait (`read`, `exists`, `read_dir`, `create_temp`, `write`,
  `sync`, `set_readonly`, `hard_link`, `remove`, `sync_dir`) with a
  `std::fs` implementation. The trait is the seam for wasm (no `std::fs`)
  and keeps tokio out.
- `store`: the public type.

Public API sketch:

```rust
pub struct Store { /* base, config, sidecar, OnceCell<StoreKey> */ }
pub struct Unsealer<'a> {
    pub oracle: Option<&'a dyn piggy_pigpen::EcdhOracle>,
    pub identities: &'a [piggy_pigpen::X25519Identity],
}
impl Store {
    pub fn open(base: &Path, opts: OpenOptions) -> Result<Store>; // no unseal
    pub fn public_key(&self) -> &piggy_markl::Id;
    pub fn sealed_recipients(&self) -> Vec<piggy_markl::Id>;
    pub fn recipient_source(&self) -> &RecipientSource;          // kind, locator, pigpen text
    pub fn unseal(&self, with: &Unsealer) -> Result<()>;         // once; one oracle call
    pub fn write(&self, plaintext: impl std::io::Read) -> Result<BlobId>; // no key needed
    pub fn has(&self, id: &BlobId) -> bool;
    pub fn read(&self, id: &BlobId) -> Result<impl std::io::Read>;      // digest-verified at EOF
    pub fn ids(&self) -> impl Iterator<Item = Result<BlobId>>;
}
pub struct OpenOptions { pub temp_dir: Option<PathBuf>, /* fs: Box<dyn Fs> */ }
```

`temp_dir` defaults to a sibling of the store base, `<parent>/.<name>.tmp`,
which is on the same filesystem by construction and outside the walk
that `ids`/Go `AllBlobs` perform. Go uses `$XDG_CACHE_HOME/madder/tmp-<pid>/`;
pigsty on Android has no XDG variables and passes its cache dir
explicitly. (I: a directory under the base would surface as an
"unknown hash format" error in Go's multi-hash walk, so it must not be
there; R: `localAllBlobsMultihash`.)

Errors: one `Error` enum with variants a vault UI can act on
(`NotFound(BlobId)`, `Locked` for read-before-unseal, `NoRecipient`,
`Oracle(String)` passed through from piggy-pigpen, `DigestMismatch`,
`ConfigTampered`, `WrongKey`, `Unsupported(&'static str)`, `Io`).

Dependencies, all pure Rust (R: piggy-pigpen's Cargo.toml for the
RustCrypto set, which this crate reuses at the same versions):
`piggy-pigpen`, `piggy-markl` (git, pinned by rev; see Decision 3),
`x25519-dalek`, `chacha20poly1305`, `hkdf`, `hmac`, `sha2`, `blake2`,
`base64`, `toml` + `serde`, `ruzstd`, `rand_core`/`getrandom` (with the
`js` feature on wasm32, as piggy-pigpen does), `zeroize`, `thiserror`.
No OpenSSL, no C, no tokio. `#![forbid(unsafe_code)]`.

## Decisions that need a yes (recommendation first)

1. **Age: hand-roll the X25519 subset, do not take the `age` crate.**
   The crate needs encrypt-to-one-X25519-recipient and
   decrypt-with-one-identity, nothing else. age v1 is a short spec
   (header stanza, HKDF-SHA256 wrap key, ChaCha20-Poly1305 body key,
   HMAC header MAC, 16-byte nonce + STREAM 64 KiB chunks), and
   piggy-pigpen already implements the same STREAM construction in-house
   (R: `crypto.rs`, "age's STREAM payload"). Hand-rolling keeps the
   dependency set identical to piggy-pigpen's, which is the set already
   proven on wasm32, and avoids the `age` crate's i18n-embed/fluent,
   scrypt, nom and plugin machinery. Interop is pinned three ways: Go-
   written fixtures read byte-exact; Rust-written blobs read by `madder
   cat`/`fsck`; and round-trips against the `age` CLI from nixpkgs in
   the cargo test lane (`age -d -i` of a Rust file, Rust decrypt of
   `age -r`). The module is isolated so swapping in the `age` crate
   later is a local change. Alternative: the `age` crate (0.11), pure
   Rust and wasm-capable, at the cost of that closure.

2. **zstd: `ruzstd` for decode; for encode, ruzstd's encoder if the
   pinned version ships one, else frames of raw (stored) blocks.** Go
   reads with libzstd, which accepts any conformant frame, and the blob
   id is the plaintext digest, so compression level never changes an
   id. Pigsty's blobs are small; losing compression on Rust-written blobs
   costs disk, not correctness. If `init` lands (Decision 4) pigsty can
   also choose `compression-type = "none"` for its own stores.

3. **piggy-pigpen and piggy-markl as Cargo git dependencies pinned by
   rev**, with `cargoLock.outputHashes` in nix exactly as piggy's flake
   does for `tap-dancer` (R: piggy flake.nix `sharedCargoLock`). The
   piggy flake does not package `piggy-pigpen` at all (V: no mention in
   piggy's flake.nix; the crate is excluded from the workspace and has
   its own Cargo.lock), so there is nothing to consume from the `piggy`
   flake input, and a cross-repo `path` dependency cannot exist. This
   means two piggy pins in madder: flake.lock (Go) and Cargo.lock
   (Rust). `just update-piggy` would learn to move both. Open question
   for piggy's lane: whether cargo's git source finds a package that
   the repo's workspace `exclude`s (I think it does, since cargo walks
   every `Cargo.toml`; I will verify empirically with the first
   `cargo build` and report). If it does not, the fallback is
   `nix`-time vendoring of `${piggy}/crates/{piggy-pigpen,piggy-markl}`
   plus a `[patch]`, which I would rather not do.

4. **`init` of a new sealed-key store: I recommend IN scope.** The brief
   left it out unless trivial. pigsty on Android cannot shell out to Go
   madder, so unless the vault's store is always created elsewhere and
   synced in, pigsty needs `init`. It is small: mint an X25519 key,
   `piggy_pigpen::Document::seal` to the recipients, write the v2
   sidecar, write a V5 config with a freshly minted `uuidv7` instance id
   and the `@` digest over the body bytes we wrote. Byte-identity with
   tommy's TOML rendering is NOT required: the digest pins our own
   bytes, and Go decodes any valid TOML. Refuses an existing store, writes
   the sidecar before the config (FDR 0011). Conformance: Go `madder
   list`/`info-repo`/`key-status` on a Rust-initialized store, and Go
   `madder write` + Rust read of it.

5. **Not V4/V3 stores.** A raw `age_x25519_sec` in the config is one
   extra config version and a trivial unseal path; pigsty does not need
   it. Cheap follow-up if dodder or cutting-garden ever want a Rust
   reader for plain stores.

## Where the eng-rust template and madder's flake disagree

I will take from `templates/eng-rust-lib` (R):

- the crate layout (`Cargo.toml`, `src/lib.rs`, `AGENTS.md` +
  `CLAUDE.md` symlink inside `crates/madder-store/`),
- `programs.rustfmt.enable` in `conformist.nix`,
- the clippy impure linter block, as a new `conformist-impure.nix`
  imported into madder's existing `conformistImpureEval`,
- cargo/rustc/clippy/rustfmt/gcc in the devShell,
- `test-rust` and a clippy lane in the justfile aggregates.

I will NOT apply:

- the template `flake.nix` or `justfile` wholesale: madder's flake is
  already the conformist shape with its own inputs, and `conform` local
  mode has nothing to add; a repo-root Rust flake would fight the Go one;
- `linters.gomod2nix.enable = mkForce false`: madder has gomod2nix and
  needs that linter on;
- scdoc `doc/lib.7.scd` + `checks.manpage-names`: madder's manpages are
  pandoc-built from `docs/man.7/*.md` (`madder-gen_man`) and already
  have a NAME-line check (`debug-man-name-lines`). The crate's page goes
  in as `docs/man.7/madder-store.md` through the existing pipeline;
- the version key: madder's `conformist.nix` pins
  `linters.eng-versioning.key = "MADDER_VERSION"` and the crate shares
  `version.env`. Whether eng-versioning also compares the Cargo.toml
  `version` against it I have not read; I will find out when wiring the
  lane and say so.

Build: `pkgs.rustPlatform.buildRustPackage` from igloo's nixpkgs
(`.#madder-store`, `cargo test` in `checkPhase`; `.#madder-store-cli` for
the bats lane). Portability gate: nixpkgs carries `rustc-wasm32` (V:
`chix doc-tree` lists it), so a `cargo check --target
wasm32-unknown-unknown` lane is feasible without a toolchain input;
`aarch64-linux-android` needs the NDK linker and is left to pigsty's
toolchain, with a `cargo check` there once pigsty exists. igloo/slim-pecan
is exploring `rustdyn` (per-crate Rust builds, igloo FDR 0009); not a
dependency of this work, but the derivation should be easy to move onto
it later.

## Conformance fixtures and the gate

Go -> Rust (committed fixtures, exercised by `cargo test`):

- New fixture binary `go/cmd/madder-test-mint-recipient` (pattern:
  `madder-test-craft-legacy-blob`, test-only, never shipped): from a
  32-byte seed prints the X25519 secret and the
  `piggy-recipient-v1@age_x25519_pub-…` id; also prints a deterministic
  `piggy-recipient-v1@pivy_ecdh_p256_pub-…` id from a fixed scalar, so
  the fixture pigpen has a P-256 line the Rust side must parse but can
  never open.
- Recipe `codemod-rust-fixtures`: in a scratch `XDG_*`, writes the
  `piggy-ids` file, `madder init -pigpen piggy-ids -pigpen-kind embedded`
  (no absolute path in the sidecar, so the fixture is hermetic), writes
  blobs (empty, 1 byte, short text, 200 KiB pseudo-random to cross a
  64 KiB STREAM chunk and a zstd block boundary), and copies the store
  plus the plaintexts, the seed and the expected ids into
  `crates/madder-store/tests/fixtures/`. A second store with
  `-compression-type none -hash_type-id sha256` covers the other codec
  and hash.
- Rust tests: config digest, sidecar recipients (two), unseal with the
  seed identity, every blob byte-exact, `has`, `ids`, re-write of each
  plaintext yields the same id (exercises EEXIST), tamper cases (body
  digest, wrong key, truncated age payload).

Rust -> Go (bats): `zz-tests_bats/pigpen_rust_interop.bats`, tag
`rust_interop`, lane overlay = `pivAgentExtraBinaries` + the Rust CLI.
Go's reader opens a sealed key only through the agent (R:
`store_key.AgentOpener` passes no software identities), so reading
Rust-written blobs in Go needs fibby's P-256 card: the pigpen is fibby's
recipient plus an X25519 recipient the Rust CLI mints, Go inits, Rust
writes, `madder cat` and `madder fsck -format ndjson` verify; Rust also
`cat`s the same blobs with the X25519 identity and no agent. Linux only,
like `test-bats-piv-agent`. If Decision 4 lands, the same file covers a
Rust-initialized store read by Go.

Justfile: `test-rust` (`nix build .#madder-store`), `test-rust-wasm`
(the wasm32 check), `test-bats-rust-interop`; all join `test`, and the
clippy lane joins `lint-worktree`. `default` stays `lint build test`.

## Anything not pinned by FDR 0011 or a vector

- The temp directory location is an implementation detail in Go (XDG
  cache), not a wire fact; the sibling-dir default above is my choice
  and is configurable.
- Age writer choices that are not observable to Go but will differ
  between implementations: Go's filippo/age writes the X25519 stanza
  body on one line and a 16-byte random nonce; chunking is fixed by the
  spec. Nothing on disk is byte-identical between two writers anyway
  (random ephemeral key and nonce), which is why the acceptance test is
  "the other side reads it", not byte equality.
- `instance-id` is opaque to this crate; if pigsty ever needs to compare
  store identities it compares the string.

## Plan of record (TDD per task, merge when green)

1. Design doc (this file) + fixture binary + `codemod-rust-fixtures` +
   committed fixtures. No Rust yet.
2. Crate skeleton from the template, workspace, nix build, devShell
   toolchain, clippy lane; `blob_id` + `hyphence` + `config` + `sidecar`
   with tests against the fixtures.
3. `age_x25519` + `codec` + `read`/`has`/`ids` reading the fixtures.
4. `publish` + `write`; bats interop lane; justfile gate; manpage.
5. `init` if approved; `update-piggy` learns the Cargo pin.
