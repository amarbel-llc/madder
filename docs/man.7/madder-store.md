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
sealed-key store the sidecar, and unseals nothing; it creates nothing
either. A folder whose files have not all arrived (a vault another
process is syncing) is told apart by error kind: **Error::NoConfig** (no
**blob_store-config**, so not a store yet), **Error::MissingSidecar**
(the config names a sealed-key store but **blob_store-key** is absent),
and, per blob, **Error::NotFound**; a half-copied blob is **Error::Age**,
**Error::Codec** or **Error::DigestMismatch**, never wrong bytes.
**Store::open_read_only(base)** (**OpenOptions.read_only**) is the mode
for such a folder: every write is **Error::ReadOnly** before anything is
touched, no temp directory is created, and the folder keeps exactly the
shape the sync tool left, so the reader leaves nothing for it to carry.
The crate takes no lock in either mode; a writer and a read-only reader
on one folder do not wait on each other, and the reader sees a published
blob on its next read. A write into a store whose **blob_store-config**
has gone (the folder removed or moved under the open handle) is
**Error::NoConfig** too, not a rebuilt skeleton of directories around one
blob; this is where the crate deliberately differs from the Go local
store, which recreates its directories on publish. **config()** carries
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

# THREAT MODEL

What the crate defends, what it leaves to other layers, and what it does
not defend at all. Guarantees that belong to piggy's pigpen format are
stated as pigpen's and cited to piggy RFC 0008 and madder FDR 0011 by
section; this page does not re-derive them.

The crate defends:

- **Blob plaintext at rest.** Every blob of an encrypted store is an age
  v1 file to the store's X25519 public key (age specification, STREAM
  construction). Reading needs the store's secret key: the sealed one
  (below) or, for a raw-key store, the one in its config.
- **Blob integrity.** A blob whose bytes were altered or truncated fails
  the age chunk authentication before any plaintext is handed on, and a
  blob that decrypts to other bytes fails the digest check at the end of
  the read (**Error::DigestMismatch**). An unencrypted store has only the
  digest check. The robustness tests (**tests/robustness.rs**) mutate
  every fixture blob at random and assert a read is refused or returns
  the original bytes, never different ones.
- **The store key, sealed.** In a sealed-key store the secret half is a
  pigpen-v1 document sealed to the recipients: only a holder of one
  recipient's key opens it (RFC 0008 §4.3, §4.4), a recipient cannot be
  added, dropped or swapped without invalidating the header MAC (RFC 0008
  §6 item 1), and the opened key is checked against the config's public
  key (**Error::WrongKey**) so a sidecar from another store is refused.
  A software identity that is not a recipient fails with
  **Error::Pigpen**, not with wrong bytes.
