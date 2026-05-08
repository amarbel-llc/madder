# `madder sftp-analyze-and-suggest-configs` — design

**Status:** proposed 2026-05-08

**Date:** 2026-05-08

**Tracks:** new madder command (no parent issue at design time; an
implementation issue will be filed when writing-plans runs)

**Related:**
- ADR 0005 (remote-driven SFTP blob stores): [`docs/decisions/0005-remote-driven-sftp-blob-stores.md`](../decisions/0005-remote-driven-sftp-blob-stores.md)
- Existing layout discovery: `go/internal/foxtrot/blob_stores/discover.go` (used by `init -discover`)
- Existing remote-config writer: `WriteRemoteConfig` in the same file
- IO pipeline: `go/internal/foxtrot/blob_io/` and `domain_interfaces.BlobIOWrapper`
- TUI prompt library: `go/internal/futility/huh/`

## Problem

The user has legacy SFTP blob stores created before the
`blob_store-config`-on-remote requirement landed. They follow the old
single-hash directory layout (no `sha256/` or `blake2b256/` parent
dirs) and may or may not be encrypted. The user has no surviving
record of which compression or encryption was used.

`init -discover` exists and infers layout (multi-hash bool, bucket
depth) but **writes** a guessed config to the remote without
verifying the guess against actual blobs. Compression and encryption
are assumed to defaults. There is no read-only path to learn what a
legacy remote actually contains, and no path to verify a candidate
config before committing it.

## Goal

A new madder command, `sftp-analyze-and-suggest-configs`, that
read-only-probes a legacy SFTP remote, samples blobs, generates
candidate `blob_store-config` files that match the on-disk encoding,
verifies each candidate against the samples, optionally deep-verifies
the top candidate against the entire store, and (optionally,
interactively) bootstraps the chosen candidate to the remote.

## Non-goals

- Detecting or supporting hash types other than sha256. Per the
  user's stipulation, the legacy stores are sha256.
- Discovering or writing inventory-archive (pack-blobs) configs.
  v1 only emits hash-bucketed `DefaultType` candidates matching the
  legacy layout.
- Recovery of corrupted blobs. Deep-verify reports failures; it does
  not repair.
- Synthesizing or guessing encryption keys. Keys are user-supplied
  via `-key`; without keys, encrypted-store candidates are still
  enumerated but their verification is skipped at the decrypt stage.
- Mutating local state by default. Candidate files land in
  `$TMPDIR/madder-suggest-<runid>/`; the local store registry is not
  touched.
- A general-purpose "is my store healthy" tool. The existing
  `madder fsck` covers ongoing health checks; this command targets
  the bootstrap-an-unknown-legacy-remote use case.

## CLI surface

```
madder sftp-analyze-and-suggest-configs
    -ssh-host <alias>             ssh_config Host alias
    -remote-path <path>           remote root containing buckets
    -known-hosts-file <path>      optional; default $HOME/.ssh/known_hosts
    -key <path>                   age x25519 private key; repeatable
    -limit <N>                    samples to draw, default 10
    -max-sample-bytes <bytes>     skip blobs larger than this, default 1 MiB
    -emit-top <N>                 max candidate files to write, default 5
    -yes-to-all                   auto-confirm every huh prompt;
                                  combine with non-tty for scripted runs
```

Transport is ssh_config-only in v1. An explicit-credential variant
can be added later if needed; the user's two known legacy stores both
resolve via ssh_config.

## Architecture

### Package layout

- **`internal/<layer>/sftp_probe/`** — pure-function probe library.
  No SFTP dependency. (Layer placement deferred to dagnabit.)
  - `magic.go`-equivalent is unnecessary; detection is by *attempted
    decode through the IO pipeline*, not byte-prefix sniffing. The
    library lives in three files:
  - `candidates.go` — `EnumerateCandidates(layout, keys) []Candidate`.
  - `verify.go` — `VerifySample(reader, expectedDigestHex, candidate)
    SampleResult` and the `Stage` enum.
  - `aggregate.go` — `Aggregate{Candidate, Verified, Total, Stages}`
    and the ranking function.

