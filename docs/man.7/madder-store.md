---
author:
-
date: October 2026
title: MADDER-STORE(7) Madder \| Miscellaneous
---

# NAME

madder-store - Rust reader and writer for madder's local blob stores

# DESCRIPTION

**madder-store** is the Rust crate at **crates/madder-store** that reads
and writes a madder **local hash-bucketed** store byte-compatibly with
the Go **madder**: the **sealed-key** store (**blob-store**(7) SEALED-KEY
STORES, FDR-0011) and the plain V3/V4 stores **madder init** writes by
default, unencrypted or with a raw key in the config. A program that
cannot link Go can thus use such a store as its backing storage. Its
first consumer is **pigsty**, a Linux and
Android password vault that keeps hyphence entry documents and
attachments as blobs. The crate is pure Rust (the RustCrypto stack
piggy's **piggy-pigpen** crate already uses) with no system libraries, so
it builds for **aarch64-linux-android** and is kept **wasm32**-portable:
filesystem access sits behind a small **Fs** trait with a **std::fs**
implementation, and nothing is async.

The crate implements exactly the on-disk facts the Go code pins:

- **blob_store-config** of type **toml-blob_store_config-v3**, **-v4**
  or **-v5**, whose **@** line (when present) is the **blake2b256**
  digest of the body bytes after the last **---** boundary plus blank
  line; a mismatch is **Error::ConfigTampered**. A v5 config (the
  sealed-key store) must name the **process** key holder and exactly one
  **age_x25519_pub** public key. A v3/v4 config (a plain store) has no
  **key-custody** table and either no **encryption** (compressed
  plaintext on disk) or exactly one **age_x25519_sec** key, the
  **madder init -encryption generate** shape (also accepted spelled as a
  single string, and with **hash_buckets** absent, meaning no
  bucketing); several keys (Go's age MultiIdentity) and the v1/v2 config
  shapes are refused.
- **blob_store-key** of type **toml-blob_store_key-v2** (v1 is read),
  present for a sealed-key store only: the recipient source (**path**,
  **papi**, **embedded**) and the sealed **pigpen-v1** document, base64
  on disk, whose 32-byte payload is the store key's secret half.
- Blob paths **\<base\>/\<hash-format\>/\<buckets\>/\<rest\>** from the
  lower-case hex digest (multi-hash layout; **single_hash** drops the
  format directory). The digest of zero bytes is the null id: nothing is
  stored for it and it reads back empty, as in Go.
- Blob bytes: plaintext, then compression (**none** or **zstd**; gzip
  and zlib are refused), then, for an encrypted store, an age v1 file
  with one **X25519** stanza to the store's public key (a sealed-key
  store's, or the public half of a plain store's raw key). The age
  subset is hand-written on the same RustCrypto crates; it refuses any
  other stanza type, several stanzas, or a missing header MAC rather
  than guessing.
- Publishing per **blob-store**(7) CONCURRENCY AND DURABILITY: temp
  file, **fsync**, **chmod 0444**, **link**(2) into the tree with
  **EEXIST** treated as the same bytes already published, **fsync** of
  the directory. Temp files default to a sibling directory of the store,
  **\<parent\>/.\<name\>.tmp**, so they are on the same filesystem and
  outside the walk **madder cat-ids** performs; **OpenOptions.temp_dir**
  overrides it (an Android app passes its cache directory).

# API

**Store::open(base)** parses and verifies the config, and for a
sealed-key store the sidecar, and unseals nothing. **config()** carries
the **ConfigVersion** and the **Encryption** (**None**, **Key** or
**Sealed**). **sealed_recipients()** and **recipient_source()** are the
read half of **madder key-status** and need no agent (empty and absent
for a plain store). **unseal(&Unsealer { oracle, identities })** opens
the sealed key once, through an injected **piggy_pigpen::EcdhOracle**
for a PIV recipient or a software **X25519Identity**, and checks the
result is the secret half of the config's public key
(**Error::WrongKey** otherwise); on a plain store it is a no-op and
**is_unsealed()** is true from the start. **write(reader)** needs no key
and returns the **BlobId**; **has(id)**, **ids()** and **read(id)**
(digest-verified at EOF; **Error::Locked** before **unseal** on a
sealed-key store) complete the surface. A blob's id is the digest of
its plaintext, so compression never changes an id.

**madder_store::init** creates a new store from an **InitEncryption**:
**Sealed** (sidecar first, then the digest-pinned v5 config), **None**
or **Key** (a v4 config). The config is written read-only (mode 0444),
as Go's **WriteImmutable** does. pigsty on Android has no Go **madder**
to shell out to.

Out of scope: **key-reseal**, **config-gen**, remote store types,
inventory archives, pointer and multi stores, the drift check against a
re-resolved pigpen, **verify-on-collision**, several raw keys in one
config, and the v1/v2 config shapes.

# TEST CLI

**madder-store-cli**, the crate's binary target (cargo feature
**agent-cli**, never shipped), speaks the subset of **madder**'s grammar
the store bats tests use: **init** (**-pigpen**, or **-encryption
none|generate**), **write** (TAP or NDJSON, **-check**, **-** for
stdin), **cat**, **cat-ids**, **has**, **fsck**, **list** (text, ndjson
and json over local stores; **-tree** and **-all** are Go-only),
**info-repo**, **key-status**, with blob-store-ids in the CWD scope
(**.name**) and the
XDG user scope (a bare **name**, or **~name** when a file in CWD shadows
it). With no id named it uses the default store the way Go's discovery
picks it (the alphabetically first store that opens), **cat** and
**has** fall back to the remaining stores, **write** warns when a file
argument shadows a store's name, and **init** fails by name on a local
store config it cannot decode, as Go's discovery does. It opens a sealed key with
**MADDER_STORE_IDENTITY_HEX** (an X25519 secret) when set, else through
piggy-agent via **piggy_pigpen::agent** (**PIGGY_AUTH_SOCK**,
**SSH_AUTH_SOCK**, **PIVY_AUTH_SOCK**). Its purpose is to let the
existing bats files run against the Rust implementation; madder's own
CLI surface beyond the store (multi and archive stores, **pack-blobs**,
**list -tree**) is not reproduced (madder#323 for the multi half of
**list**).

