# madder-store: a Rust reader/writer for sealed-key local stores

Status: confirmed by piggy's lane and implemented (2026-10-25); the
"Decided after the first implementation" section below records what
changed on the way.
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
  <digest>\n---\n\n` then a TOML body. The `@` digest is the bare
  `blake2b256-<blech32>` of the body bytes (V: the fixture shows no
  purpose prefix; Go's `Id.String()` drops the purpose and
  `markl.AssertEqual` compares format and bytes only), where the body
  starts after the LAST `---\n\n` in the file (`DecodeAndVerify`). The
  Rust parser accepts an optional purpose on that line and ignores it. A
  config with no `@` line is trusted (legacy).
- The null blob: Go treats the digest of zero bytes as the null id,
  writes no file for it, reports `has` true and reads it as empty (R:
  `blob_io/mover.go`, `store_local_hash_bucketed.go`; V: `madder write`
  of an empty file prints no id). The crate mirrors that.
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

Justfile: `test-rust` (`nix build .#madder-store`),
`test-bats-rust-interop` and `test-bats-rust-cli` join `test`, and the
clippy lane joins `lint-worktree`. `default` stays `lint build test`.
The wasm32 (and Android) `cargo check` lane is not built yet: madder#320.

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

## Decided after the first implementation (Sasha, 2026-10-25)

- **The existing bats files are the Rust conformance suite too.** The
  crate's test binary, `madder-store-cli`, speaks the subset of madder's
  grammar the sealed-key tests use (`init -pigpen`, `write -format tap`,
  `cat`, `cat-ids`, `fsck -format ndjson`, `key-status`; CWD-relative
  `.name` ids only), and `.#bats-rust_cli` (`just test-bats-rust-cli`)
  runs the tests of `zz-tests_bats/pigpen_store.bats` tagged
  `# bats test_tags=rust_cli` with `MADDER_BIN` pointed at it. V: 10 of
  the file's 17 tests pass against Rust unchanged, including the reads
  through the real piggy-agent over fibby with one card ECDH per
  process. The other 7 are out of the crate's scope: `key-reseal` (4),
  the drift warning and pigpen pointers (2), and a plain store (1).
  madder#318 (plain-store files, reopens decision 5) and madder#319 (an
  X25519-identity variant of the agent tests) are sequenced at the end
  of the queue.
- **Agent oracle.** piggy landed `piggy_pigpen::agent::{AgentEcdhOracle,
  resolve_auth_sock}` behind the non-default `agent` feature (pure std
  `UnixStream` + p256; piggy rev 9ebebd31a74e). The crate exposes it to
  its CLI only, through the cargo feature `agent-cli` the bin target
  requires; the library's default build stays feature-free and
  wasm-clean, and pigsty wires its own oracle.
- **Two piggy pins, one bump.** The crate first merged with Cargo.lock
  at piggy 9ebebd31a74e (the oracle) and flake.lock at d28e399b (the Go
  side); `just update-piggy 9ebebd31a74e…` then realigned them. The
  recipe rewrites the rev in `crates/madder-store/Cargo.toml` and runs
  `cargo update` on the two crates alongside the flake.lock and go.mod
  moves; its Cargo half has run once, for that bump. Note the flake half
  is `nix flake update piggy`, which lands on piggy master's head, not
  on the sha passed: the two agree only when the sha is the head.
- **init is in**: `madder_store::init` writes the sidecar then the
  digest-pinned config, hand-rendering the sidecar's two long fields as
  TOML basic multi-line strings (the shape Go writes and the bats tests
  grep for). V: Go `list`, `key-status`, `write`, `cat` and `fsck` all
  accept a Rust-initialized store (`pigpen_rust_interop.bats`).

## Decision 5 reopened: plain stores (madder#318; Sasha, 2026-10-25)

Sasha's scope (asked and answered 2026-10-25): make the **store-behaviour**
tests of the plain-store bats files pass against the Rust CLI shim; the
crate learns **V4 read+write and V3 read**; addendum first, then build.
The rest of those files is split off: madder#321 (whole files: the arg
resolver's shadow warnings, `info-repo` in full, discovery error
reporting) and madder#322 (`list.bats`), both sequenced after madder#319
and madder#320 unless the parent or the coordinator orders otherwise.

