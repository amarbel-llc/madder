---
status: experimental
date: 2026-10-05
promotion-criteria: |
  Promoted to `experimental` 2026-10-05: piggy froze pigpen-v1 (RFC 0008
  accepted) and landed the Go helpers, madder pins that work, and a LOCAL
  store can be initialized against a pigpen, written to without an
  agent, and read back through the real piggy-agent with one ECDH per
  process (zz-tests_bats/pigpen_store.bats, piv_agent lane). Promote to
  `testing` once `key-status`, `key-reseal` and the drift warning exist
  and remote (sftp) stores are supported. Promote to `accepted` once a
  recipient has been added to a real store by re-sealing with no blob
  rewrite, and one remote store has run on this design with no secret in
  its remote config (madder#296).
---

# Pigpen-sealed store key

> **Implementation status (2026-10-05).** Built: the store-key crypto, the
> `TomlV5` config and `blob_store-key` sidecar, and `madder init -pigpen`
> plus the read path for **local** stores. Not built yet, though
> described below: `key-status`, `key-reseal`, the drift warning, and
> `-pigpen` on the remote store types (sftp, webdav, s3).

## Problem Statement

An encrypted blob store keeps its age X25519 secret key in its own
`blob_store-config`. For a remote store (sftp, webdav, s3) that config
lives on the remote, so the secret sits next to the ciphertext and the
encryption protects nothing against anyone who can read the remote path
(madder#296; ADR 0005's key-blind layering is unimplemented).

The alternative that exists today, a single `pivy_ecdh_p256_pub`
recipient, keeps secrets off the remote but ties the store to one PIV
card, rejects a second recipient, and costs one card operation per blob
on read. A 91,500-blob fsck is 91,500 serialized card operations.

Operators already maintain the set of keys that should be able to read
their data as a piggy pigpen recipient document (their PIV slot-9D keys,
plus age recipients). A store should be encrypted to that set, be
readable by any enrolled key through the agent, and let the set change
without rewriting every blob.

## Interface

### Model

The store has one X25519 **store key**. Blobs are unchanged: each is an
ordinary age file encrypted to the store key's public half.

The store key's secret half is never stored in the clear. It is the
32-byte payload of a **sealed pigpen-v1 document**, sealed to the
encryption recipients of the operator's pigpen recipient document.

- **Write** needs only the store public key. No agent, no card.
- **Read** opens the sealed document once per madder process, with one
  agent ECDH call (`ecdh@joyent.com`) for a P-256 recipient or a local
  age identity for an X25519 recipient. The store key then lives in
  madder's process memory and every blob decrypts in software.

### On-disk shape

A sealed-key store's immutable `blob_store-config` is its own config
version, `toml-blob_store_config-v5`. It is not the default: ordinary
stores are still written as v4, so only stores created with `-pigpen`
need a madder new enough to know v5. It carries public material only:

    encryption = ["piggy-recipient-v1@age_x25519_pub-…"]

    [key-custody]
    holder = "process"

`encryption` carries the store **public** key. `key-custody.holder`
names who holds the unsealed store key at run time. `process` (madder's
own memory) is the only value this record defines. The field exists so a
later holder (a key-holding agent, or a fibby-backed virtual card) can
be added as a new value without a new config type.

The sealed key lives in a mutable sidecar next to the config,
`blob_store-key`, because it changes when recipients change and the
config must not (ADR 0005 immutability, FDR 0008 digest pins):

    ---
    ! toml-blob_store_key-v1
    ---

    [recipients]
    source = "<absolute path of the pigpen given to init>"
    digest = "blake2b256-…"

    [sealed]
    document = """
    <sealed pigpen-v1 document>
    """

`recipients.source` is always a path. A remotely hosted pigpen is
reached through a pointer document at that path, which piggy resolves.
`recipients.digest` is the digest of piggy's canonical recipient-set
bytes at seal time. For a remote store both files live at the remote
root; neither holds a secret.

`init` writes the sidecar before the config, and refuses outright if the
store already exists, so an existing store's sidecar is never replaced
by a second `init`.

### Commands

- `madder init[-sftp-*|-webdav|-s3] -pigpen <source> <store>` mints a
  store key, resolves `<source>` to a recipient set, seals the key to
  it, and writes the config and sidecar. `<source>` is a path to a
  `piggy-ids` file in any of its three forms (RFC 0003 lines, pigpen
  recipient set, pigpen pointer). `-pigpen` and `-encryption` are
  mutually exclusive.
- `madder key-status <store>` re-resolves the recorded source and
  reports the sealed recipient set, the current one, and whether they
  differ. Needs no agent.
- `madder key-reseal <store>` opens the sealed document through the
  agent, re-resolves the source, seals the same store key to the current
  recipient set, and replaces the sidecar atomically. No blob is
  touched.

### Drift

On every command that opens the store for reading or writing, madder
re-resolves the recorded source and compares its canonical recipient-set
digest with `recipients.digest`. On a mismatch it prints a warning
naming the store and pointing at `key-status` and `key-reseal`, then
proceeds. It never re-seals on its own. If the source cannot be resolved
(missing file, resolver failure, timeout) madder warns and proceeds with
the sealed set it has.

### Agent socket

Madder asks piggy to resolve the agent socket: `PIGGY_AUTH_SOCK`, then
`SSH_AUTH_SOCK`, then `PIVY_AUTH_SOCK`. No socket is needed to write.

## Examples

Create a remote store sealed to the recipients of a password store's
pigpen, then push to it with no card present:

    $ madder init-sftp-ssh_config -host backup -remote-path Library/superior \
        -pigpen ~/.password-store/piggy-ids .superior
    $ madder sync -format ndjson baikal .superior

Read it back. The first blob triggers one agent call; the rest do not:

    $ madder fsck .superior

A YubiKey is enrolled in the pigpen later:

    $ madder cat .superior blake2b256-…
    # (blob_store: .superior) recipient set has changed since the store
    # key was sealed; run `madder key-status .superior`
    $ madder key-status .superior
    sealed to:   2 recipients (blake2b256-aaaa…)
    source now:  3 recipients (blake2b256-bbbb…)
    added:       piggy-recipient-v1@pivy_ecdh_p256_pub-…
    $ madder key-reseal .superior

## Limitations

- **Revocation is not rotation.** Removing a recipient and re-sealing
  stops that key from opening the *new* sidecar. Anyone who kept the old
  sidecar, or the store key itself, can still read every existing blob.
  Real revocation needs a new store key and a rewrite of every blob,
  which today means a new store and a full `sync`. This record does not
  add an in-place rotation command.
- **The store key is in madder's memory** for the life of the process.
  The card gates the session, not each blob. Moving the key out of
  madder's address space is the job of a future `key-custody.holder`.
- **Whole-store granularity.** One key per store. There are no per-blob
  or per-prefix recipient sets.
- **Sealed payload is whole-buffer.** piggy's Go `Seal`/`Open` are not
  streaming. That is fine for a 32-byte key and is why blobs are not
  themselves pigpen documents.
- **Pointer resolvers are external.** A remotely hosted pigpen resolves
  through a `pigpen-resolver-<kind>` binary on `PATH` (piggy RFC 0010).
  Madder ships none and trusts the resolver's output.
- **Multi stores** do not get a store key of their own; each member
  store is sealed independently.
- **Existing stores are not converted.** A store with a raw
  `age_x25519_sec` in its config keeps working. Moving it to this design
  is a new store plus a sync.
- **Depends on unreleased piggy work.** Nothing here can be built until
  piggy freezes the pigpen-v1 bytes. Madder will not persist the sealed
  format before then.

## Tuning Levers

| Lever | Current | Rationale | Change signal |
|---|---|---|---|
| Drift response | warn and proceed | a changed pigpen must not make a backup unreadable or block a push | a recipient removal goes unnoticed long enough to matter; then refuse writes, or add a strict flag |
| Drift check frequency | every store open | recipient changes should be seen on the next use | resolver latency or failures become a visible cost on ordinary commands; then cache or check only on `key-status` |
| Resolver timeout | to be set when built | pointer resolution may make a network call | timeouts on a healthy network, or hangs that stall commands |
| Sidecar vs in-config | sidecar file | keeps the config immutable and digest-pinnable | the two files drifting apart on remotes proves worse than a mutable config |
| Key holder | `process` only | accepted as sufficient for now | a key-holding agent or fibby-backed holder exists in piggy |

## More Information

- madder#296: remote stores write the encryption secret into the remote
  config. This record is the intended fix for stores created with
  `-pigpen`.
- `docs/decisions/0005-remote-driven-sftp-blob-stores.md`: the key-blind
  layering this implements.
- FDR 0008 (config digest pins): why the sealed key cannot live in the
  config.
- piggy RFC 0008 (pigpen), RFC 0009 (phases), RFC 0010 (pointer
  resolvers). All draft at the time of writing.
- piggy plan `docs/plans/2026-10-05-pigpen-go-for-madder.md` (in review):
  the Go API this record assumes: `pigpen.ParseRecipients`,
  `Document.EncryptionRecipients`, `CanonicalRecipientSet`,
  `pigpen_resolve.LoadRecipients`, `agent.AgentECDHOracle`,
  `agent.ResolveAuthSock`, and an encrypt-only IO wrapper for
  `age_x25519_pub`.
- Decisions recorded 2026-10-05 (Sasha): session-gated card access is
  acceptable; piggy freezes the format before madder persists it; drift
  is detected, not auto-repaired; the pigpen may be remotely hosted; the
  config names the key holder so a later holder can be added.
