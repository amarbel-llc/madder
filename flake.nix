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
      # NB: conformist no longer consumes purse-first (it builds
      # golangci-lint-dewey from a pinned FOD), so there is no
      # inputs.purse-first.follows override here — that would warn on a
      # non-existent input. purse-first still follows conformist (above) to
      # keep ONE conformist node in the lock (doppelgang lint dedup).
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
        #
        # Also carries the dewey-facade-export drift CHECK as the merge-gate
        # safety net (FDR 0013): `just lint-worktree` is in the `lint` aggregate
        # the pre-merge hook runs, so committed facade drift fails the merge even
        # if the pre-commit auto-repair hook was bypassed. This replaces the old
        # standalone `lint-facades` recipe (`dagnabit export --check`). The
        # pre-commit lane (conformistCodegenEval) does the REPAIR; this lane does
        # the merge CHECK — same module, two lanes.
        conformistImpureEval = conformist.lib.evalModule pkgs {
          imports = [
            conformist.lib.presets.eng-impure
            purse-first.lib.conformistLinters.dewey-facade-export
            conformistFacadeModule
          ];
          package = conformist.packages.${system}.default;
          projectRootFile = "flake.nix";
        };

        # The two codegen-repair lanes. Neither has a conformist registry program
        # (they are tommy-/dewey-specific), so they live as inline freeform blocks
        # here where the `tommy` / `purse-first` flake inputs are in scope — a
        # standalone ./conformist.nix can't see flake inputs (cutting-garden#114
        # pattern). Both run at PRE-COMMIT (conformistCodegenEval below), so the
        # staged hook regenerates-and-stages drift at authoring time; the new
        # conformist stage-mutation tiers (#55/#56/#57) restage the regenerated
        # outputs into the commit (tracked/new/deleted).
        #
        # `command = "true"` makes both checks no-ops: a real check needs the `go`
        # toolchain and (tommy) gofumpt's its own render, so `conformist check`
        # would false-positive in the sandbox. The REPAIR side is what matters at
        # pre-commit. Drift is still loud-gated at the merge by `just`
        # (lint-tommy / the facade check in lint-worktree).
        conformistCodegenModule =
          { ... }:
          {
            # tommy: TOML formatter + the *_tommy.go codegen-repair lane.
            # getExe' for explicit binary names: the module would lib.getExe the
            # `command`, but tommy lacks meta.mainProgram and — critically —
            # `repair-command` is a FREEFORM field that is NOT coerced, so a bare
            # derivation there serializes to the store DIRECTORY, not the binary.
            settings.formatter.tommy = {
              command = pkgs.lib.getExe' tommy.packages.${system}.default "tommy";
              options = [ "fmt" ];
              includes = [ "*.toml" ];
            };
            settings.linter.tommy-codegen = {
              command = "true";
              "repair-command" =
                pkgs.lib.getExe' tommy.packages.${system}.conformist-tommy-codegen
                  "conformist-tommy-codegen";
              includes = [ "*.go" ];
              "passes-files" = false;
              "restage-repair-outputs" = true; # tier 2 (#55): restage modified *_tommy.go
              "stage-new-outputs" = true; # tier 3 (#56): stage a brand-new companion
              "stage-deleted-outputs" = true; # tier 4 (#57): stage a removed companion
            };
          };

        # The dewey pkgs/ facade-export lane, CONSUMED from purse-first's published
        # module (purse-first#163) rather than hand-wired: the module owns the
        # dagnabit invocation + the DAGNABIT_CONFORMIST_CONFIG threading, fed the
        # PURE formatter config so its facade-format pass matches `nix fmt`. This
        # is what retired madder's old dagnabitWrapped shim + lint/codemod-facades
        # recipes (FDR 0013 pilot). The tier opt-ins are layered on here (the
        # upstream module ships the check/repair commands but not the
        # stage-mutation flags, since it was authored for the merge-gate lane).
        #
        # conformistConfig = conformistEval.config.build.configFile is the PURE
        # eval's output, referenced from a SEPARATE eval (this one) — so it is not
        # a self-reference: the facade linter does not live in the eval that
        # produces the config it bakes. Same cycle-free shape purse-first uses
        # (impure eval's linter fed the pure eval's config).
        conformistFacadeModule =
          { ... }:
          {
            linters.dewey-facade-export.enable = true;
            linters.dewey-facade-export.deweyDir = "go";
            # madder uses //go:generate dagnabit export directives, not --library.
            linters.dewey-facade-export.library = false;
            # Pinned package ⇒ hermetic, PATH-independent dagnabit (no ambient
            # dependency). This module is shared by both the pre-commit codegen
            # eval (REPAIR) and conformistImpureEval (the merge-gate CHECK via
            # lint-worktree); the pin keeps the lane self-contained in either.
            linters.dewey-facade-export.dagnabitPackage = purse-first.packages.${system}.dagnabit;
            linters.dewey-facade-export.conformistConfig = conformistEval.config.build.configFile;
            # Layer the stage-mutation tiers onto the module's generated linter.
            settings.linter.dewey-facade-export = {
              "restage-repair-outputs" = true; # tier 2: restage modified facades
              "stage-new-outputs" = true; # tier 3: stage a brand-new pkgs/ facade
              "stage-deleted-outputs" = true; # tier 4: stage a removed/relocated facade
            };
          };

        # Dedicated PRE-COMMIT (codegen-repair) eval. EXPLICIT membership: the
        # formatters + excludes from ./conformist.nix, plus the two codegen-repair
        # lanes — but deliberately NOT presets.eng (its convention linters stay at
        # the merge/worktree gate, not commit time). build.preCommit from THIS eval
        # is the sweatfile [hooks].pre-commit hook, so a commit auto-formats and
        # regenerates-and-stages codegen drift, and nothing else.
        #
        # Hand-curated here as the first proof of "scope which linters belong to a
        # hook"; the declarative generalization is conformist's RFC-0002 §4 [hook]
        # section (followup). See conformist#<followup>.
        conformistCodegenEval = conformist.lib.evalModule pkgs {
          imports = [
            ./conformist.nix
            # The facade-export linter MODULE (options.linters.dewey-facade-export.*);
            # conformistFacadeModule below sets its enable + params.
            purse-first.lib.conformistLinters.dewey-facade-export
            conformistCodegenModule
            conformistFacadeModule
          ];
          package = conformist.packages.${system}.default;
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
          # The module-generated, toolchain-hermetic per-commit hook wrapper,
          # exposed on the devShell PATH as `conformist-pre-commit` — the
          # sweatfile [hooks].pre-commit command. Built from the dedicated
          # codegen-repair eval (formatters + tommy + facade, no presets.eng), so
          # a commit auto-formats AND regenerates-and-stages codegen drift via the
          # stage-mutation tiers. (The pure conformistEval still drives `nix fmt`
          # + the sandboxed check + the eng linters at the merge gate.)
          conformistPreCommit = conformistCodegenEval.config.build.preCommit;
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
          # Dogfood build.preCommit: `nix build .#conformist-pre-commit` forces
          # the codegen-eval output to build (verifies the pinned conformist
          # exposes it + the tommy/facade lanes resolve), and it is the same
          # wrapper the devShell puts on PATH.
          conformist-pre-commit = conformistCodegenEval.config.build.preCommit;
        };
        devShells.default = result.devShells.default;
        # `nix fmt` runs the generated conformist wrapper (see conformistEval).
        formatter = conformistEval.config.build.wrapper;
      }
    ));
}