On-disk facts, from the Go code (R):

- `toml-blob_store_config-v3` and `-v4` share `hash_buckets`,
  `hash_type-id`, `encryption` (an array of markl ids; empty means no
  encryption layer), `compression-type`, `verify-on-collision`,
  `single_hash`, `base_path`; V4 adds `instance-id`. V4 is
  `TypeTomlBlobStoreConfigVCurrent`: `madder init -encryption
  none|generate` writes V4 with the `@` digest and a uuidv7 instance id,
  through `files.WriteImmutable` (mode 0444). V3 upgrades to V4 in memory
  on read, fields copied verbatim. V1/V2 (a single `encryption` string)
  stay out: Unsupported, loudly.
- `encryption = []`: `EncryptionKeys.GetIOWrapper` returns nil and
  `blob_io.MakeConfig` uses the identity wrapper, so the file on disk is
  the compressed plaintext and nothing else.
- One key, `madder-private_key-v1@age_x25519_sec-<blech32>` (32-byte
  X25519 scalar): piggy's `AgeX25519GetIOWrapper` re-encodes it as an
  `AGE-SECRET-KEY-` string and hands it to dewey's `age.Identity`, a
  filippo.io/age `X25519Identity`: encrypt to its recipient, decrypt with
  the identity. The blob is the same age v1 single-X25519-stanza file the
  sealed path writes, to the public key of that scalar; no sidecar, no
  unseal. Two or more keys are `age.MultiIdentity` (several stanzas) and
  are refused here.
- The default store is the alphabetically first discovered store whose
  backend built (`blob_store_env.selectDefaultBlobStoreId`); `cat` and
  `has` fall back to the remaining stores in id order when the blob is not
  in the active one; `fsck` with no argument checks every store.
- Output shapes the tests pin: `write`'s NDJSON record is `id`, `size`,
  `source`, then `store` only when a store id was named on the command
  line, `present` under `-check`; `-format` other than tap/json/ndjson is
  "unsupported output format"; TAP on a terminal, NDJSON on a pipe.
  `has` prints `<id>\tfound\t<store-id>` or `<id>\tnot found`, refuses
  `-all` with an explicit store, exits non-zero on a miss. `info-repo
  <key>` prints Go's `ConfigKeyValues` entry (`encryption` is the keys'
  `StringWithFormat` joined by ", ", empty for none).

Crate changes:

- `Config` carries a version and `encryption: Encryption::{None,
  Key(secret), Sealed(public)}`; `public_key()` is derived for a `Key`
  store. `Config::parse` accepts v3, v4 and v5 with per-version checks
  (v5 as before; v3/v4: zero or one `age_x25519_sec` id, no
  `[key-custody]`).
- `Store::open` reads the sidecar only for a sealed-key config. A plain
  store is unsealed from the start (`unseal` is a no-op, `is_unsealed`
  true), has no sealed recipients, and `recipient_source()` becomes an
  `Option`. `read` skips the age layer for `None` and decrypts with the
  config's secret for `Key`; `write` mirrors it.
- `init` takes an `InitEncryption::{Sealed{recipients, source}, None,
  Key(Option<scalar>)}` and writes V4 for plain stores, V5 for sealed; the
  config is chmod 0444 in both cases (Go's `WriteImmutable`; the sealed
  sidecar stays writable, as Go leaves it).
- Fixtures: two more Go-written stores, `plain-none-v4` and
  `plain-key-v4`, from `just codemod-rust-fixtures`.
