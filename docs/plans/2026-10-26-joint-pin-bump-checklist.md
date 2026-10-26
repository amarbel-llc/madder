# Joint piggy / madder / pigsty pin bump: the checklist

Written 2026-10-26 (madder/live-cedar) for circus/eager-holly's third
queue, item 4, so pigsty phase 4's bump can run in one pass on the
operator's word. Facts marked V were read from the repos on this date;
nothing here bumps anything. The reasoning behind the order is in
`2026-10-25-rust-sealed-key-store-design.md`, "Note: a joint madder/piggy
bump for pigsty phase 4".

## Why one pass

Three repos hold piggy, and two hold madder, and cargo unifies a git
dependency at exactly one rev per graph (V: pigsty's
`crates/pigsty-core/Cargo.toml` comment). pigsty's graph contains
piggy-markl twice: once directly and once through madder-store. Two revs
would be two `Id` types and a type error, so piggy moves in madder first
and in pigsty together with madder, never one without the other.

## The pins, today (V)

| repo | file | what it pins | value on 2026-10-26 |
|---|---|---|---|
| madder | `flake.lock` node `piggy` | the Go lanes' piggy (fibby, piggy-agent, `piggy/go`) | 9ebebd31a74e28f2dd6c4095a2b6ba5d284cf408 |
| madder | `go/go.mod` | `code.linenisgreat.com/piggy/go` pseudo-version | v0.1.24-0.20261025125401-9ebebd31a74e |
| madder | `crates/madder-store/Cargo.toml` + `Cargo.lock` | piggy-pigpen, piggy-markl `rev` | 9ebebd31a74e28f2dd6c4095a2b6ba5d284cf408 |
| madder | `version.env` | MADDER_VERSION, also the crate's `version` | 0.4.7 |
| pigsty | `crates/pigsty-core/Cargo.toml` | piggy-markl, piggy-pigpen, piggy-piv, fibby `rev` | 9ebebd31a74e |
| pigsty | `crates/pigsty-core/Cargo.toml` | madder-store `rev` | 5a685867a434f8f7762c4c735a07fb1983d92d61 |
| pigsty | `flake.nix` input `madder` | the Go madder for the interop test, same rev as madder-store | 5a685867a434f8f7762c4c735a07fb1983d92d61 |
| pigsty | `flake.nix` `outputHashes` | `madder-store-0.4.7`, `piggy-piv-0.1.0`, … per git source in Cargo.lock | sha256 per rev |

madder's three piggy pins are moved together by `just update-piggy <sha>`
(V: the recipe updates flake.lock, go.mod, the Cargo `rev` lines, runs
`cargo update -p piggy-pigpen -p piggy-markl`, then `build-gomod2nix`).
Its flake half is `nix flake update piggy`, which lands on piggy master's
HEAD, so the sha passed must be that HEAD; an older rev needs the recipe
to grow a pinned-rev form first.

## What pigsty needs from the bump (R: pigsty `2026-10-26-phase-4-prep.md`)

A piggy rev with piggy#337 complete (piggy-piv builds pure with
`default-features = false`) and piggy#341 merged (`CardEcdh` in
piggy-piv). piggy reported both on its origin/master at 2d96355d. From
madder, whatever master holds when the bump runs: madder-store's API
since 5a685867 gained `InitEncryption::{None, Key}` and plain-store
reading, `Encryption` as an enum, `OpenOptions.read_only` and the
`NoConfig` / `MissingSidecar` / `ReadOnly` error kinds, none of which
removes a call pigsty makes (V: pigsty's used surface per
piggy/live-sequoia's relay is open, init, unseal, public_key,
sealed_recipients, is_unsealed, write_bytes, read_to_vec, has, ids,
parse_recipients, `InitOptions::embedded`); adding a field to
`OpenOptions` breaks only a struct literal, and pigsty should build it
from `OpenOptions::default()`.

## The pass, in order

Each step waits for the previous one to land. One gate per repo at a time
(the fleet rule); check memory above 4 GB and disk above the floor before
each.

