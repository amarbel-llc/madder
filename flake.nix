{
  inputs = {
    nixpkgs = {
      url = "github:amarbel-llc/nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.treefmt-nix.follows = "treefmt-nix";
    };

    nixpkgs-master.url = "github:NixOS/nixpkgs/d233902339c02a9c334e7e593de68855ad26c4cb";

    # `nix fmt` driver. Config lives in ./treefmt.nix.
    treefmt-nix = {
      url = "github:numtide/treefmt-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    utils = {
      url = "https://flakehub.com/f/numtide/flake-utils/0.1.102";
      inputs.systems.follows = "nixpkgs/systems";
    };

    tommy = {
      url = "github:amarbel-llc/tommy";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
      inputs.bats.follows = "bats";
      inputs.tap.follows = "tap";
    };

    bats = {
      url = "github:amarbel-llc/bats";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.treefmt-nix.follows = "treefmt-nix";
      inputs.utils.follows = "utils";
    };

    purse-first = {
      url = "github:amarbel-llc/purse-first";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.treefmt-nix.follows = "treefmt-nix";
      inputs.utils.follows = "utils";
    };

    # Sourced via goFlakeInputs (see madder#208) so a tap bump only
    # touches flake.lock — no go.mod / gomod2nix.toml lockstep edits.
    tap = {
      url = "github:amarbel-llc/tap";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
      inputs.bats.follows = "bats";
      inputs.treefmt-nix.follows = "treefmt-nix";
      inputs.purse-first.follows = "purse-first";
      inputs.crane.follows = "purse-first/crane";
      inputs.gomod2nix.follows = "purse-first/gomod2nix";
      inputs.rust-overlay.follows = "purse-first/rust-overlay";
    };

    # Provides `lint`; flake.lock dedup gate (madder#214).
    doppelgang = {
      url = "github:amarbel-llc/doppelgang";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
      inputs.treefmt-nix.follows = "treefmt-nix";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      nixpkgs-master,
      utils,
      tommy,
      bats,
      purse-first,
      tap,
      doppelgang,
      treefmt-nix,
      ...
    }:
    let
      # version.env at repo root is the single source of truth for
      # the release version. Burnt into the binary via the fork's
      # auto-injected -ldflags; consumed by bats too. `just bump-version`
      # sed-rewrites version.env. Match expression captures everything
      # after `MADDER_VERSION=` up to the line break.
      madderVersion = builtins.head (
        builtins.match ".*MADDER_VERSION=([^\n]+).*" (builtins.readFile ./version.env)
      );
      # shortRev for clean builds, dirtyShortRev for dirty working trees
      # (so devshell builds show `dirty-abcdef` rather than masquerading
      # as a clean release), "unknown" as a last-resort fallback.
      madderCommit = self.shortRev or self.dirtyShortRev or "unknown";
    in
    (utils.lib.eachDefaultSystem (
      system:
      let
        # Needed for the mkGoPkgs producer call in go/gomod.nix.
        # buildGoApplication / mkGoEnv consumers live in go/default.nix.
        pkgs = import nixpkgs { inherit system; };

        gomod = import ./go/gomod.nix {
          inherit
            pkgs
            system
            tap
            tommy
            ;
          # Scope the producer at go/ so downstream consumers reference
          # go-pkgs directly with no subPath. Madder's repo root has
          # no Go-relevant assets, so a full-repo filter would only
          # bloat the closure.
          src = self + "/go";
        };

        inherit (gomod.goPkgs) go-pkgs go-pkgs-test;

        # `nix fmt` entry point. Config lives in ./treefmt.nix.
        treefmtEval = treefmt-nix.lib.evalModule pkgs ./treefmt.nix;

        result = import ./go/default.nix {
          inherit
            nixpkgs
            nixpkgs-master
            tommy
            bats
            purse-first
            doppelgang
            system
            ;
          # Pivot self-consumption onto the published artifact: every
          # buildGoApplication in go/default.nix uses this as `src`,
          # so the same closure downstream consumers receive via
          # go-pkgs-test is what madder builds itself from. Contract
          # test for the producer-side split — if the filter ever
          # drops a file the build needs, this build breaks (#212).
          goPkgsTest = go-pkgs-test;
          inherit (gomod) goFlakeInputs;
          man7Src = ./docs/man.7;
          # Test-only inputs for the bats lanes' installCheckPhase.
          # Kept out of the build-time `src` closure so test-only
          # changes don't trigger a full Go rebuild. `version.env`
          # is the source of truth for the release version (read by
          # both flake.nix and version.bats).
          batsSrc = ./zz-tests_bats;
          versionEnv = ./version.env;
          version = madderVersion;
          commit = madderCommit;
        };
      in
      {
        packages = result.packages // {
          inherit go-pkgs go-pkgs-test;
        };
        devShells.default = result.devShells.default;
        formatter = treefmtEval.config.build.wrapper;
        # Sandboxed treefmt check for `just lint-fmt` and `nix flake
        # check`. Runs formatters over the source tree in a nix build
        # and exits non-zero on drift — no working-tree side effects,
        # unlike `nix fmt -- --ci`.
        checks.treefmt = treefmtEval.config.build.check self;
      }
    ));
}