- The shim gains store discovery under `$PWD/.madder/local/share/
  blob_stores/`, the default-store rule, `write` to the default store with
  `-` for stdin, `-check`, Go's record fields and terminal detection,
  `has`, `cat`'s fallback, `fsck` over every store, `info-repo [.store]
  <key>` (plus the raw config for a lone store id), and `init -encryption
  none|generate`.
- Tests tagged `rust_cli`: the store-behaviour subset of `write_read`
  (9), `fsck` (4), `init` (7), `cli_contract` (9) and, since the shim
  grows `info-repo <key>` for `init.bats` anyway, `info_repo` (4).
  V: all 43 `rust_cli` tests (these 33 plus pigpen_store's 10) pass
  against the Rust shim unchanged; `pigpen_rust_interop.bats` gained a
  plain-store test (Go-inited stores of both shapes read and written by
  Rust, Rust-inited ones by Go) and its 7 tests pass; `cargo test` is at
  55.

## The identity variant of the agent tests (madder#319)

`pigpen_store.bats` gains an agent-less mode, switched by
`MADDER_STORE_IDENTITY_MODE` in the lane's environment: `setup` mints an
X25519 recipient with the Rust CLI instead of starting fibby and
piggy-agent, the pigpen names it, `MADDER_STORE_IDENTITY_HEX` hands the
CLI the secret, and the card log is an empty file so every card count is
0 (`expected_card_ecdh` makes the two "exactly one ECDH" assertions
expect 0 in that mode). `run_madder_no_agent` also unsets the identity,
so the "no agent socket" failure path is still exercised. The lane
`.#bats-rust_cli_identity` (`just test-bats-rust-cli-identity`) runs every
`rust_cli` test that way and carries no piggy binaries: it is the proof
that the Rust read path needs no agent, which is the shape pigsty on a
host without piggy-agent (or a wasm target) runs in. The Go lanes see the
mode unset and behave as before.

## The portability gate (madder#320)

`.#madder-store-wasm-check` (`just test-rust-wasm`, in the `test`
aggregate) is a `cargo check --lib -p madder-store --target
wasm32-unknown-unknown` with default features, on nixpkgs' stock
`rustPlatform` (its rustc ships the wasm32 target; the `rustc-wasm32`
attribute the issue named is an alias of `rustc` now) over the same
vendored Cargo.lock as `.#madder-store`. V: the library and its whole
closure, piggy-pigpen included, check clean for the target. Default features keep piggy-pigpen's
`agent` oracle (std `UnixStream`) out, as pigsty's build does. The lane
checks and installs a marker file; it builds and runs nothing, since
wasm32-unknown-unknown has no runtime here. Decision: aarch64-linux-android
is not cross-checked in madder. A `cargo check` for it needs that
target's std and a build needs the NDK linker, and pigsty's own toolchain
builds the crate for Android as its primary target, so a second copy of
that setup here would add maintenance and no assurance.

## The rest of the plain-store files (madder#321)

Three more tests from the plain-store files run against the shim; the
`list` tests stay with madder#322 and the inventory-archive and
pack-blobs tests stay Go-only.

- **Two scopes.** The shim resolves `.name` (CWD scope) and a bare
  `name` or `~name` (XDG user scope, `$XDG_DATA_HOME/madder/blob_stores/
  <name>`, `~/.local/share` without XDG_DATA_HOME), discovers both
  directories, and sorts the id strings, which keeps Go's default-store
  rule (`.default` sorts before any bare name).
- **The shadow warning.** A file argument whose bare name is also a
  configured store's name gets Go's `FormatShadowWarning` text through
  the write sink (a TAP comment, or stderr in JSON mode), with the `~name`
  spelling as the unambiguous hint for a user-scope store and the
  prefixed id for any other, as `DisambiguatedString` renders them.
- **Strict discovery in init.** Go's init decodes every configured store
  first and fails by name on one it cannot decode. The shim does the same
  for local-store configs (v3/v4/v5 type lines) and passes over other
  store types, which are not this crate's to judge. For the broken-store
  fixture that test writes (`encryption = ""`, `hash-buckets`
  misspelled, `hash_type-id = ""`), `Config::parse` now accepts
  `encryption` as a single string and a missing `hash_buckets` (no
  bucketing, as Go reads it), so the error is the hash type's, and
  `Error::Unsupported` renders as "unsupported hash type …" the way Go's
  does.