- **`internal/india/commands/sftp_analyze_and_suggest_configs.go`** —
  CLI wrapper. Owns flags, SSH dial via the existing
  `makeSSHClientForSFTPViaSSHConfig` helper, the SFTP walk + sample
  routine, the huh prompts, and the `WriteRemoteConfig` invocation
  that performs the bootstrap. No probe logic of its own.

- **`zz-tests_bats/sftp_analyze_and_suggest_configs.bats`** — bats
  integration suite, `file_tags=net_cap`. Mirrors `sftp.bats` setup
  (`start_sftp_server`, `init_sftp_test_store`, `stop_sftp_server`).

- **`go/cmd/madder-test-craft-legacy-blob/`** — small `_test`-tagged
  helper binary that takes `-compression -encryption -content` and
  writes a properly-encoded blob to a path. Used by bats fixtures to
  build legacy-shaped remotes deterministically; never shipped.

### Probe library API

```go
package sftp_probe

type Candidate struct {
    StoreConfig blob_store_configs.Config  // hyphence-encodable for emission
    IOConfig    blob_io.Config             // for verification
    Label       string                     // "zstd/none", "zstd/age-key1", ...
}

func EnumerateCandidates(
    layout blob_stores.DiscoveredConfig,
    keys   []markl.Id,
) []Candidate

type Stage int
const (
    StageOK Stage = iota
    StageDecrypt
    StageDecompress
    StageHashMismatch
)

type SampleResult struct {
    Ok    bool
    Stage Stage
    Err   error
}

func VerifySample(
    blobReader        io.Reader,
    expectedDigestHex string,
    candidate         Candidate,
) SampleResult

type Aggregate struct {
    Candidate Candidate
    Verified  int
    Total     int
    Stages    map[Stage]int
}

func Rank(aggregates []Aggregate) []Aggregate
```

### Detection model — verify, don't sniff

We do not classify blob bytes by magic-byte inspection. Instead, we
*attempt every plausible decode* through the existing `blob_io.Config`
reader pipeline and let success-or-failure speak.

Candidate enumeration is purely combinatorial:

```
hash:        {sha256}                              -- fixed
compression: {none, gzip, zlib, zstd}              -- 4
encryption:  {none}  ∪  { [keyᵢ] for each -key }   -- 1 + N
```

So with 0 keys → 4 candidates; with N keys → 4·(1+N) candidates.

`VerifySample` builds a reader chain from `candidate.IOConfig`, copies
its output into `sha256.New()`, and compares the digest hex to the
filename. Each layer's failure mode classifies cleanly:

- **age decrypt error** → either no encryption (we picked an
  `age+key` candidate against a plaintext store) **or** wrong key.
  Distinguished at aggregate time: if the matching plaintext
  candidate at the same compression-level verifies on the same
  sample, the key candidate is the one that's wrong. If the
  plaintext candidate also fails at decompress/hash, the store is
  encrypted and the key is wrong (or absent).
- **decompression error** (`gzip: invalid header`, `zstd: magic
  mismatch`, etc.) → wrong codec for this sample.
- **hash mismatch with no decode error** → all decodes accepted but
  recovered content didn't hash to the filename → wrong combination
  of layers (rare in practice).
- **OK** → that candidate verifies for that sample.

`recover()` inside `VerifySample` converts panics from any decode
layer into `Stage=Decrypt` or `Stage=Decompress` results with a
`panic_recovered` flag in the diagnostic. Bats covers a
malicious-bytes case to confirm.

### Sampling — random hex-prefix scatter, bounded

```
1. ReadDir remoteRoot → list populated top-level entries.
2. Drop blob_store-config and tmp_* names.
3. For each i in 0..limit:
     a. Pick a random populated top-level bucket.
     b. For each remaining bucket-depth level: ReadDir, pick a random
        subdir entry; if the level is empty, restart i.
     c. ReadDir leaf, filter to file entries, pick one at random.
     d. Reconstruct expected digest hex via
        markl.SetHexStringFromRelPath on the relative path.
     e. Open + read into a bounded buffer (cap at -max-sample-bytes,
        default 1 MiB; skip and re-pick on overflow with one retry).
4. Stop after `limit` successes or `2*limit` total attempts (refuse
   noisily if we couldn't gather enough).
```