# CONFORMANCE

**just test-rust** builds the crate and runs **cargo test** in the nix
sandbox: unit tests, the Go-written fixture stores under
**crates/madder-store/tests/fixtures** (two sealed-key and two plain
stores, regenerated by **just codemod-rust-fixtures** from the test-only
**madder-test-mint-recipient** binary and **madder init**) read back
byte-exact, and age files round-trip with the nixpkgs **age** CLI.
**just test-bats-rust-cli** runs every bats test tagged **rust_cli**
(in **pigpen_store**, **write_read**, **fsck**, **init**,
**cli_contract**, **info_repo** and **list**) with **MADDER_BIN** pointed at the
Rust test CLI, so the same tests run against both implementations.
**just test-bats-rust-cli-identity** runs them once more with
**pigpen_store.bats** in its agent-less mode
(**MADDER_STORE_IDENTITY_MODE**): the pigpen names an X25519 recipient
the CLI mints and the sealed key opens through
**MADDER_STORE_IDENTITY_HEX**, with no fibby, piggy-agent or card, so the
read path is covered as a host without an agent would run it. **just
test-bats-rust-interop** runs **zz-tests_bats/pigpen_rust_interop.bats**
against one live store in both directions, with the real piggy-agent
over fibby. **just test-rust-wasm** is the portability gate: a **cargo check** of the
library alone, default features, for **wasm32-unknown-unknown** (a
target nixpkgs' **rustc** ships) over the same vendored **Cargo.lock**
(**.#madder-store-wasm-check**). It proves the dependency closure and
the **Fs** seam compile without unix sockets, tokio or C;
**aarch64-linux-android** is left to **pigsty**'s toolchain, which
builds the crate for Android. **just lint-worktree** runs clippy
(**conformist-impure.nix**); **nix fmt** runs rustfmt.

The piggy crates are Cargo git dependencies meant to sit at the rev of
madder's **piggy** flake input; **just update-piggy** moves flake.lock,
go.mod and the Cargo pin together. When the two are apart, the comment
in **crates/madder-store/Cargo.toml** says why.

# SEE ALSO

**blob-store**(7), **markl-id**(7), **madder-key-status**(1),
**madder-init**(1)

FDR 0011: Pigpen-sealed store key
(docs/features/0011-pigpen-sealed-store-key.md)

Design: docs/plans/2026-10-25-rust-sealed-key-store-design.md
