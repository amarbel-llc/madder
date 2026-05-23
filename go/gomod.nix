# goFlakeInputs bridge table — consumer side of FDR-0003 / FDR-0004
# (amarbel-llc/nixpkgs#32, amarbel-llc/nixpkgs#39).
#
# Each entry routes a `require` line in go.mod onto a flake input,
# bypassing the organic gomod2nix.toml hash. This file is the single
# place to add/remove a bridged dep — go/default.nix imports it and
# threads the result through every buildGoApplication and mkGoEnv.
#
# Keep all gomod2nix.toml consumers in sync: a buildGoApplication
# call that forgets `goFlakeInputs` sees the unmerged module graph
# and resurrects the lockstep (madder#208 / madder#211).
{
  tap,
  tommy,
  system,
}:
{
  # tap's go module lives under the repo's `go/` subdir; tommy's at
  # the repo root, so no subPath. Tommy publishes a dedicated
  # `go-pkgs` output (cleanSourceWith-filtered Go tree) per
  # FDR-0004.
  "github.com/amarbel-llc/tap/go" = {
    src = tap;
    subPath = "go";
  };
  "github.com/amarbel-llc/tommy" = {
    src = tommy.packages.${system}.go-pkgs;
  };
}