The reconstructed digest is the ground truth — we don't trust any
candidate's claim about it.

### Wrapper main loop

```
samples    := scatterSample(sftpClient, remoteRoot, layout, limit)
candidates := sftp_probe.EnumerateCandidates(layout, keys)

if existingConfig := readRemoteConfigOptional(sftpClient, remoteRoot);
   existingConfig != nil {
    candidates = prepend(candidates, candidateFromExisting(existingConfig))
}

for each sample in samples:
    for each candidate in candidates:
        result := sftp_probe.VerifySample(
            bytes.NewReader(sample.Buf),
            sample.DigestHex,
            candidate,
        )
        aggregate[candidate].add(result)

ranked := sftp_probe.Rank(aggregate)
emit(ranked)         -- TAP plan + candidate files (gated, see below)
runInteractiveFlow(ranked, samples, sftpClient)
```

### IO budget

- `-limit` (default 10): blobs sampled. Bounds read-side IO.
- `-max-sample-bytes` (default 1 MiB): skip oversized blobs.
- `-emit-top` (default 5): how many candidates to write to disk.
- **Read-each-sample-once optimization:** each sample is buffered in
  memory (≤ `-max-sample-bytes`) and fed to every candidate via
  `bytes.NewReader`. With limit=10 and 24 candidates: 10 SFTP opens,
  not 240.

### Read-only invariant

The probing phase calls only `sftpClient.ReadDir`, `Stat`, `Open`,
and `Read`. It never calls `Create`, `Mkdir`, `Rename`, `Chmod`, or
`Remove`. Bats #7 asserts strict mtime+size equality of the entire
remote tree before/after the probing phase.

The bootstrap phase, when reached and consented, mutates the remote.
That phase is gated behind huh #3 (or `-yes-to-all`) — never runs
without explicit consent.

### Existing-config validation

If `<remote-root>/blob_store-config` exists, it's decoded and
prepended to the candidate list as a `Candidate` labeled `existing`.
It always sorts to position #1 in the output regardless of `verified`
count, so the user sees "what's on the remote right now" first.

- **Existing verifies (10/10):** TAP `ok 1 - existing verified=10/10`.
  Note: existing config is consistent with sampled blobs. No
  candidate file emitted for `existing`. Synthesized candidates are
  emitted as files only with `-yes-to-all` or when the user opts in
  via huh.
- **Existing fails (≠ 10/10):** TAP `not ok 1 - existing FAILED`.
  Triggers huh #1.
