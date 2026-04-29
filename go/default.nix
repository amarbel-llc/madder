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

  # Shared bats installCheckPhase: runs the bats suite against the
  # derivation's `$out/bin/madder` after installPhase. Used by `madder`
  # (release lane) and `madder-race` (race-instrumented lane) so a
  # nix-build failure means the suite failed against a real installed
  # binary. net_cap-tagged scenarios are filtered out — they need
  # loopback binding the nix sandbox doesn't grant.
  #
  # Plain `bats --no-sandbox` (not bob's batman wrapper): on Darwin the
  # sandcastle/fence path shells out to /usr/bin/sandbox-exec, which the
  # nix build sandbox doesn't expose. The build sandbox is already an
  # isolation layer, so the extra wrapping is redundant here.
  #
  # jq is referenced inline by cli_contract.bats helpers (parsing
  # `write -check` JSON output to compute "missing" digests). Other
  # scenarios use only coreutils + the binary under test, both already
  # on PATH inside the nix build sandbox.
  batsInstallCheck = {
    doInstallCheck = batsSrc != null && versionEnv != null;
    nativeInstallCheckInputs = [ pkgs-master.jq ];
    installCheckPhase = ''
      runHook preInstallCheck

      # Stage bats sources to a writable scratch dir; bats writes
      # per-test temp dirs and BATS_LIB_PATH discovery walks from there.
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
  madder-race = madder.overrideAttrs (old: {
    pname = "madder-race";

    # Race detection requires CGO and a -race build flag for the
    # binaries (not just the test binaries). buildGoApplication's
    # goBuildHook picks up buildFlagsArray and passes it through to
    # `go install`.
    CGO_ENABLED = 1;
    buildFlagsArray = (old.buildFlagsArray or [ ]) ++ [ "-race" ];

    checkPhase = ''
      runHook preCheck
      go test -tags test -race -p $NIX_BUILD_CORES ./...
      runHook postCheck
    '';
  });

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
    inherit madder madder-race madder-cover;
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
