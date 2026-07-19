# madder's conformist overlay, merged with conformist.lib.presets.eng in
# flake.nix (conformist.lib.evalModule). The preset enables the eng-convention
# linters (eng-versioning, flake-outputs/lock, the justfile-* roster); here we
# choose the formatters, the shellcheck linter, and the repo-specific tweaks.
#
# This replaces the former hand-written ./conformist.toml: the config is now
# Nix-generated so the eng linters' checker scripts (built by Nix) are reachable.
# `nix fmt` runs the generated wrapper; `just lint-fmt` runs `conformist check`
# against the same generated config. See conformist-nix(7).
{ pkgs, ... }:
{
  # Formatters — preserve madder's exact chain (goimports before gofumpt so the
  # import-grouped output is re-canonicalized by gofumpt).
  programs.goimports.enable = true; # package=gotools, -w, *.go (module defaults)
  programs.goimports.priority = 1;
  programs.gofumpt.enable = true; # -w, *.go (module defaults)
  programs.gofumpt.priority = 2;
  programs.nixfmt.enable = true;

  # go.mod lives at go/, not the tree root. Without this, goimports/gofumpt
  # run with cwd at the tree root, where Go tooling can't resolve the
  # module — confirmed in langlang (see langlang/conformist.nix) to SILENTLY
  # DELETE correctly-used imports as apparently-unused when the imported
  # package's declared name differs from its path's last segment, because
  # the resolver can't discover which identifier the import provides. That's
  # a silent build break, not a style nit. workingDir (conformist#38) scopes
  # the formatter's cwd to go/, matching a `cd go &&` invocation.
  programs.goimports.workingDir = "go";
  programs.gofumpt.workingDir = "go";

  # shfmt: a raw stanza rather than `programs.shfmt.enable`. The module cannot
  # emit `-ci` (no option for it) and its default includes lack `*.bats` — both
  # of which madder's project shell style requires. So spell the command out to
  # match the old conformist.toml exactly: 2-space indent, simplify, case-branch
  # indent; over *.sh / *.bash / *.bats.
  settings.formatter.shfmt = {
    command = "${pkgs.shfmt}/bin/shfmt";
    options = [
      "-w"
      "-i"
      "2"
      "-s"
      "-ci"
    ];
    includes = [
      "*.sh"
      "*.bash"
      "*.bats"
    ];
  };

  # shellcheck linter (read-only in `conformist check`). The module's default
  # includes lack *.bats, which madder lints, so override the include set.
  linters.shellcheck.enable = true;
  linters.shellcheck.includes = [
    "*.sh"
    "*.bash"
    "*.bats"
  ];

  # Go-specific eng linter (not in presets.eng). A no-op today (madder has no
  # .golangci.yml) but it installs the conformist#10 wiring guard for if/when
  # madder adopts golangci-lint with the dewey plugin.
  linters.golangci-dewey.enable = true;

  # eng-versioning(7) derives the version key from go.mod's module path; madder's
  # module is code.linenisgreat.com/madder/go, whose last segment yields the
  # wrong key (GO_VERSION). Pin it explicitly to match version.env.
  linters.eng-versioning.key = "MADDER_VERSION";

  # Excludes layered on conformist's default-excludes (which already cover
  # *.lock, go.mod, go.sum, LICENSE).
  #
  # NB: an exclude here drops the file before BOTH formatters and whole-tree
  # (passes-files=false) linters see it — so we must NOT exclude files an eng
  # linter keys on, even though no enabled formatter would touch them. In
  # particular version.env (eng-versioning), sweatfile (the eng-impure
  # sweatfile linter), and gomod2nix.toml/go.* (the gomod2nix linter) are
  # deliberately NOT excluded; no formatter matches them anyway. Only genuine
  # build/scratch/prose artifacts are excluded. *.md is excluded here (no md
  # formatter enabled); this does NOT affect the agents-md linter, which lives
  # in the separate IMPURE config (presets.eng-impure, run via just
  # lint-worktree) with its own excludes — so AGENTS.md/CLAUDE.md are still seen
  # there.
  settings.excludes = [
    "*.md"
    "result"
    "result-*"
    ".tmp/**"
  ];
}
