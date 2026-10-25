# AGENTS.md

Orientation for coding agents working in `madder-store`, the Rust crate that
reads and writes madder sealed-key local blob stores (FDR 0011)
byte-compatibly with the Go `madder`, for the pigsty vault. `CLAUDE.md` is a
symlink to this file. Reference prose lives in `madder-store(7)`
(`docs/man.7/madder-store.md`) and the design doc
`docs/plans/2026-10-25-rust-sealed-key-store-design.md`; keep this file a map.

## Commands (from the repo root, inside the devShell)

- `just test-rust` — `nix build .#madder-store`: build plus `cargo test` in
  the sandbox, the authoritative lane.
- `cargo test --workspace` — the dev loop (the devShell carries the Rust
  toolchain; `age` on PATH enables the age CLI round-trip test).
- `just test-bats-rust-cli` — the EXISTING `pigpen_store.bats` tests tagged
  `rust_cli`, run against this crate's madder-grammar CLI as `MADDER_BIN`
  (Linux only).
- `just test-bats-rust-interop` — Rust and Go against one live store, both
  directions, via the real piggy-agent (Linux only).
- `just codemod-rust-fixtures` — regenerate the Go-written fixture stores
  under `tests/fixtures/` (changes their bytes, never their ids).
- `just lint-worktree` runs clippy; `nix fmt` runs rustfmt.

## Layout

- `src/store.rs` — `Store`: open, unseal, read, write, has, ids.
- `src/age_x25519.rs` — the age v1 X25519 subset (one module, swappable).
- `src/config.rs`, `src/sidecar.rs`, `src/hyphence.rs` — the two store files.
- `src/blob_id.rs` — digests, hex, bucket paths. `src/codec.rs` — none/zstd.
- `src/fs.rs` — the `Fs` trait (wasm seam) and `StdFs`.
- `src/init.rs` — creating a new sealed-key store.
- `src/bin/madder-store-cli.rs` — test-only CLI speaking madder's grammar
  (cargo feature `agent-cli`, which pulls piggy-pigpen's agent oracle).
- `tests/fixtures/<store>/` — a Go-written store, its plaintexts, the
  X25519 secret (the seed) and the expected ids.

## Rules

- Pure Rust only, no C, no tokio: the crate must build for Android and stay
  wasm32-portable. The RustCrypto versions follow piggy-pigpen's.
- Never guess an on-disk detail; the Go code and the fixtures are the
  contract. Anything outside the implemented subset is refused loudly.
- The piggy crates are git deps meant to sit at the `piggy` flake input's
  rev; `just update-piggy <sha>` moves flake.lock, go.mod and the Cargo
  pin together. When they are apart, Cargo.toml's comment says why.
