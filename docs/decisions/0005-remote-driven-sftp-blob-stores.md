---
status: accepted
date: 2026-04-25
decision-makers: Sasha F
---

# Remote-driven SFTP blob stores; eliminate remote-opaque mode

## Context and Problem Statement

Madder's SFTP-backed blob stores live today in an unnamed mixture of two latent operating modes:

* **Mode A — remote-opaque.** SFTP is dumb file transport. The remote filesystem stores arbitrary bytes; the *local* config is the source of truth for everything blob-store-shaped (hash type, buckets, compression, encryption). Different clients can write to the same path with different settings; the remote is happily oblivious. Closer to how rclone / rsync think.
* **Mode B — remote-driven.** The remote root carries `blob_store-config`; all clients read it and conform. One canonical truth per remote store. Closer to how restic / borg think.

The current implementation is mostly Mode B but inconsistently:

| Property | Today | Mode |
|---|---|---|
| `hash_type-id` | `*remoteSftp.defaultHashType` populated from remote config in `readRemoteConfig`. | **B** |
| `hash_buckets` | `*remoteSftp.buckets` populated from remote config. | **B** |
| `compression-type` | Carried in remote config wire format; `*remoteSftp.makeEnvDirConfig` has a TODO and currently uses `env_dir.DefaultConfig` regardless. | **B nominally, A in practice** |
| Encryption | `TomlSFTPV0` has no encryption field; remote config has none specified; `blobIOWrapper` is `nil` with a TODO. Gates [#57](https://github.com/amarbel-llc/madder/issues/57). | **undecided** |

The mixed state has produced four bugs in quick succession ([#57](https://github.com/amarbel-llc/madder/issues/57), [#58](https://github.com/amarbel-llc/madder/issues/58), [#59](https://github.com/amarbel-llc/madder/issues/59), [#60](https://github.com/amarbel-llc/madder/issues/60)), all of them stemming from the same ambiguity: which side owns which property? Without a committed answer, every fix risks landing inconsistent answers — encryption ends up local-only, compression stays remote-only, the next addition lives somewhere new, and the divergence accumulates as silent debt.

## Decision Drivers

* **Content-addressing forces Mode B for `hash_type-id` and `hash_buckets`.** A content-addressed store can't be content-addressed if two clients use different hashes — the same blob has different ids and the store fragments. There is no "Mode A" option for these keys; we just haven't named it.
* **Mode A is a correctness footgun for the *other* keys too.** Two clients with mismatched compression write blobs that decompress to different bytes. Two clients with mismatched encryption write blobs only one of them can read — but the remote claims they're a single store. There is no detection mechanism; the symptoms surface as unrelated downstream failures (fsck mismatches, decode errors, partial reads).
* **"I don't trust the remote operator with my key material" is a legitimate threat model — but it does not require Mode A.** The remote config can be **key-blind**: it names the encryption scheme and identifies keys by public-key fingerprint or KDF parameters; clients hold the actual key material locally. This is the restic / borg pattern. Mode B + key-blind preserves the threat model without splitting the property between two configs.
* **Configs are immutable per store identity.** A store's hash type, buckets, and encryption define what "this store" *is*. Changing them produces a different store; the right operation is to create a new one and migrate, not to mutate the existing config. This makes Mode B safe to cache aggressively — a stale read of an immutable file is still a correct read.
* **Test surface and code surface both shrink under a single mode.** Two latent modes mean two sets of behaviors to specify, test, and document, even when the second is "unsupported."

## Considered Options

1. **Status quo (mixed mode).** Continue accumulating bugs at the property-by-property level. Each new feature decides its own mode in isolation.
2. **Eliminate Mode A. Commit to Mode B for all blob-store-shaped properties; local SFTP config carries transport only.** Names the existing reality for hash/buckets, makes it the rule for compression/encryption, defines a key-blind layering for encryption.
3. **Make Mode A explicit via a flag (`-trust-remote-config=false`).** Doubles the testing matrix. Encourages footguns: anyone running Mode A on a multi-writer store eventually corrupts it. Adds a knob for an unsafe configuration.
4. **Drop SFTP support entirely.** Removes the question. Not actually on the table — SFTP is a stated near-term load-bearing feature.

## Decision Outcome

Chosen option: **Option 2 — eliminate remote-opaque mode; SFTP stores are always remote-driven.** This commits to one model, names the existing reality for hash/buckets, and pre-declares the design constraints for encryption and compression. The key-blind variant of Mode B preserves the no-trust-of-remote-operator threat model.

### What lives where

* **Local SFTP config** (`TomlSFTPV0`, `TomlSFTPViaSSHConfigV0`): transport ONLY.
  * Host, port, user, password, private-key-path, remote-path, known-hosts-file.
  * MUST NOT carry hash type, buckets, compression, or encryption settings.
* **Remote `blob_store-config`** (`TomlV3` at `<remote_path>/blob_store-config`): blob-store properties ONLY.
  * `hash_type-id`, `hash_buckets`, `compression-type`, encryption descriptors (see below).
  * Authoritative: clients read this on connect and conform; mismatch is an error, not a merge.

### Encryption layering (key-blind Mode B)

The remote config describes *what kind of encryption* the store uses, not the key bytes:

* **Public, in remote config:** algorithm identifier (e.g. `age-x25519-v1`), recipient public-key fingerprint(s), any non-secret KDF parameters.
* **Private, on each client:** the private key material itself, in a local key store (CWD-relative, XDG, OS keychain, etc. — out of scope here).

A new client connecting to an encrypted SFTP store reads the remote config, sees "this store is encrypted to public key X," looks up the matching private key locally, and proceeds. If the client doesn't hold a matching private key, reads fail; writes are still possible (encrypt-only with the public key).

Concretely, this means [#57](https://github.com/amarbel-llc/madder/issues/57) lands as: `TomlV3` (and its successor) grows an `encryption` field whose contents are public-side descriptors; `TomlSFTPV0` stays unchanged (no encryption field there). The `init-sftp-explicit -encryption …` flag operates on the *remote* config it writes (per [#58](https://github.com/amarbel-llc/madder/issues/58)).

### Caching

Because configs are immutable per store identity, clients MAY cache the remote `blob_store-config` locally to avoid round-tripping on every operation. Cache invalidation is bounded: an outdated cache reflects an earlier (still valid) version of an immutable artifact, and the remote config's id (or content hash) can serve as the cache key for a future explicit-refresh path. Caching is a follow-up, not part of this decision; the model just permits it.

A stronger formalization — making config files literally immutable on disk via restricted file modes, mirroring how published blobs are written read-only — is filed as a separate follow-up.

### `GetBlobStoreConfig()` semantics

The `BlobStore.GetBlobStoreConfig()` method, by name, returns the *blob store's* config. For local stores that already happens to be the local config object. For SFTP, it MUST return the **remote** config (the `TomlV3` decoded from `<remote_path>/blob_store-config`), NOT the local SFTP transport config. The local transport config is reachable via the `BlobStoreInitialized.Config` struct field, which retains its existing shape.

This semantic correction is what `info-repo` will use to surface remote-side keys ([#60](https://github.com/amarbel-llc/madder/issues/60)).

### `info-repo … config-immutable` wire shape

The `madder info-repo <store> config-immutable` pseudo-key MUST encode `BlobStore.GetBlobStoreConfig()` only. Operators wanting transport details for an SFTP store read individual keys (`host`, `port`, `user`, `private-key-path`, `remote-path`, `known-hosts-file`) — those continue to read from `BlobStoreInitialized.Config` (the local transport).

This mirrors the cleavage drawn above between blob-store properties (via the interface method) and transport configuration (via the struct field). Encoding both at this pseudo-key would deliberately re-merge what the rest of this ADR splits, and would promote the output from human-readable pretty-print to a wire-format contract requiring a separator definition. We do not need that contract today: nothing parses `config-immutable` programmatically — the surface is human and MCP, both consumers of the existing single-block hyphence form.

Implementation lands as part of [#60](https://github.com/amarbel-llc/madder/issues/60), since the `info_repo.go` encoder pivot and the SFTP `GetBlobStoreConfig()` semantic flip share the wire-shape concern (`BlobStoreInitialized.Config.Type` no longer matches the bare `GetBlobStoreConfig()` for SFTP once the flip ships). [#78](https://github.com/amarbel-llc/madder/issues/78) records this decision; #60 carries the code change.

### Negative consequences

* **`info-repo` on an SFTP store triggers lazy init for blob-store-shaped keys.** Reading `host` or `port` stays purely local; reading `compression-type` or `hash_type-id` opens the SSH/SFTP connection that lazy init has always required. Acceptable. Caching mitigates this if the cost ever bites in practice.
* **Multi-writer stores must agree on remote config or one writer wins.** This is already the rule for hash/buckets; this ADR makes it the rule for everything. The alternative (Mode A) is materially less safe.
* **Existing implicit Mode-A users (if any) break.** The honest answer: Mode A was never a guaranteed, named mode of operation; users relying on its accidental availability would have hit one of #57/#58/#59/#60 anyway.

## Implementation Pointers

The following changes follow from this decision. They are not part of this ADR's scope but are listed so the consequences are visible:

* [#60](https://github.com/amarbel-llc/madder/issues/60) — `info-repo` on SFTP. Pivot `(*remoteSftp).GetBlobStoreConfig()` to return the remote config (with `initializeOnce`); update `info_repo.go` to read keys from both `blobStore.Config` (transport) and `blobStore.GetBlobStoreConfig()` (blob-store properties).
* `*remoteSftp.makeEnvDirConfig` — wire the remote config's compression and (post-#57) encryption into the IO wrapper. Today's `env_dir.DefaultConfig` ignores the remote config in code while honoring it in the wire format; that contradiction goes away under this ADR.
* [#57](https://github.com/amarbel-llc/madder/issues/57) — SFTP encryption support. Lands as a `TomlV3` extension (or successor), key-blind: public material in the remote config, private material local-only.
* Local-cache-of-remote-config — separate follow-up, motivated by but not blocking this ADR.
* Config-file immutability via file modes — separate follow-up, mirroring how published blobs are written read-only.

## Related

* [#55](https://github.com/amarbel-llc/madder/issues/55) — `sftp.bats` test parity. Phase 1 landed; further phases will assert this decision's invariants where they touch SFTP.
* [#61](https://github.com/amarbel-llc/madder/issues/61) — SFTP feature parity with local stores (meta). #57/#58/#59/#60 are sub-issues; this ADR is the design bedrock #61's design call referenced.
