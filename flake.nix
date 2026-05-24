{
  inputs = {
    nixpkgs.url = "github:amarbel-llc/nixpkgs";
    nixpkgs-master.url = "github:NixOS/nixpkgs/d233902339c02a9c334e7e593de68855ad26c4cb";
    utils.url = "https://flakehub.com/f/numtide/flake-utils/0.1.102";

    tommy = {
      url = "github:amarbel-llc/tommy";
      inputs.utils.follows = "utils";
    };

    bats = {
      url = "github:amarbel-llc/bats";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };

    purse-first = {
      url = "github:amarbel-llc/purse-first";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };

    # Sourced via goFlakeInputs (see madder#208) so a tap bump only
    # touches flake.lock — no go.mod / gomod2nix.toml lockstep edits.
    tap = {
      url = "github:amarbel-llc/tap";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
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
      ...
    }:
    let
      # version.env at repo root is the single source of truth for
      # the release version. Burnt into the binary via the fork's
      # auto-injected -ldflags; consumed by bats too. `just bump-version`
      # sed-rewrites version.env. Match expression captures everything
      # after `MADDER_VERSION=` up to the line break.
      madderVersion = builtins.head (builtins.match
        ".*MADDER_VERSION=([^\n]+).*"
        (builtins.readFile ./version.env));
      # shortRev for clean builds, dirtyShortRev for dirty working trees
      # (so devshell builds show `dirty-abcdef` rather than masquerading
      # as a clean release), "unknown" as a last-resort fallback.
      madderCommit = self.shortRev or self.dirtyShortRev or "unknown";
    in
    (utils.lib.eachDefaultSystem (
      system:
      let
        # Only needed for the inline mkGoPkgs equivalent below.
        # buildGoApplication / mkGoEnv consumers live in go/default.nix.
        pkgs = import nixpkgs { inherit system; };

        # Producer half of RFC 0001's flake-input-go_mod protocol
        # (#212). Splits madder's Go source tree into two outputs:
        #
        #   - go-pkgs: prod-shape — *.go excluding *_test.go, plus
        #     go.mod / go.sum / gomod2nix.toml. What downstream
        #     consumers (e.g. dodder) bridge against via goFlakeInputs
        #     when they only compile madder's code.
        #
        #   - go-pkgs-test: superset adding *_test.go and testdata/**.
        #     What madder's own derivations consume as `src` (so
        #     checkPhase can run `go test ./...`), and what downstream
        #     consumers bridge against if they need to run madder's
        #     tests.
        #
        # Inlined here (rather than calling pkgs.goSourceFilter twice)
        # because goSourceFilter's default keep-set matches *_test.go
        # via `.*\.go$`, so a naive `go-pkgs` would leak test files
        # into prod consumers. amarbel-llc/nixpkgs#46 proposes hoisting
        # this split into an `mkGoPkgs` helper plus an RFC 0001
        # amendment; once that lands, this whole block collapses to:
        #
        #   inherit (pkgs.mkGoPkgs { src = self + "/go"; })
        #     go-pkgs go-pkgs-test;
        goPkgsSrc = self + "/go";

        mkFilteredGoTree = { name, predicate }:
          let
            filteredPath = builtins.path {
              inherit name;
              path = goPkgsSrc;
              filter = path: type:
                let
                  relPath = pkgs.lib.removePrefix
                    (toString goPkgsSrc + "/") (toString path);
                in
                type == "directory" || predicate relPath;
            };
          in
          pkgs.runCommand name {
            preferLocalBuild = true;
            allowSubstitutes = false;
          } ''
            cp -r ${filteredPath} $out
          '';

        isModuleFile = relPath:
          relPath == "go.mod"
          || relPath == "go.sum"
          || relPath == "gomod2nix.toml";

        isProdGoFile = relPath:
          pkgs.lib.hasSuffix ".go" relPath
          && !pkgs.lib.hasSuffix "_test.go" relPath;

        isTestGoFile = relPath:
          pkgs.lib.hasSuffix "_test.go" relPath;

        isTestdataFile = relPath:
          builtins.match ".*/testdata/.*" relPath != null
          || builtins.match "^testdata/.*" relPath != null;

        go-pkgs = mkFilteredGoTree {
          name = "madder-go-pkgs";
          predicate = relPath:
            isProdGoFile relPath || isModuleFile relPath;
        };

        go-pkgs-test = mkFilteredGoTree {
          name = "madder-go-pkgs-test";
          predicate = relPath:
            isProdGoFile relPath
            || isModuleFile relPath
            || isTestGoFile relPath
            || isTestdataFile relPath;
        };

        result = import ./go/default.nix {
          inherit
            nixpkgs
            nixpkgs-master
            tommy
            bats
            purse-first
            tap
            system
            ;
          # Pivot self-consumption onto the published artifact: every
          # buildGoApplication in go/default.nix uses this as `src`,
          # so the same closure downstream consumers receive via
          # go-pkgs-test is what madder builds itself from. Contract
          # test for the producer-side split — if the filter ever
          # drops a file the build needs, this build breaks.
          goPkgsTest = go-pkgs-test;
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
        packages = result.packages // { inherit go-pkgs go-pkgs-test; };
        devShells.default = result.devShells.default;
      }
    ));
}