## `list` for local stores (madder#322)

Sasha (2026-10-25): the local-store half first, the multi/`-tree` half
(madder#323) queued after it merges. The shim's `list` walks both scopes
in id order and renders what Go's `emitListText` / `emitListNDJSON` /
`emitListJSONObject` render for a local hash-bucketed store: the text
line `<id>@<digest>: local hash bucketed # path: <rel>` with the bare
`blake2b256-…` config digest, or `<id> (unmigrated)` plus the
`config-pin_digest` note when the config has no `@` line; the ndjson
record with `id`, `description`, `config_path`, `base` and `digest`
(`madder-blob_store-config-digest-v1@…`) or `digest_missing`; and
`-format json` as one object keyed by id. Output is sorted, so it is
byte-stable across runs. `-tree` and `-all` are refused by name. Eleven
more tests carry the `rust_cli` tag (57 total); the three multi tests
stay Go-only until madder#323.

## The multi half of `list` (madder#323)

The shim, not the crate, learns `toml-blob_store_config-multi-v1` (FDR
0009) for two purposes only: `list` and `init-multi`. It reads no blobs
through a multi store. `list` shows a multi with Go's description
(`multi/mirror(a,b)`, `multi/write-through(W=a, R=b)`, children looked up
by name among the listed stores), its ndjson record carries `mode`,
`read_fill` (write-through only) and `refs` (name, digest, role), and
`-tree` forces text and draws `[multi/<mode>]` with each reference
indented beneath as `└── <name>  <description>  (<role>)`. `init-multi`
takes Go's double-dash flags, pins each bare reference to the referenced
config's `@` digest in its canonical spelling (`.name@blake2b256-…`), and
writes the config 0444 with an instance id, through the crate's now
public `init::mint_instance_id`. This makes all of `list.bats` and
`init_multi.bats` run against the shim too. V: 64 `rust_cli` tests pass;
in the plain-store files only the inventory-archive inits and the
pack-blobs test remain Go-only.

## Hardening pass (circus/eager-holly's queue, 2026-10-26)

Scope confirmed with piggy/live-sequoia: defects rooted in piggy-pigpen
are piggy's to fix (file there, never patch around in madder); pigpen's
guarantees are cited, not re-derived.

- **Robustness tests** (`tests/robustness.rs`, a dependency-free
  xorshift PRNG, every case reproducible by seed): random bytes through
  every parser and the age/zstd readers; random mutations of every
  fixture config and sidecar, in memory and on disk; random damage to
  every fixture blob (flip, delete, insert, truncate, append), asserting
  a read is refused or returns the original bytes, never different ones,
  and that streaming and whole reads agree; sealed stores under one to
  four random recipients and blob sizes from 0 to 200 KB, each recipient
  alone opening the key and a stranger refused with the store left
  locked. V: all pass; no panic and no defect found, in the store or in
  pigpen, so nothing to file on piggy.
- **Threat model**: madder-store(7) THREAT MODEL states what the crate
  defends (plaintext at rest, blob integrity, the sealed key, pinned
  configs, refusal over guessing), what it leaves to the caller (key
  custody, the `Fs`) and what it does not defend (metadata, writers, a
  removed recipient, raw-key stores against a config reader, collisions,
  the host), citing RFC 0008 §6 and FDR 0011 LIMITATIONS by item.
- **Portability check**: done as madder#320 (wasm32); Android stays with
  pigsty's toolchain.
- **Issue triage**: madder#292 (two manpage NAME lines) is already clean
  on master, V: doppelgang `lint-man --max 72` over the built pages
  reports 52 pages, 0 errors (new paved path `just debug-lint-man`);
  madder#251 documented the multi-dot ancestor ids in blob-store(7);
  madder#269 got its nil guard in `Multi.MakeBlobWriter`; madder#276 got
  a root-cause note (an id with a broken checksum falls through to a
  silent store switch) and waits on a semantics decision; madder#315
  needs a reproduction first.
- **Joint bump note**: the section below.

## Note: a joint madder/piggy bump for pigsty phase 4 (2026-10-26)

Written at circus/eager-holly's request, agreed with piggy/live-sequoia;
no bump is made here.

- **Three pins, one rev.** madder holds piggy in flake.lock (the Go
  lanes), go.mod (the devShell's Go) and `crates/madder-store/Cargo.toml`
  + Cargo.lock (the Rust crate). `just update-piggy <full sha>` moves all
  three; its flake half is `nix flake update piggy`, which lands on
  piggy master's head, so the sha passed must be that head (or the recipe
  needs a `--override-input`-style pin before it can target an older rev).
  Today all three sit at 9ebebd31a74e28f2dd6c4095a2b6ba5d284cf408, which
  is still piggy master.
- **What pigsty's next bump will carry.** piggy#337 (OpenSSL and pcsc
  behind features in piggy-piv) and piggy#341 (a card-backed
  `piggy_pigpen::EcdhOracle` in piggy-piv behind a `pigpen` feature).
  Neither changes piggy-pigpen's or piggy-markl's API, so madder's Cargo
  deps are unaffected by both; only pigsty, which depends on piggy-piv,
  needs them. #341 adds a path dep piggy-piv → piggy-pigpen, which does
  not change how madder's git dep resolves.
