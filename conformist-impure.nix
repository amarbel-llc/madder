# madder's IMPURE conformist overlay, merged with conformist.lib.presets.eng-impure
# (and the dewey-facade-export module) in flake.nix's conformistImpureEval. The
# impure lane runs `conformist check` against the live working tree (`just
# lint-worktree`), so it can host checks that need a real toolchain -- here,
# clippy over the Rust workspace (crates/madder-store). This is the eng-rust
# template's clippy block (conformist-conform(7) FLAKE TEMPLATES); madder keeps
# its gomod2nix linter on, unlike the template, since it has a Go module.
{
  pkgs,
  lib,
  ...
}:
let
  # Bake the Rust toolchain into the check via runtimeInputs (hermetic: no
  # runtime `nix shell`, so the check never depends on the flake registry,
  # network, or nix being reachable from inside `conformist check`). gcc
  # supplies the linker cargo invokes.
  rustToolchain = with pkgs; [
    cargo
    clippy
    rustc
    gcc
  ];

  check = pkgs.writeShellApplication {
    name = "conformist-clippy";
    runtimeInputs = rustToolchain;
    text = ''
      # Whole-tree check (passes-files=false): cwd is the tree root, no file
      # args. The workspace root is the repo-root Cargo.toml; --workspace covers
      # every member crate.
      [ -f Cargo.toml ] || exit 0
      cargo clippy --manifest-path Cargo.toml --workspace --all-targets --all-features -- -D warnings
    '';
  };

  repair = pkgs.writeShellApplication {
    name = "conformist-clippy-repair";
    runtimeInputs = rustToolchain ++ [ pkgs.git ];
    text = ''
      [ -f Cargo.toml ] || exit 0
      # Partial autofix: clippy --fix applies only the machine-applicable
      # suggestions. Findings with no mechanical rewrite are left for a human
      # and still fail the check. Self-stage the fixes.
      cargo clippy --fix --allow-dirty --allow-staged --all-targets --all-features --workspace \
        --manifest-path Cargo.toml
      # Stage only what clippy can have rewritten (sources and the lockfile),
      # never unrelated tracked edits the author has in flight.
      git add -u -- '*.rs' Cargo.lock
    '';
  };
in
{
  # clippy as a custom impure linter. It compiles the crate, so it can only run
  # in the working-tree lane, never the sandboxed checks.formatting.
  settings.linter.clippy = {
    command = lib.getExe check;
    "repair-command" = lib.getExe repair;
    # Whole-tree check: the includes are a fire-trigger, not the input set (the
    # check reads the workspace via --manifest-path Cargo.toml).
    includes = [
      "**/*.rs"
      "**/Cargo.toml"
      "Cargo.toml"
    ];
    passes-files = false;
  };
}
