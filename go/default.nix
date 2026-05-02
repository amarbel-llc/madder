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

  # Shared bats invocation: stages the bats sources + version.env into
  # a writable scratch dir, exports MADDER_BIN, and runs the suite under
  # `bats --no-sandbox` (the nix build sandbox is already an isolation
  # layer; sandcastle/fence on Darwin needs /usr/bin/sandbox-exec which
  # the nix sandbox doesn't expose).
  #
  # `filter` is forwarded verbatim to `bats --filter-tags`. Default
  # `!net_cap` excludes loopback-binding scenarios the nix sandbox
  # doesn't grant; mkBatsLane callers override with arbitrary tag
  # expressions for dev-loop selection.
  #
  # `madderBin` is the path string MADDER_BIN should point at. Default
  # `$out/bin/madder` is a bash literal (the `$` is plain text in nix's
  # `''` strings) — appropriate for installCheckPhase where bats runs
  # against the just-installed binary in the same derivation. mkBatsLane
  # passes a nix-resolved store path (`${base}/bin/madder`) so its
  # derivation references an existing `madder` build instead of
  # rebuilding Go per filter.
  #
  # `--jobs $NIX_BUILD_CORES` and `BATS_TEST_TIMEOUT=30` mirror
  # zz-tests_bats/justfile so dev-loop and release lanes share the
  # same parallelism/timeout contract.
  #
  # jq is referenced inline by cli_contract.bats helpers (parsing
  # `write -check` JSON output to compute "missing" digests).
  mkBatsRunCommand = {
    filter ? "!net_cap",
    madderBin ? "$out/bin/madder",
    cgBin ? "$out/bin/cutting-garden",
  }: ''
    mkdir -p stage/zz-tests_bats
    cp -r ${batsSrc}/* stage/zz-tests_bats/
    chmod -R u+w stage

    # version_matches_source_of_truth reads MADDER_VERSION from
    # version.env at $BATS_TEST_DIRNAME/../version.env. Mirror that
    # layout: stage/version.env is sibling of stage/zz-tests_bats/.
    cp ${versionEnv} stage/version.env

    export MADDER_BIN="${madderBin}"
    export CG_BIN="${cgBin}"
    export BATS_TEST_TIMEOUT=30

    cd stage/zz-tests_bats
    ${bob.packages.${system}.batman}/bin/bats --no-sandbox \
      --jobs $NIX_BUILD_CORES \
      --filter-tags '${filter}' \
      *.bats
    cd "$NIX_BUILD_TOP"
  '';

  # Sanitize a bats `--filter-tags` expression for use as a derivation
  # name suffix. Replaces shell-unfriendly characters with `_`.
  batsLaneSuffix = filter:
    builtins.replaceStrings [ "!" "," ":" " " ] [ "not_" "_" "_" "_" ] filter;

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
      ln -s cutting-garden $out/bin/cg
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
    extraNativeInstallCheckInputs = [ pkgs-master.jq ];
    coverIntegrationCommand = mkBatsRunCommand { };
  };

  # mkBatsLane: standalone derivation that runs the bats suite against
  # an existing `madder` build (referenced by store path), filtered by
  # tag expression. The auto-generated `bats-${tag}` flake outputs (see
  # `batsLaneOutputs` below) are the canonical entry points; this
  # function is kept internal so the same shape can be reused for the
  # `bats-default` lane and any future hand-rolled lanes.
  #
  # Crucially this does NOT rebuild Go — the bats lane references
  # `${base}/bin/madder` from an upstream `madder` derivation that's
  # already in the cache. Per-filter cache miss only re-runs the bats
  # suite, never the compile. Going through the standard flake-eval
  # path (named output, no `--impure --expr` workaround) means the
  # consumed `madder` derivation is bit-identical to `.#madder`'s,
  # so dev-loop and release share a single cache lane.
  #
  # Output is a stamp file (touched on success); the derivation exists
  # to stand for "the bats suite passed under this filter for this base."
  mkBatsLane = { filter ? "!net_cap", base ? madder }:
    pkgs.runCommand
      "${base.pname}-bats-${batsLaneSuffix filter}"
      {
        nativeBuildInputs = [ pkgs-master.jq ];
      }
      (mkBatsRunCommand {
        inherit filter;
        madderBin = "${base}/bin/madder";
        cgBin = "${base}/bin/cutting-garden";
      } + ''
        touch $out
      '');

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
