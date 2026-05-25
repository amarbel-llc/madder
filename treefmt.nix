# treefmt-nix configuration. Run via `nix fmt` or `just fmt`.
{ lib, ... }:
{
  projectRootFile = "flake.nix";

  # Go: goimports → gofumpt chain. Lower priority runs first; goimports must
  # run before gofumpt so the import-grouped output is then re-canonicalized
  # by gofumpt. Matches the prior `just fmt` ordering.
  programs.goimports.enable = true;
  settings.formatter.goimports.priority = 1;
  programs.gofumpt.enable = true;
  settings.formatter.gofumpt.priority = 2;

  programs.nixfmt.enable = true;

  programs.shfmt.enable = true;
  settings.formatter.shfmt.includes = [
    "*.sh"
    "*.bash"
    "*.bats"
  ];
  # treefmt-nix's shfmt module exposes `indent_size` and `simplify` but
  # not `--case-indent` (-ci). Override the full options list to keep
  # those flags AND add -ci so `case` branches stay indented one level
  # past the `case` keyword (project style; matches pre-treefmt code).
  settings.formatter.shfmt.options = lib.mkForce [
    "-i"
    "2"
    "-s"
    "-ci"
  ];

  settings.global.excludes = [
    "flake.lock"
    "go.sum"
    "gomod2nix.toml"
    "version.env"
    "sweatfile"
    "LICENSE"
    "*.md"
    "result"
    ".tmp/**"
  ];
}
