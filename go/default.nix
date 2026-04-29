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
  pkgs = import nixpkgs {
    inherit system;
    overlays = [ nixpkgs.overlays.default ];
  };
  pkgs-master = import nixpkgs-master { inherit system; };

  # =========================================================================
  # Tracer-bullet implementations of buildGoCover and buildGoRace as proposed
  # in amarbel-llc/nixpkgs#13. These are deliberately co-located with their
  # one consumer (this file) for now — once the API stabilizes here, lift
  # them into pkgs/build-support/gomod2nix/default.nix in the fork.
  #
  # The helpers operate via overrideAttrs on a base derivation produced by
  # buildGoApplication, rather than wrapping buildGoApplication directly.
  # That keeps the call site for `madder-race` / `madder-cli-cover` close
  # to today's idiom (override the release derivation), and avoids forcing
  # the upstream API into a particular argument shape before we know what
  # works.
  # =========================================================================

  # buildGoRace: race-instrumented variant of an existing buildGoApplication
  # derivation. Sets CGO_ENABLED, appends `-race` to buildFlagsArray (so the
  # `go install` that produces $out/bin/* picks it up), and overrides
  # checkPhase to also pass `-race` to `go test`. Caller's existing
  # checkPhase tags / -p handling are preserved by passing them in via the
  # `tags` arg.
  buildGoRace = { base, tags ? [ ], pnameSuffix ? "-race" }:
    base.overrideAttrs (old: {
      pname = "${old.pname}${pnameSuffix}";
      CGO_ENABLED = 1;
      # See note on buildGoCover.preBuild — buildFlagsArray must be set
      # as a true bash array via preBuild rather than as a nix list attr.
      preBuild = (old.preBuild or "") + ''
        buildFlagsArray+=("-race")
      '';
      checkPhase = ''
        runHook preCheck
        go test ${if tags == [ ] then "" else "-tags ${pkgs-master.lib.concatStringsSep "," tags}"} -race -p $NIX_BUILD_CORES ./...
        runHook postCheck
      '';
    });

  # buildGoCover: coverage-instrumented variant of an existing
  # buildGoApplication derivation. Builds the binary with `go build -cover
  # -covermode=atomic`, then runs `coverIntegrationCommand` (which the
  # caller provides as a phase fragment) under a fresh $GOCOVERDIR. After
  # the integration command, the helper:
  #   - copies the binary covdata fragments to $out/covdata/  (mergeable)
  #   - converts them to textfmt at $out/coverage.out         (inspectable)
  #
  # The caller's `coverIntegrationCommand` runs against `$out/bin/<binary>`
  # with $GOCOVERDIR already exported. It is responsible for whatever test
  # plumbing it needs (MADDER_BIN, staging files, etc.).
  buildGoCover =
    { base
    , coverIntegrationCommand
    , pnameSuffix ? "-cli-cover"
    , extraNativeInstallCheckInputs ? [ ]
    }:
    base.overrideAttrs (old: {
      pname = "${old.pname}${pnameSuffix}";

      # buildFlagsArray must be set as a true bash array, not via a
      # nix list attr — stdenv serializes list attrs as space-joined
      # strings that the goBuildHook treats as a single argv entry,
      # which breaks for multi-flag values like `-covermode=atomic`.
      # Setting it in preBuild puts it in the bash environment as an
      # actual array, which `declare -p > $TMPDIR/buildFlagsArray`
      # can then round-trip correctly.
      preBuild = (old.preBuild or "") + ''
        buildFlagsArray+=("-cover" "-covermode=atomic")
      '';

      # The base derivation's postInstall invokes the instrumented
      # `$out/bin/madder-gen_man` to render man pages. Without a
      # GOCOVERDIR exported, the cover runtime prints a warning to
      # stderr ("GOCOVERDIR not set, no coverage data emitted"). The
      # man-gen run isn't part of the integration coverage surface,
      # so route its fragments to a discardable scratch dir before
      # running the existing postInstall.
      postInstall = ''
        export GOCOVERDIR="$(mktemp -d)"
      '' + (old.postInstall or "");

      doInstallCheck = true;
      nativeInstallCheckInputs =
        (old.nativeInstallCheckInputs or [ ])
        ++ extraNativeInstallCheckInputs;
      installCheckPhase = ''
        runHook preInstallCheck

        gocover_data="$(mktemp -d)"
        export GOCOVERDIR="$gocover_data"

        ${coverIntegrationCommand}

        mkdir -p $out/covdata $out
        cp -r "$gocover_data"/* $out/covdata/
        go tool covdata textfmt -i="$gocover_data" -o="$out/coverage.out"

        runHook postInstallCheck
      '';
    });

  # Shared bats invocation: stages the bats sources + version.env into
  # a writable scratch dir, exports MADDER_BIN, and runs the suite under
  # `bats --no-sandbox` (the nix build sandbox is already an isolation
  # layer; sandcastle/fence on Darwin needs /usr/bin/sandbox-exec which
  # the nix sandbox doesn't expose).
  #
  # net_cap-tagged scenarios are filtered out — they need loopback
  # binding the nix sandbox doesn't grant.
  #
  # Used by:
  #   - batsInstallCheck (consumed by madder + madder-race via attrset
  #     merge / overrideAttrs)
  #   - madder-cli-cover (passed as coverIntegrationCommand to buildGoCover)
  #
  # jq is referenced inline by cli_contract.bats helpers (parsing
  # `write -check` JSON output to compute "missing" digests).
  batsRunCommand = ''
    mkdir -p stage/zz-tests_bats
    cp -r ${batsSrc}/* stage/zz-tests_bats/
    chmod -R u+w stage

    # version_matches_source_of_truth reads MADDER_VERSION from
    # version.env at $BATS_TEST_DIRNAME/../version.env. Mirror that
    # layout: stage/version.env is sibling of stage/zz-tests_bats/.
    cp ${versionEnv} stage/version.env

    export MADDER_BIN="$out/bin/madder"

    cd stage/zz-tests_bats
    ${bob.packages.${system}.batman}/bin/bats --no-sandbox \
      --filter-tags '!net_cap' \
      *.bats
    cd "$NIX_BUILD_TOP"
  '';

  # Shared bats installCheckPhase: runs the bats suite against the
  # derivation's `$out/bin/madder` after installPhase. Used by `madder`
  # (release lane) and `madder-race` (race-instrumented lane).
  batsInstallCheck = {
    doInstallCheck = batsSrc != null && versionEnv != null;
    nativeInstallCheckInputs = [ pkgs-master.jq ];
    installCheckPhase = ''
      runHook preInstallCheck
      ${batsRunCommand}
      runHook postInstallCheck
    '';
  };

  madder = pkgs.buildGoApplication ({
    pname = "madder";
    inherit version commit;
    src = ./.;
    pwd = ./.;
    subPackages = [
      "cmd/madder"
      "cmd/madder-cache"
      "cmd/madder-gen_man"
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
  } // batsInstallCheck);

  # madder-race exercises the same package-level test surface as `madder`
  # but with the Go race detector enabled. Concurrent-write paths
  # (pack_parallel, blob_mover link publish) are load-bearing, so the
  # default `just test` target builds this variant. Build artifacts are
  # NOT release-suitable — race-instrumented binaries are slower and
  # not what we ship.
  #
  # The shared bats installCheckPhase (inherited from `madder` via
  # overrideAttrs) reruns the suite against the race-instrumented
  # `$out/bin/madder`, catching races that only surface in real CLI flows.
  madder-race = buildGoRace {
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
  madder-cli-cover = buildGoCover {
    base = madder;
    extraNativeInstallCheckInputs = [ pkgs-master.jq ];
    coverIntegrationCommand = batsRunCommand;
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
  };

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