- **Config integrity, when pinned.** A config whose **@** digest line
  does not match its body is **Error::ConfigTampered**. A config with no
  such line is trusted as written (FDR 0008's unmigrated state).
- **Refusal over guessing.** Anything outside the implemented subset
  (other config versions, several raw keys, other age stanza types, other
  compression types, a pointer pigpen) is a loud error, never a lenient
  read, and random or damaged input to any parser is an error, not a
  panic.

The crate leaves to its caller:

- **Who holds the key.** The unsealed store key lives in the process for
  the life of the **Store** (FDR 0011 LIMITATIONS, "The store key is in
  madder's memory"); the only **key-custody** holder is **process**. The
  injected **EcdhOracle** is trusted to answer honestly; the crate cannot
  tell a card from a software oracle.
- **The filesystem.** Durability and atomicity rest on the **Fs**
  implementation honouring **fsync**, **link**(2) semantics and mode
  0444; a host **Fs** that does not is outside the crate's guarantees.

The crate does not defend:

- **Metadata.** Blob ids, their count and approximate sizes, the bucket
  layout, the recipient ids and the store's public key are cleartext
  (RFC 0008 §6 item 7 for the sidecar; the same holds for age files).
- **Writers.** Anyone holding the store's public key can write a blob
  the store will accept; neither age nor pigpen authenticates a sender
  (RFC 0008 §6 item 6). A plain unencrypted store accepts anything.
- **A removed recipient.** Removing a recipient and re-sealing stops
  that key from opening the *new* sidecar, and pigpen rotates the
  document's file key on removal (RFC 0008 §6 item 2), but the store key
  underneath is the same, so anyone who kept the old sidecar or the store
  key still reads every existing blob. Revocation is a new store and a
  full copy (FDR 0011 LIMITATIONS, "Revocation is not rotation"). This
  crate implements neither re-sealing nor rotation.
- **Raw-key and unencrypted stores against a reader of the config.** A
  V3/V4 store's secret key, when it has one, is in the config in the
  clear; whoever reads the config reads the blobs.
- **Collisions.** **verify-on-collision** is not implemented: a blob
  whose path already exists is taken to be the same bytes (madder ADR
  0002).
- **The host.** A compromised process, a debugger, swap, or a hostile
  **Fs** implementation see the unsealed key and every plaintext. Side
  channels beyond what the RustCrypto primitives provide are not
  considered.

# COMPATIBILITY

The crate owns no format. Everything it reads and writes is a shape the
Go **madder** defined and still writes, and the Go code plus the
Go-written fixtures are the contract: a change to any of these is a
madder format change first and a crate change second.

Frozen, in the sense that the crate reads exactly these and refuses
anything else:

- Config documents **toml-blob_store_config-v3**, **-v4** (plain stores;
  v4 is what **madder init** writes) and **-v5** (sealed-key store), in
  hyphence framing with the optional **@** blake2b256 body digest (FDR
  0008); the v1/v2 shapes are not read.
- Sidecar documents **toml-blob_store_key-v2** (written) and **-v1**
  (read), carrying a **pigpen-v1** document (piggy RFC 0008), base64 at
  64 columns.
- Blob bytes: zstd frames or raw bytes, inside an age v1 file with one
  **X25519** stanza when the store encrypts; the bucketed path from the
  lower-case hex digest, multi-hash or single-hash layout; the null id.
- Ids: markl **blake2b256** and **sha256** digests, **age_x25519_pub**
  and **age_x25519_sec** keys, the **uuidv7** instance id, in piggy RFC
  0011's wire form (piggy-markl).

What a format bump costs, for a consumer weighing one (pigsty's
signed-index question is the current case):

- A new hyphence type tag (**toml-blob_store_config-v6**, say) so old
  readers, Go and Rust, refuse the file rather than misread it; the Go
  coder, **Validate** and **init** path; this crate's **ConfigVersion**,
  parser and **init**; a Go-written fixture for it (**just
  codemod-rust-fixtures**) and the bats that exercise it against both
  binaries. The same shape for a sidecar or blob-layout change. Roughly a
  day's work across the two implementations, plus the coordination of
  landing Go first so the fixture exists.
- Nothing, if the change lives above the store. A blob is opaque bytes to
  madder: a signed index, a per-device signing key, or any other
  authenticated structure pigsty wants is pigsty's own blob format
  (**pigsty-index-v2**, in pigsty's terms), stored and read through the
  unchanged **Store** API. madder neither signs nor verifies blobs and
  should not: the store does not know who its writers are (THREAT
  MODEL, "Writers"). If the signing key's public half is to live in the
  pigpen beside the recipients, that is a piggy RFC 0008 question, not a
  madder one.

# PERFORMANCE

Measured with **just run-bench-rust**
(**crates/madder-store/benches/throughput.rs**, a release build, plain
**std::time**) on 2026-10-26 on the x86_64 Linux dev host, temp dir on
the same disk as the store, random (incompressible) bytes. Numbers are
per operation unless marked; a phone will be slower by a factor this
page does not know, so what matters is the shape.

| operation | plain store (zstd) | sealed-key store (zstd + age) |
|---|---|---|
| init (mint, seal, two files) | | 4.3 ms |
| open (config + sidecar parse) | 29 µs | 29 µs |
| open + unseal (software identity) | | 106 µs |
| write 1 KB | 2.1 ms | 2.6 ms |
| read 1 KB | 34 µs | 185 µs |
| write 1 MB | 32 ms (31 MB/s) | 29 ms (35 MB/s) |
| read 1 MB | 2.2 ms (450 MB/s) | 2.0 ms (500 MB/s) |
| write 50 MB | 1.24 s (40 MB/s) | 1.11 s (45 MB/s) |
| read 50 MB | 155 ms (320 MB/s) | 187 ms (270 MB/s) |
| has | 4 to 14 µs | 4 to 14 µs |
| open + unseal, 10 000 blobs in the store | | 190 µs |
| **ids()** walk, 10 000 blobs | | 11.7 ms |
| write 10 000 blobs of 1 KB | | 30 s (3.0 ms each) |

What would hurt a phone, in order:

- **Writes are fsync-bound for small blobs** (two fsyncs per blob, the
  file and its directory, per **blob-store**(7) CONCURRENCY AND
  DURABILITY): about 2 to 3 ms each here, so a vault saving an entry pays
  a few ms per blob and an import of 10 000 entries takes tens of
  seconds. Flash on a phone fsyncs slower than this disk. The protocol is
  the format's, not negotiable per store; batching several blobs into one
  save is the consumer's lever.
- **zstd encoding is the write ceiling for large blobs**: 40 to 45 MB/s
  here, pure Rust (**ruzstd**) with no SIMD paths, so a 50 MB attachment
  costs about a second on this host and several on a phone. Compression
  is a store-wide config choice: a store meant for attachments can be
  created with **Compression::None**, which also makes decompression
  free. Reads are not the problem: 270 to 500 MB/s.
- **Each sealed blob read pays an X25519 agreement**: age derives a
  per-file key from an ephemeral public key, so a sealed read of a 1 KB
  blob is about 150 µs more than a plain one here. Reading every entry of
  a 10 000-entry vault at once is therefore about 2 s of key agreement on
  this host. A vault should read its index blob and the entries on
  screen, not every blob on open.
- **read_to_vec** holds the whole plaintext; a 50 MB attachment is 50 MB
  of heap. Stream large blobs through **BlobReader** (checking the digest
  at EOF before trusting them) instead.
- Opening is cheap regardless of store size (**open** reads two files,
  **unseal** is one agreement), and **ids()** walks the tree in about a
  microsecond per blob; neither needs care.

# TEST CLI

**madder-store-cli**, the crate's binary target (cargo feature
**agent-cli**, never shipped), speaks the subset of **madder**'s grammar
the store bats tests use: **init** (**-pigpen**, or **-encryption
none|generate**), **write** (TAP or NDJSON, **-check**, **-** for
stdin), **cat**, **cat-ids**, **has**, **fsck**, **list** (text, ndjson,
json and **-tree**, over local stores and multi configs; **-all** is
Go-only), **init-multi**, **info-repo**, **key-status**, with
blob-store-ids in the CWD scope
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
existing bats files run against the Rust implementation. Multi configs
are read for **list** and written by **init-multi** only; the shim reads
no blobs through a multi store, and inventory archives, **pack-blobs**
and the host-wide registry are not reproduced.

# CONFORMANCE

**just test-rust** builds the crate and runs **cargo test** in the nix
sandbox: unit tests, the Go-written fixture stores under
**crates/madder-store/tests/fixtures** (two sealed-key and two plain
stores, regenerated by **just codemod-rust-fixtures** from the test-only
**madder-test-mint-recipient** binary and **madder init**) read back
byte-exact, and age files round-trip with the nixpkgs **age** CLI.
**just test-bats-rust-cli** runs every bats test tagged **rust_cli**
(in **pigpen_store**, **write_read**, **fsck**, **init**,
**cli_contract**, **info_repo**, **list** and **init_multi**) with **MADDER_BIN** pointed at the
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
