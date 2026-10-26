# Pickup note: madder/live-cedar (written 2026-10-26, fleet focus change)

The session madder/live-cedar (the madder-store Rust crate, FDR 0011
sealed-key stores, and lately madder#296) was asked to wrap for the
fleet's pivot to unattended operation. This is the state a stranger needs
to continue. Everything below was read from the repo on 2026-10-26.

## Branch state

- `live-cedar` is one commit ahead of origin/master: **98c8560**
  "fix(remote): refuse a secret -encryption key on remote init; warn when
  a remote config holds one" (closes madder#296). master is at a9d1c70.
- The commit is attested (`nothing-but-the-truth` at 98c8560bbf8d). Its
  merge gate was started twice: the first (3f66974) failed in the race
  lane on the commit's own unit test, which caught a real misclassification
  (fixed and amended into 98c8560); the second (merge-74a56dc3) was
  cancelled four minutes in, on the focus-change order, not on a failure.
- Nothing is uncommitted. The worktree is clean.

## What 98c8560 does, and what was verified

- `init-sftp-explicit`, `init-sftp-ssh_config`, `init-webdav` refuse a
  secret-bearing `-encryption` (`generate`, or any secret-key format)
  before anything is created, naming the key as purpose@format only and
  pointing at `-pigpen`. `init-s3` has no `-encryption` flag; untouched.
- A remote config that already holds a secret keeps working and warns on
  stderr on the first blob read or write per process (same shape as the
  FDR 0011 drift warning). No on-disk format change.
- `blob_store_configs.IsSecretKeyId` / `FirstSecretKey`, keyed on piggy's
  secret format ids (`age_x25519_sec`, `ed25519_sec`, `nonce`, `_sec`
  suffix). NOT on `markl.FormatSec`: piggy registers `age_x25519_pub` as a
  FormatSec too (encrypt-only wrapper), which is what the first gate caught.
- Verified: `just test-bats-net-cap` passed 75/75 with the three new bats
  (sftp refusal, webdav refusal, legacy remote config with a secret still
  round-trips and warns) on the 3f66974 content; the Go code compiled
  under nix there. The unit tests in
  `go/internal/delta/blob_store_configs/encryption_secret_test.go` were
  fixed after the race-lane failure and have NOT been re-run since; the
  gate runs them.
- Design note and approval: sent to and approved by circus/eager-holly on
  2026-10-26 (refuse, warn, point at -pigpen; no format change).

## Exact next steps

1. Check memory (over 6 GB available) and disk, and that no other madder
   gate runs. Then, from the live-cedar worktree:
   `merge-this-session-async` with `targets: []` (deploys paused). If the
   attestation has lapsed, re-run `nothing-but-the-truth` for the five
   listed skills first.
2. If the gate fails in `test-go-race` or `test-go-godyn`, read
   `nix log <drv>` for `FAIL` lines; the only untested change since the
   last green lane is `encryption_secret.go`'s predicate and its tests.
3. Report the merged sha to circus/eager-holly; nothing is owed to pigsty
   or piggy for #296.

## Dev-loop facts a stranger will hit

- Bare `go build ./...` in the devshell fails on the tommy codegen
  (`document.ParseStrict` undefined): go.mod pins tommy v0.5.0 while the
  nix build uses the flake input's tommy. Known under madder#303/#304. Use
  the nix lanes (`just test-bats-net-cap`, `just test-go-godyn`, the gate).
- `just debug-rust-dev-loop` is the Rust dev loop (fmt, clippy, cargo
  test); `just run-bench-rust` the benchmark. Both work.

## Parked, by whose word

- Recipient management in the crate (lift key-status into a typed library
  result; reseal through an injected oracle): parked by piggy and pigsty
  until pigsty's phase 4, or Sasha's ask via eager-holly or
  piggy/live-sequoia. Facts in the design doc's "Third queue" and in
  `madder#324`.
- madder#324 (`Store` is `!Send` through `Box<dyn Fs>`; an `Fs` over
  Android content URIs has no `link(2)`): phase 4.
- The joint piggy/madder/pigsty pin bump: operator's word only;
  `docs/plans/2026-10-26-joint-pin-bump-checklist.md` is the runbook.
- pigsty keeps its own vanished-base write guard until it pins madder at
  or after a9d1c70 (their bump list).
- FDR 0011's last promotion criterion (a real remote store on the sealed
  key design with no secret on the remote): the operator's to confirm.

## Where the longer record is

- `docs/plans/2026-10-25-rust-sealed-key-store-design.md`: the crate's
  design, every queue's record, decisions and their dates.
- `docs/man.7/madder-store.md`: API, THREAT MODEL, COMPATIBILITY,
  PERFORMANCE.
- `crates/madder-store/AGENTS.md`: the map.