- **Pin a rev, never a branch or tag.** piggy's tags are release tags of
  the piggy binary (`vX.Y.Z` and `go/vX.Y.Z`, latest v0.1.23, before the
  pigsty series); piggy-pigpen's own Cargo version is a static 0.1.0 that a
  release does not bump, so a tag marks a piggy release, not a pigpen API
  level. For a joint bump piggy cuts a release so the target rev is a tag's
  commit, and madder pins `rev = <sha>` with a comment naming the tag.
- **Order, each step after the previous lands.** (1) piggy lands what
  pigsty needs and releases; (2) madder runs `just update-piggy <sha>`
  (flake.lock, go.mod, Cargo rev and lock together) and its gate, which
  includes `.#madder-store`, `.#bats-rust_interop`, `.#bats-rust_cli`,
  `.#bats-rust_cli_identity` and `.#madder-store-wasm-check`, all of which
  a piggy bump must pass; (3) pigsty bumps madder and piggy to those two
  revs in one commit. madder bumps piggy only to a rev pigsty has asked
  for, so the three repos never hold two piggy revs at once.
- **Cost on madder's side.** A bump is a lock change plus the gate; no
  code moves unless piggy-pigpen's API does (it has not since
  9ebebd31). The one API edge madder depends on is
  `piggy_pigpen::agent` behind the `agent` feature (test CLI only) and
  `Document::{parse, seal, open, to_bytes, encryption_recipients}` plus
  `recipient_id`, `EcdhOracle`, `X25519Identity`; a change to any of
  those shows up in `.#madder-store` first.

## Plan of record (TDD per task, merge when green)

1. Design doc (this file) + fixture binary + `codemod-rust-fixtures` +
   committed fixtures. No Rust yet.
2. Crate skeleton from the template, workspace, nix build, devShell
   toolchain, clippy lane; `blob_id` + `hyphence` + `config` + `sidecar`
   with tests against the fixtures.
3. `age_x25519` + `codec` + `read`/`has`/`ids` reading the fixtures.
4. `publish` + `write`; bats interop lane; justfile gate; manpage.
5. `init` if approved; `update-piggy` learns the Cargo pin.
6. Plain stores (madder#318): fixtures, `Config` versions and
   `Encryption`, read/write/init, the shim's discovery and commands, the
   `rust_cli` tags.
7. The identity variant of the agent tests (madder#319): the mode in
   `pigpen_store.bats`, `mkBatsLane`'s `extraEnv`, the lane and recipe.
8. The portability gate (madder#320): the wasm32 `cargo check` lane.
9. The rest of the plain-store files (madder#321): two scopes, the shadow
   warning, strict discovery in init.
10. `list` for local stores (madder#322).
11. The multi half of `list` and `init-multi` in the shim (madder#323).
