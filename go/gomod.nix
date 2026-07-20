# Nix side of go.mod for madder. Carries both producer- and
# consumer-half of the flake-input-go_mod protocol (amarbel-llc/nixpkgs
# RFC 0001):
#
#   - producer: mkGoPkgs publishes go-pkgs / go-pkgs-test for downstream
#     consumers (e.g. dodder) and for madder's own self-consumption
#     (#212).
#
#   - consumer: goFlakeInputs routes specific go.mod `require` lines
#     onto flake inputs, bypassing the organic gomod2nix.toml hash
#     (#208 / #211 / #213).
#
# Mixed-flake shape per RFC 0001 § The `gomod.nix` convention. Single
# place to add/remove either side; flake.nix imports once and passes
# the relevant outputs into go/default.nix.
#
# Keep all gomod2nix.toml consumers in sync: a buildGoApplication
# call that forgets `goFlakeInputs` sees the unmerged module graph
# and resurrects the lockstep (#208 / #211).
{
  pkgs,
  src,
  tap,
  tommy,
  crap,
  hyphence,
  piggy,
  system,
}:
let
  # Bridging tap/tommy through their own `go-pkgs` outputs means
  # non-Go edits in those repos no longer trigger madder rebuilds
  # (#213). tap's go-pkgs is full-repo-filtered (its repo is
  # polyglot), so consumers still slice with `subPath = "go"`;
  # tommy's module is at its repo root.
  goFlakeInputs = {
    "github.com/amarbel-llc/tap/go" = {
      src = tap.packages.${system}.go-pkgs;
      subPath = "go";
    };
    "github.com/amarbel-llc/tommy" = {
      src = tommy.packages.${system}.go-pkgs;
    };
    # crap's go-pkgs is full-repo-filtered (its repo is polyglot:
    # go-crap/ alongside rust-crap/), so the go-crap module lives at
    # the go-crap/ subtree and consumers slice with subPath. The module
    # declared path gained a /v2 suffix at the v2.0.0 release; the source
    # subtree is still go-crap/.
    "github.com/amarbel-llc/crap/go-crap/v2" = {
      src = crap.packages.${system}.go-pkgs;
      subPath = "go-crap";
    };
    # hyphence's go-pkgs producer is scoped to its go/ subdir, so the module
    # root maps with no subPath (like tommy). See madder#253.
    "code.linenisgreat.com/hyphence/go" = {
      src = hyphence.packages.${system}.go-pkgs;
    };
    # piggy owns the markl-id framework (piggy#183 inversion). Its
    # producer is scoped to go/ (module root, no subPath); piggy's own
    # passthru bridges dewey for consumers that need it — madder's
    # dewey dep stays on its organic gomod2nix pin (dewey tags are
    # public), so no bridge entry is added here.
    "code.linenisgreat.com/piggy/go" = {
      src = piggy.packages.${system}.go-pkgs;
    };
  };
in
{
  # mkGoPkgs defaults fit madder's tree (no embedded assets outside
  # testdata/), so no `extras` / `testExtras` needed. goFlakeInputs is
  # passed so go-pkgs / go-pkgs-test carry `passthru.goFlakeInputs`,
  # letting downstream consumers that bridge madder inherit madder's own
  # bridges (go-crap/v2, tap, tommy) at depth-1 rather than re-declaring
  # them (RFC 0001 § Multi-producer closures; amarbel-llc/igloo#39
  # workspace-mode chaining). mkGoPkgs uses it only to attach the
  # passthru — the filtered source tree is unchanged.
  # Explicit name per RFC 0001 Appendix A (nixpkgs#49): madder's module
  # path ends in /go, so the inferred store-path prefix would be the
  # unhelpful "go-go-pkgs".
  goPkgs = pkgs.mkGoPkgs {
    inherit src goFlakeInputs;
    name = "madder";
  };
  inherit goFlakeInputs;
}
