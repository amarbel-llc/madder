{
  nixpkgs,
  nixpkgs-master,
  tommy,
  bob,
  purse-first,
  system,
  man7Src ? null,
  # Test-only inputs consumed by the bats installCheckPhase shared
  # between `madder` and `madder-race`. Defaulted to null so direct
  # `import ./go/default.nix` callers without a flake context still
  # work — they just can't run the bats lanes. versionEnv is the
  # source-of-truth file the bats version test consults.
  batsSrc ? null,
  versionEnv ? null,
  # Passed to buildGoApplication's `version` and `commit` attrs; the
  # fork's nixpkgs auto-injects them as `-X main.version` and
  # `-X main.commit` ldflags on every subPackage. Defaulted so that
  # non-flake consumers (e.g. direct `import ./go/default.nix`) still
  # work, but release builds always override via flake.nix.
  version ? "dev",
  commit ? "unknown",
}:
let
  # The fork's default.nix shim auto-applies overlays.default, so an
  # explicit `overlays = [ nixpkgs.overlays.default ]` here would just
  # compose the overlay twice.
  pkgs = import nixpkgs { inherit system; };
  pkgs-master = import nixpkgs-master { inherit system; };

  # mkBatsLane wraps pkgs.testers.batsLane with madder's parameter
  # shape: vanilla bats, bob's bats-libs on BATS_LIB_PATH, MADDER_BIN
  # exported, version.env staged sibling-of-bats, jq for the
  # cli_contract.bats helpers. Only MADDER_BIN is exported by the
  # builder; common.bash derives CG_BIN from MADDER_BIN's dirname
  # because both binaries ship from the same install.
  mkBatsLane = { filter ? "!net_cap", base ? madder }:
    pkgs.testers.batsLane {
      inherit base filter batsSrc;
      binaryName = "madder";
      binaryEnvVarName = "MADDER_BIN";
      # bats-libs lays out the libraries under share/bats/<libname>/, so
      # BATS_LIB_PATH needs to point at share/bats, not the derivation
      # root. (Upstream nixpkgs convention; pkgs.testers.batsLane could
      # do this resolution itself — see follow-up.)
      batsLibPath = [ "${bob.packages.${system}.bats-libs}/share/bats" ];
      extraStagedFiles = [
        { src = versionEnv; dest = "version.env"; }
      ];
      # pkgs.parallel is needed because vanilla bats's --jobs path
      # shells out to GNU parallel, which the builder doesn't include
      # by default. jq is for cli_contract.bats's JSON helpers.
      nativeBuildInputs = [ pkgs-master.jq pkgs.parallel ];
    };

  # madder-cli-cover's coverIntegrationCommand is a phase fragment
  # (shell embedded in buildGoCover's own installCheckPhase), not a
  # derivation, so pkgs.testers.batsLane can't be substituted directly
  # here. The shell mirrors what the builder generates internally.
  cliCoverIntegrationCommand = ''
    mkdir -p stage/zz-tests_bats
    cp -r ${batsSrc}/* stage/zz-tests_bats/
    chmod -R u+w stage
    cp ${versionEnv} stage/version.env

    export MADDER_BIN="$out/bin/madder"
    export BATS_LIB_PATH="''${BATS_LIB_PATH:+$BATS_LIB_PATH:}${bob.packages.${system}.bats-libs}/share/bats"

    cd stage/zz-tests_bats
    ${pkgs.bats}/bin/bats \
      --jobs $NIX_BUILD_CORES \
      --filter-tags '!net_cap' \
      *.bats
    cd "$NIX_BUILD_TOP"
  '';

  # The bats integration suite is intentionally NOT in installCheckPhase
  # — it lives as separate `bats-*` lanes (`batsLaneOutputs`) so
  # downstream consumers don't pay the integration-test cost on a
  # from-source rebuild. See amarbel-llc/eng#62.
  madder = pkgs.buildGoApplication ({
    pname = "madder";
    inherit version commit;
    src = ./.;
    pwd = ./.;
    subPackages = [
      "cmd/madder"
      "cmd/madder-cache"
      "cmd/madder-gen_man"
      "cmd/cutting-garden"
      "cmd/cg"
    ];
    modules = ./gomod2nix.toml;
    go = pkgs-master.go_1_26;
    GOTOOLCHAIN = "local";

    nativeBuildInputs = [
      purse-first.packages.${system}.dagnabit
    ] ++ pkgs-master.lib.optionals (man7Src != null) [
      pkgs-master.pandoc
    ];

    preBuild = ''
      dagnabit export
    '';

    # buildGoApplication's stock goCheckHook only tests subPackages, which
    # are cmd/* directories that have no test files. Override checkPhase
    # to test all packages with the `test` build tag (gates internal
    # test-only symbols like ui.T / ui.TestContext). doCheck stays true
    # by default in buildGoApplication.
    checkPhase = ''
      runHook preCheck
      go test -tags test -p $NIX_BUILD_CORES ./...
      runHook postCheck
    '';

    # madder-gen_man takes a *prefix* and writes to {prefix}/share/man/man1/
    postInstall = ''
      $out/bin/madder-gen_man $out
      rm $out/bin/madder-gen_man
    ''
    + pkgs-master.lib.optionalString (man7Src != null) ''
      mkdir -p $out/share/man/man7
      for f in ${man7Src}/*.md; do
        name="$(basename "$f" .md)"
        pandoc -s -t man "$f" -o "$out/share/man/man7/$name.7"
        ${pkgs-master.gnused}/bin/sed -i '3a\.\" Formatting overrides\n.ss 12 0\n.na' "$out/share/man/man7/$name.7"
      done
    '';
  });

  # madder-race exercises the same package-level test surface as `madder`
  # but with the Go race detector enabled. Concurrent-write paths
  # (pack_parallel, blob_mover link publish) are load-bearing, so the
  # default `just test` target builds this variant. Build artifacts are
  # NOT release-suitable — race-instrumented binaries are slower and
  # not what we ship.
  #
  # The bats lane against this binary lives in `batsLaneOutputs` as
  # `bats-race`. There is no nix-driven race+net_cap lane today —
  # the net_cap suite needs the devshell-only sftp test harness.
  madder-race = pkgs.buildGoRace {
    base = madder;
    tags = [ "test" ];
  };

  # madder-cover runs the unit suite with coverage collection enabled
  # and writes the profile to $out/coverage.out. Coverage collection
  # uses installCheckPhase (after $out exists from installPhase) rather
  # than checkPhase so the profile lands at a stable path callers can
  # read. View the HTML report with
  # `go tool cover -html=result/coverage.out`.
  # The justfile recipe `test-go-cover` invokes this and tails the
  # func summary.
  #
  # The bats suite is intentionally NOT run here: this lane's purpose
  # is unit-test coverage (Go-level), and madder-race already exercises
  # the bats suite against an instrumented binary. Mixing CLI bats
  # coverage in here would conflate two signals — the coverage profile
  # would no longer correspond to "what `go test` covered."
  madder-cover = madder.overrideAttrs (old: {
    pname = "madder-cover";
    # Suppress the default checkPhase — its job (running the suite)
    # is being replaced with an installCheckPhase that emits the
    # coverage profile to $out.
    doCheck = false;
    doInstallCheck = true;
    installCheckPhase = ''
      runHook preInstallCheck
      go test -tags test \
        -coverprofile=$out/coverage.out \
        -covermode=atomic \
        -p $NIX_BUILD_CORES \
        ./...
      runHook postInstallCheck
    '';
  });

  # madder-cli-cover builds the CLI binary with `go build -cover`, then
  # runs the bats suite against the instrumented `$out/bin/madder` under
  # a fresh $GOCOVERDIR. After the suite, the helper persists binary
  # covdata fragments to `$out/covdata/` (mergeable with unit-test
  # fragments via `go tool covdata merge`) and a textfmt profile to
  # `$out/coverage.out` (inspectable with `go tool cover`).
  #
  # This complements madder-cover (unit-test coverage) — the two answer
  # different questions: madder-cover shows what `go test` covers,
  # madder-cli-cover shows what the bats suite exercises through the
  # real CLI. Merging them (via `just cover-merged`) gives the full
  # picture.
  madder-cli-cover = pkgs.buildGoCover {
    base = madder;
    extraNativeInstallCheckInputs = [ pkgs-master.jq pkgs.parallel ];
    coverIntegrationCommand = cliCoverIntegrationCommand;
  };

  # Auto-discover bats `file_tags` directives at flake-eval time and
  # produce one `bats-${tag}` derivation per unique tag, plus a
  # `bats-default` lane for the standard `!net_cap` filter.
  #
  # Each `.bats` file declares its tags via a `# bats file_tags=foo,bar`
  # comment. This block reads all `.bats` files under `batsSrc`, splits
  # those directives, deduplicates, and produces a `mkBatsLane` per
  # unique tag. Adding/removing tags in a `.bats` file invalidates the
  # eval cache — the right behavior, but worth knowing.
  #
  # Only file-level tags are surfaced; per-test `@test`-line tags are
  # not auto-discovered. Use `mkBatsLane` directly for ad-hoc filters.
  #
  # Empty when batsSrc is null (non-flake import path), so direct
  # `import ./go/default.nix` callers without a flake context stay
  # working — they just don't get the bats lane outputs.
  batsLaneOutputs =
    if batsSrc == null then { }
    else
      let
        batsFiles = builtins.filter
          (f: pkgs-master.lib.hasSuffix ".bats" f)
          (builtins.attrNames (builtins.readDir batsSrc));

        extractFileTags = file:
          let
            content = builtins.readFile (batsSrc + "/${file}");
            lines = pkgs-master.lib.splitString "\n" content;
            tagLines = builtins.filter
              (l: pkgs-master.lib.hasPrefix "# bats file_tags=" l)
              lines;
          in
            if tagLines == [ ] then [ ]
            else pkgs-master.lib.splitString ","
              (pkgs-master.lib.removePrefix "# bats file_tags="
                (builtins.head tagLines));

        allFileTags = pkgs-master.lib.unique
          (pkgs-master.lib.concatMap extractFileTags batsFiles);
      in
        pkgs-master.lib.listToAttrs (map
          (tag: pkgs-master.lib.nameValuePair "bats-${tag}"
            (mkBatsLane { filter = tag; }))
          allFileTags) // {
          bats-default = mkBatsLane { filter = "!net_cap"; };
          # No bats-race-net_cap: the net_cap suite needs
          # madder-test-sftp-server (devshell-only by design).
          bats-race = mkBatsLane { filter = "!net_cap"; base = madder-race; };
        };

  # Devshell-only test harness for SFTP integration tests (RFC 0001).
  # Intentionally NOT included in the `packages` output — release
  # artifacts must not ship a server that accepts any password.
  madder-test-sftp-server = pkgs.buildGoApplication {
    pname = "madder-test-sftp-server";
    version = "0.0.0";
    src = ./.;
    pwd = ./.;
    subPackages = [ "cmd/madder-test-sftp-server" ];
    modules = ./gomod2nix.toml;
    go = pkgs-master.go_1_26;
    GOTOOLCHAIN = "local";
  };
in
{
  packages = {
    inherit madder madder-race madder-cover madder-cli-cover;
    default = madder;
  } // batsLaneOutputs;

  devShells.default = pkgs-master.mkShell {
    packages = [
      (pkgs.mkGoEnv { pwd = ./.; })
      tommy.packages.${system}.default
      bob.packages.${system}.batman
      purse-first.packages.${system}.dagnabit
      madder-test-sftp-server
    ]
    ++ (with pkgs-master; [
      delve
      gofumpt
      gopls
      gotools
      just
      pandoc
      shfmt
    ]);

    GOTOOLCHAIN = "local";
  };
}