- **Existing decodable but references unknown fields** (unsupported
  hash type / encryption format): treat as fatal; do not enumerate
  synthesized candidates (we'd be guessing). Exit 1.
- **Existing read but doesn't decode** (truncated/corrupted): treat
  as "no existing config"; proceed with synthesized candidates.
  Banner notes the remote has unparseable bytes at that path.

### Output: candidate files + TAP summary

Per emitted candidate, a file under
`$TMPDIR/madder-suggest-<runid>/candidate-<rank>-<comp>-<enc>.hyphence`,
encoded the same way `WriteRemoteConfig` does it.

TAP body, one block per emitted candidate:

```
ok 1 - candidate-01-zstd-none verified=10/10
  ---
  path: /tmp/madder-suggest-<runid>/candidate-01-zstd-none.hyphence
  compression: zstd
  encryption: none
  hash: sha256
  layout: { multi_hash: false, buckets: [2] }
  verified: 10
  total: 10
  stages: { ok: 10 }
  bootstrap:
    - ssh '<alias>' test ! -e '<remote-path>/blob_store-config' \
        || { echo 'remote blob_store-config already exists; refusing'; exit 1; }
    - scp '<local-path>' '<alias>:<remote-path>/blob_store-config'
    - ssh '<alias>' chmod 0444 '<remote-path>/blob_store-config'
  ...
```

For overwrite (existing-fails path), the bootstrap block is the
chmod-0644 → scp → chmod-0444 sequence.

The bootstrap block stays inline (single ordered list) so users can
copy the three lines and paste them as a script. No JSON output in
v1 (YAGNI).

### Interactive end-of-run flow via `huh`

Three sequential prompts after sample-based ranking finishes:

```
samples gathered, candidates ranked
            │
            ▼
   any candidate verified == total?
   ├── no  → print TAP failure summary, exit 1
   └── yes → identify top candidate
            ▼
       huh #2: "Deep-verify <label> against the full store?"
       ├── no  → skip walk, jump to huh #3
       └── yes → run full-store walk; emit deep-verify TAP block
                 ├── K == M → fall through to huh #3
                 └── K < M  → fire huh #2.5 (bootstrap-anyway?)
                              ├── no  → exit 1
                              └── yes → fall through to huh #3
            ▼
       huh #3: "Bootstrap <label> to <alias>:<remote-path>?"
       ├── no  → exit 0 (or exit 2 if K < M was consented above)
       └── yes → call blob_stores.WriteRemoteConfig with the
                 candidate's StoreConfig. Existing-fails path runs
                 chmod-0644, write, chmod-0444.
                 ├── ok        → exit 0 (or exit 2 if K < M consented)
                 └── ssh error → exit 1
```

`huh #1` from the existing-fails path fires earlier (before huh #2)
and gates whether candidate files are emitted at all.

**Polarity & non-tty behavior:**

| Mode | Default polarity for prompts |
|------|------------------------------|
| tty without flag | each prompt asks the user |
| non-tty without flag | every prompt answers "no". Read-only: emit TAP, candidate files (when applicable), bootstrap text. No deep-verify, no remote write |
| `-yes-to-all` (any tty state) | every prompt answers "yes". No huh UI. Suitable for cron / CI / scripted runs |

`-yes-to-all` answers user prompts, not safety gates — the
`existing-references-unknown-fields` halt and the TAP-failure exit
both still trigger.

### Bootstrap execution

When huh #3 (or `-yes-to-all`) consents to bootstrap, the command
calls `blob_stores.WriteRemoteConfig` directly with the candidate's
`StoreConfig` and the already-open SFTP client. No shell-out to scp,
no re-dial, no shell-quoting concerns. The equivalent shell commands
are still printed to stderr for audibility ("running: scp ...",
"running: chmod ...") — but execution is in-process.

### Exit codes

- `0` — clean success: probe completed, optionally bootstrapped
  without deep-verify failures.
- `1` — real failure: connect failed, no candidate verified, existing
  config references unsupported fields, deep-verify-failures gate
  declined, bootstrap step itself errored.
- `2` — bootstrap completed, but with known deep-verify failures the
  user explicitly consented to. The store is mostly-functional under
  the new config; some blobs will remain inaccessible until
  investigated.

Documented in the man page and in the `--help` text.

## Error handling & edge cases

**Flag-parse-time (fail fast, no SFTP):**
- `-ssh-host` empty or alias not in ssh_config → bad-request.
- `-remote-path` empty → bad-request.
- `-key <path>` missing/unreadable/not-an-age-key → bad-request,
  name the failing key file.
- `-limit < 1`, `-emit-top < 1`, `-max-sample-bytes < 1024` →
  bad-request.

**Connect-time:** SSH dial / SFTP subsystem failures → wrap and exit
1; no candidate files written.

**Walk-time:**
- `-remote-path` missing on remote → wrap with the path, exit 1.
- `-remote-path` exists but is a file → explicit error, exit 1.
- Root has zero bucket-shaped entries → "not a blob store?", exit 1.
- Walk gathers fewer than `limit` samples after `2*limit` attempts
  → continue with what we have, TAP diagnostic warns about sparsity.
- Walk gathers zero samples → exit 1.

**Sample-time:** oversized / truncated / open-failed samples are
dropped and retried within the budget.

**Verify-time:** decrypt/decompress/hash-mismatch failures are
verdict stages, not errors. They populate the candidate's stage
histogram. Panics in any decode layer are recovered and reported as
a stage.

**Output-time:** `$TMPDIR` not writable → exit 1 before writing any
candidate file. Per-candidate write failures are reported and the
others continue.

**Concurrent runs:** each invocation gets a fresh
`madder-suggest-<runid>/` under `$TMPDIR`. No collisions.

**Ctrl-C / context cancel:** all SFTP IO is gated by the existing
`interfaces.ActiveContext` pattern. Cancel mid-run leaves any
already-written candidate files in place; no remote mutation if the
cancel arrives before huh #3 returns yes.

## Testing strategy

### Unit tests in `sftp_probe/`

Pure-function, no SFTP. Strategy: synthesize forward via the encode
pipeline, verify backward via `VerifySample`. Table-driven cases:

| # | Forward construction | Candidate under test | Expected verdict |
|---|---|---|---|
| 1 | none + none + sha256 | none/none | OK |
| 2 | zstd + none + sha256 | zstd/none | OK |
| 3 | gzip + none + sha256 | gzip/none | OK |
| 4 | zlib + none + sha256 | zlib/none | OK |
| 5 | none + age(K1) + sha256 | none/age-K1 | OK |
| 6 | zstd + age(K1) + sha256 | zstd/age-K1 | OK |
| 7 | zstd + none + sha256 | gzip/none | not-OK, stage=Decompress |
| 8 | zstd + none + sha256 | none/none | not-OK, stage=HashMismatch |
| 9 | none + age(K1) + sha256 | none/age-K2 | not-OK, stage=Decrypt |
| 10 | zstd + age(K1) + sha256 | zstd/age-K2 | not-OK, stage=Decrypt |
| 11 | none + none + sha256 | none/age-K1 | not-OK, stage=Decrypt |
| 12 | zstd + age(K1) + sha256 | none/none | not-OK, stage=Decrypt-or-Decompress |

Plus enumeration tests (`EnumerateCandidates` returns exactly
`4·(1+|keys|)` candidates, no duplicates, stable ordering) and
aggregate/ranking tests (ties broken by stage diversity, no NaN /
panic on empty slices).

**TDD lane:** start with case 1 (red until `VerifySample` exists),
add cases by row.

### Bats integration in `zz-tests_bats/sftp_analyze_and_suggest_configs.bats`

`file_tags=net_cap`. Hand-crafts legacy-shaped remotes via
`madder-test-craft-legacy-blob`. Stdin-redirected huh interactions
via `<<<` heredocs.

1. **Verifies a freshly-init'd store.** Init via
   `init-sftp-explicit`, write blobs, run analyze → `existing` block
   verifies 10/10, no candidate files written.
2. **Detects unencrypted-zstd legacy layout.** Hand-craft remote
   with no `blob_store-config` and zstd-compressed blobs → `zstd/none`
   candidate verifies, file written.
3. **Detects unencrypted-uncompressed legacy.** Same as #2, raw
   blobs → `none/none` verifies.
4. **age-encrypted store, key provided** → `zstd/age-key1` verifies.
5. **age-encrypted store, no key.** Same tree as #4, no `-key` →
   all candidates fail; TAP diagnostic indicates `decrypt` stage
   dominant; exit 1.
6. **Wrong key.** Tree from #4 with `-key K2.txt` → `decrypt`-stage
   failures; clear diagnostic; exit 1.
7. **Read-only invariant.** Strict mtime+size snapshot before/after
   probing phase; assert exact equality. (Test exits before huh #3
   could fire; uses `-non-interactive` semantics i.e. no
   `-yes-to-all`.)
8. **Bootstrap end-to-end.** Verifying tree + `-yes-to-all` →
   `WriteRemoteConfig` runs in-process → assert
   `<remote>/blob_store-config` exists with mode 0444 and is
   byte-identical to the candidate file → register a local sftp
   store pointing at the same remote → `madder info-repo` reports
   the right `compression-type`.
9. **`-limit` is honored.** 100-blob tree, `-limit 3` → exactly 3
   SFTP `Open` calls (instrumentable via `madder-test-sftp-server`
   logs).
10. **`-emit-top` caps file emission.** Run with `-emit-top 2`
    against a tree where many candidates would otherwise emit →
    exactly 2 candidate files on disk.
11. **Bad inputs.** Bogus `-ssh-host`, missing `-key`, missing
    `-remote-path` → fast bad-request errors.
12. **Empty store.** Remote root has buckets but they're all empty
    → "no blobs found", exit 1.
13. **Existing config verifies; alternatives requested.** Init
    working store; run with `-yes-to-all` → `existing` verifies AND
    alternative candidate files exist.
14. **Existing config wrong; non-tty.** Init working store, hand-edit
    remote `blob_store-config` to claim wrong compression; run via
    bats (no tty, no flag) → `existing` fails 0/10; no candidate
    files written; exit 1.
15. **Existing config wrong; `-yes-to-all`.** Same setup with
    `-yes-to-all` → correct candidate file written with overwrite
    bootstrap; in-process `WriteRemoteConfig` runs the chmod-0644 →
    write → chmod-0444 sequence; `info-repo` confirms the new config.
16. **Existing config wrong; tty-simulated.** Same setup with stdin
    `<<< $'y\ny\ny\n'` → identical outcome to #15. Stdin `<<<
    $'n\n'` → exit 1, no files written.
17. **Existing config corrupt.** Remote `blob_store-config` exists
    but is truncated/invalid → treated as "no existing config";
    synthesized candidates emitted with non-overwrite bootstrap.
18. **Existing config references unknown fields.** Hand-craft a
    config that claims an unsupported encryption format → halt with
    explicit error; no synthesized candidates emitted; exit 1.
19. **Deep-verify clean.** Verifying tree, stdin
    `<<< $'y\ny\ny\n'` → deep-verify reports 100% verified, bootstrap
    runs, exit 0.
20. **Deep-verify partial; user accepts.** Verifying tree with one
    corrupted blob (manually flip a byte), stdin `<<< $'y\ny\ny\n'`
    → deep-verify reports failures, huh #2.5 yes, bootstrap runs,
    exit 2.
21. **Deep-verify partial; user declines.** Same setup, stdin
    `<<< $'y\ny\nn\n'` → deep-verify reports failures, huh #2.5 no,
    no bootstrap, exit 1.
22. **`-yes-to-all` against partially-corrupted store.** Same setup
    with `-yes-to-all` → bootstrap runs (huh #2.5 auto-yes), exit 2.
23. **huh requires a tty regression.** Run via bats (no tty) without
    `-yes-to-all` → command falls back to non-tty mode automatically,
    no panics, no hangs.

### `madder-test-craft-legacy-blob` helper

Tiny Go binary, `_test`-tagged, lives at
`go/cmd/madder-test-craft-legacy-blob/`. Flags:

```
-compression {none|gzip|zlib|zstd}    default none
-encryption  {none|age}               default none
-recipient   <age-pubkey>             required if -encryption=age
-content     <path|->                 source bytes (default stdin)
-out         <path>                   destination
```

Computes sha256 of the cleartext, applies compression (if any), then
encryption (if any), writes the result to `<out>`. The bats fixture
moves the file into the bucket-correct path. Used by tests #2-#6,
#15-#22.

## Rollback strategy

This is a greenfield command and a greenfield package. Rollback is a
single-commit revert: delete the new files (`sftp_probe/`,
`sftp_analyze_and_suggest_configs.go`, the bats file, the
test-craft helper), and the codebase is unchanged from before this
work. No dual-architecture period applies — there is nothing being
replaced.

The only mutation the command performs (when consented) is calling
the existing `blob_stores.WriteRemoteConfig` against a remote. That
function is itself revert-safe: the file it writes is on a remote
the user controls, can be removed via `ssh <host> rm
<remote>/blob_store-config`, and was only created with explicit
consent.

## Out-of-scope follow-ups (post-v1)

- Explicit-credential transport variant (`-host`, `-port`, `-user`,
  `-password`, `-private-key-path`).
- `-format json` for machine-readable output.
- Inventory-archive layout detection (pack-blobs stores).
- blake2b256 / future hash-type detection.
- Auto-discovery of additional candidate keys (e.g. probing
  `~/.config/madder/keys/`).
- A `-bootstrap-allow-deep-verify-failures` flag to make the
  consent path inferable from CLI alone (today the consent is via
  huh #2.5).