1. **piggy** (piggy/live-sequoia). Confirm master holds #337 and #341 and
   is green; cut a release so the target rev is a tag's commit (piggy's
   tags mark piggy-binary releases, not a pigpen API level, so the tag is
   a label for humans and the pin stays a sha). Hand the full 40-char sha
   to madder and pigsty. Gate: piggy's own `just` default.
2. **madder** (madder/live-cedar).
   - `just update-piggy <sha>`: flake.lock, go.mod, Cargo rev + lock,
     gomod2nix.toml move together. Inspect the diff: Cargo.toml's two
     `rev` lines and the comment above them, `piggy/go` pseudo-version,
     flake.lock's `piggy` node.
   - Commit, attest, `merge-this-session` (deploys paused: `targets: []`).
     The gate is `just` (default): the Go lanes plus `.#madder-store`
     (cargo test in the sandbox), `.#madder-store-wasm-check`,
     `.#bats-rust_cli`, `.#bats-rust_cli_identity`, `.#bats-rust_interop`
     (Rust and Go against one sealed store through the real piggy-agent)
     and `.#bats-piv_agent`. All of them touch the piggy rev; a piggy API
     change shows in `.#madder-store` first, a wire change in
     `.#bats-rust_interop`.
   - Hand pigsty the merged madder sha (origin/master after the merge),
     and, if `version.env` moved, the new MADDER_VERSION (it names the
     `outputHashes` key in pigsty's flake).
3. **pigsty** (pigsty/calm-redwood), one commit:
   - `crates/pigsty-core/Cargo.toml`: every piggy `rev` (piggy-markl,
     piggy-pigpen, piggy-piv, fibby; also under `[dev-dependencies]`) to
     the piggy sha; `madder-store` `rev` to the madder sha; `piggy-piv`
     with `default-features = false` as a plain dependency, the `card`
     feature dropped (phase-4 prep step 1).
   - `flake.nix`: input `madder` to the same madder sha;
     `outputHashes` for `madder-store-<version>` and each piggy crate
     refreshed (build once, read the expected hash from the failure, set
     it).
   - `cargo update -p madder-store -p piggy-markl -p piggy-pigpen -p
     piggy-piv -p fibby` so Cargo.lock holds one piggy rev; check with
     `cargo tree -i piggy-markl` that one version remains.
   - Gate: pigsty's `just` default (`test-rust`, `test-flutter`, the
     lint lanes); `tests/madder_interop.rs` must pass against the Go
     madder at the new rev.
4. **Record**: madder's design doc note and this file get the new revs;
   pigsty's phase-4 prep marks step 1 done. Tell circus/eager-holly.

## What each gate must show, as a line each

- piggy: green default gate at the sha; the tag exists and points at it.
- madder: `.#madder-store`, `.#madder-store-wasm-check`,
  `.#bats-rust_interop`, `.#bats-rust_cli`, `.#bats-rust_cli_identity`,
  `.#bats-piv_agent` and the Go lanes green at one piggy rev in three
  files; `cargo tree -i piggy-markl` in the workspace shows one version.
- pigsty: `cargo tree -i piggy-markl` shows one version; the interop test
  ran against a Go madder whose `madder version` prints the pinned
  madder's version; the Android APK builds (`just build-android-apk`)
  since piggy-piv is now in the cdylib's graph.

## What can go wrong

- **Two piggy revs in pigsty's graph**: a `rev` line missed (there are
  five in pigsty's Cargo.toml, V) or madder not yet bumped. Symptom: two
  `piggy_markl::Id` types, an E0308 at the madder-store boundary. Fix:
  the missed line, never a `[patch]`.
- **madder's flake half lands on a newer piggy than the Cargo half**:
  `nix flake update piggy` takes HEAD; if piggy moved after step 1, rerun
  `just update-piggy` with HEAD or wait for piggy to settle. The
  rust_interop lane catches a wire mismatch between the two halves.
- **outputHashes stale**: pigsty's nix build fails with the hash it
  wanted; paste it. A changed MADDER_VERSION renames the key.
- **An API change in piggy-pigpen** (`Document::{parse, seal, open,
  to_bytes, encryption_recipients}`, `recipient_id`, `EcdhOracle`,
  `X25519Identity`): `.#madder-store` fails first; madder adapts before
  pigsty bumps.
