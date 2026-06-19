{
  inputs = {
    igloo = {
      url = "github:amarbel-llc/igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.treefmt-nix.follows = "bats/treefmt-nix";
    };

    nixpkgs-master.url = "github:NixOS/nixpkgs/d233902339c02a9c334e7e593de68855ad26c4cb";

    utils = {
      url = "https://flakehub.com/f/numtide/flake-utils/0.1.102";
      inputs.systems.follows = "igloo/systems";
    };

    tommy = {
      url = "github:amarbel-llc/tommy";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
      inputs.bats.follows = "bats";
      inputs.tap.follows = "tap";
    };

    bats = {
      url = "github:amarbel-llc/bats";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.igloo.follows = "igloo";
      inputs.utils.follows = "utils";
    };

    purse-first = {
      url = "github:amarbel-llc/purse-first";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.igloo.follows = "igloo";
      inputs.utils.follows = "utils";
      # purse-first gained a `conformist` input (its dagnabit facade-format lane
      # points `dagnabit export` at conformist's generated config — purse-first#159).
      # Collapse it onto madder's top-level conformist so the lock has ONE
      # conformist node (doppelgang lint dedup). No cycle: conformist no longer
      # consumes purse-first (it builds golangci-lint-dewey from a pinned FOD).
      inputs.conformist.follows = "conformist";
    };

    # conformist: the linter + formatter multiplexer (treefmt successor).
    # Config is Nix-generated from ./conformist.nix (+ presets.eng) via
    # conformist.lib.evalModule.
    conformist = {
      url = "github:amarbel-llc/conformist";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
      inputs.purse-first.follows = "purse-first";
    };

    # Sourced via goFlakeInputs (see madder#208) so a tap bump only
    # touches flake.lock — no go.mod / gomod2nix.toml lockstep edits.
    tap = {
      url = "github:amarbel-llc/tap";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
      inputs.bats.follows = "bats";
      inputs.treefmt-nix.follows = "bats/treefmt-nix";
      inputs.purse-first.follows = "purse-first";
      inputs.gomod2nix.follows = "purse-first/gomod2nix";
    };

    # Sourced via goFlakeInputs (see madder#208) so a crap bump only
    # touches flake.lock — no go.mod / gomod2nix.toml lockstep edits.
    crap = {
      url = "github:amarbel-llc/crap";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
      inputs.bats.follows = "bats";
      inputs.conformist.follows = "conformist";
    };

    # Provides `lint`; flake.lock dedup gate (madder#214).
    doppelgang = {
      url = "github:amarbel-llc/doppelgang";
      inputs.igloo.follows = "igloo";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
      inputs.treefmt-nix.follows = "bats/treefmt-nix";
    };
  };

  outputs =
    {
      self,
      igloo,
      nixpkgs-master,
      utils,
      tommy,
      bats,
      purse-first,
      tap,
      crap,
      doppelgang,
      conformist,
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
        pkgs = import igloo { inherit system; };

        gomod = import ./go/gomod.nix {
          inherit
            pkgs
            system
            tap
            tommy
            crap
            ;
          # Scope the producer at go/ so downstream consumers reference
          # go-pkgs directly with no subPath. Madder's repo root has
          # no Go-relevant assets, so a full-repo filter would only
          # bloat the closure.
          src = self + "/go";
        };

        inherit (gomod.goPkgs) go-pkgs go-pkgs-test;

        # conformist config, Nix-generated from ./conformist.nix merged with the
        # eng-convention preset (conformist.lib.presets.eng). Two consumers:
        #   - `nix fmt` uses build.wrapper (config + every tool baked as
        #     /nix/store paths).
        #   - `just lint-fmt` / `just codemod-fmt` pass build.configFile to a
        #     BARE conformist via --config-file (see go/default.nix devShell).
        # We expose the BARE conformist (not the wrapper) on the devShell PATH
        # because dagnabit's facade formatter (`dagnabit export`) appends
        # `--tree-root <outdir>` + `--config-file <generated>` to the on-PATH
        # conformist, which would collide with the wrapper's baked flags.
        # lint-fmt/codemod-fmt reach the generated config via
        # $MADDER_CONFORMIST_CONFIG (those recipes self-enter the devShell).
        # dagnabit reaches it via the dagnabitWrapped shim (go/default.nix),
        # which bakes the config + a runtime ceiling so the facade lane is
        # hermetic even in the env-less pre-merge hook (purse-first#159, #163).
        # See conformist-nix(7).
        conformistEval = conformist.lib.evalModule pkgs {
          imports = [
            conformist.lib.presets.eng
            ./conformist.nix
          ];
          package = conformist.packages.${system}.default;
        };

        # Impure lane: the eng-convention checks that need a live working tree /
        # host tools (git-remotes, git-default-branch, sweatfile, agents-md,
        # gomod2nix). They CANNOT run in the sandboxed pure config, so `just
        # lint-worktree` runs them against the real worktree via this config.
        # See conformist.lib.presets.eng-impure.
        conformistImpureEval = conformist.lib.evalModule pkgs {
          imports = [ conformist.lib.presets.eng-impure ];
          package = conformist.packages.${system}.default;
          projectRootFile = "flake.nix";
        };

        result = import ./go/default.nix {
          nixpkgs = igloo;
          inherit
            nixpkgs-master
            tommy
            bats
            purse-first
            doppelgang
            conformist
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
          # The Nix-generated conformist config file (full eng roster). The
          # devShell exposes it as $MADDER_CONFORMIST_CONFIG so `just lint-fmt` /
          # `just codemod-fmt` pass it to the bare conformist via --config-file.
          conformistConfig = conformistEval.config.build.configFile;
          # The impure-lane config (git-state checks). Exposed as
          # $MADDER_CONFORMIST_IMPURE_CONFIG for `just lint-worktree`.
          conformistImpureConfig = conformistImpureEval.config.build.configFile;
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
        # `nix fmt` runs the generated conformist wrapper (see conformistEval).
        formatter = conformistEval.config.build.wrapper;
      }
    ));
}
