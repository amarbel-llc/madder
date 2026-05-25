{
  nixpkgs,
  nixpkgs-master,
  tommy,
  bats,
  purse-first,
  # `doppelgang lint` runs in the `default` lane as a CI gate against
  # flake.lock duplication (madder#214). Defaulted to null so direct
  # `import ./go/default.nix` callers without a flake context still
  # work — the devShell just won't carry doppelgang in that mode.
  doppelgang ? null,
  system,
  # Filtered Go source tree (test-superset shape) produced by
  # mkGoPkgs in go/gomod.nix and threaded through flake.nix. Every
  # buildGoApplication self-consumes this as `src`/`pwd` so madder
  # builds itself from the same artifact downstream consumers see —
  # contract test for the go-pkgs / go-pkgs-test split (#212).
  # Defaulted to ./. so non-flake callers (`import ./go/default.nix`
  # without flake context) still work — they just build from the
  # live worktree.
  goPkgsTest ? ./.,
  # Flake-input bridge table (see ./gomod.nix). Defaulted to {} so
  # non-flake callers degrade to organic gomod2nix.toml resolution.
  goFlakeInputs ? { },
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

  # mkBatsLane wraps bats.lib.${system}.batsLane (from amarbel-llc/bats)
  # with madder's parameter shape: vanilla bats, bats-libs on
  # BATS_LIB_PATH, MADDER_BIN / CG_BIN / HYPHENCE_BIN exported via the
  # binaries-map form, version.env staged sibling-of-bats, jq for
  # cli_contract.bats's JSON helpers, git for bats-island's
  # setup_test_home, BATS_TEST_TIMEOUT=30 to mirror zz-tests_bats/justfile.
  #
  # `filter` is forwarded verbatim to `bats --filter-tags`. Default
  # `!net_cap` excludes loopback-binding scenarios; callers override
  # for per-tag dev-loop selection.
  #
  # `extraBinaries` is an overlay onto the default binaries map. The
  # net_cap lane uses this to add the devshell-only test-fixture
  # binaries (madder-test-sftp-server, madder-test-craft-legacy-blob,
  # madder-test-webdav-server) so `nix build .#bats-net_cap` is
  # self-sufficient.
  mkBatsLane = { filter ? "!net_cap", base ? madder, extraBinaries ? { } }:
    bats.lib.${system}.batsLane {
      inherit base filter batsSrc;
      binaries = {
        MADDER_BIN   = { inherit base; name = "madder"; };
        CG_BIN       = { inherit base; name = "cutting-garden"; };
        HYPHENCE_BIN = { inherit base; name = "hyphence"; };
      } // extraBinaries;
      batsLibPath = [ bats.packages.${system}.bats-libs.batsLibPath ];
      extraStagedFiles = [
        { src = versionEnv; dest = "version.env"; }
      ];
      extraEnv = { BATS_TEST_TIMEOUT = "30"; };
      # git: bats-island's setup_test_home shells out to `git config`.
      # jq: cli_contract.bats's JSON helpers.
      # openssh: zz-tests_bats/lib/sftp_legacy.bash spawns ssh-agent
      #   and shells out to ssh-keygen.
      # openssl: webdav.bats generates self-signed client certs for
      #   the TLS-client-cert tests.
      # unixtools.xxd: sftp.bash / webdav.bash generate per-test
      #   cookies via `xxd -p`; webdav.bats also reads file magic bytes.
      nativeBuildInputs = [
        pkgs-master.jq
        pkgs-master.git
        pkgs-master.openssh
        pkgs-master.openssl
        pkgs-master.unixtools.xxd
      ];
    };

  # Test-fixture binaries (madder-test-sftp-server,
  # madder-test-craft-legacy-blob, madder-test-webdav-server) exported
  # under env vars the bats helpers read (zz-tests_bats/lib/sftp.bash,
  # sftp_legacy.bash, webdav.bash). Only attached to the net_cap lane
  # so non-net_cap lanes don't pay the cache-invalidation cost when a
  # fixture binary's source changes.
  netCapExtraBinaries = {
    MADDER_TEST_SFTP_SERVER = {
      base = madder-test-sftp-server;
      name = "madder-test-sftp-server";
    };
    MADDER_TEST_CRAFT_LEGACY_BLOB = {
      base = madder-test-craft-legacy-blob;
      name = "madder-test-craft-legacy-blob";
    };
    MADDER_TEST_WEBDAV_SERVER = {
      base = madder-test-webdav-server;
      name = "madder-test-webdav-server";
    };
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
    export CG_BIN="$out/bin/cutting-garden"
    export HYPHENCE_BIN="$out/bin/hyphence"
    export BATS_LIB_PATH="''${BATS_LIB_PATH:+$BATS_LIB_PATH:}${bats.packages.${system}.bats-libs.batsLibPath}"
    export BATS_TEST_TIMEOUT=30

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
    inherit version commit goFlakeInputs;
    src = goPkgsTest;
    pwd = goPkgsTest;
    subPackages = [
      "cmd/madder"
      "cmd/madder-cache"
      "cmd/madder-gen_man"
      "cmd/madder-mcp"
      "cmd/cutting-garden"
      "cmd/cg"
      "cmd/hyphence"
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

  # cutting-garden is the standalone flake-package form of the
  # cmd/cutting-garden binary (plus its `cg` alias). The `madder`
  # derivation already builds both as part of its subPackages list, so
  # the binaries also live at `${madder}/bin/{cutting-garden,cg}`. This
  # standalone package exists so downstream `amarbel-llc/cutting-garden`
  # can `nix build github:amarbel-llc/madder#cutting-garden` for the
  # Phase 6 receipt-identity cross-test (madder#176) without pulling
  # the rest of the madder toolchain.
  #
  # `doCheck = false` because the full Go test suite is already run by
  # the `madder` derivation's checkPhase; re-running it here would
  # double-pay the cost on every downstream consumer. Man pages are
  # intentionally not generated — `madder-gen_man` emits pages for
  # every utility at once, which is the wrong shape for a single-binary
  # package, and the cross-test consumer only needs the binary itself.
  cutting-garden = pkgs.buildGoApplication {
    pname = "cutting-garden";
    inherit version commit goFlakeInputs;
    src = goPkgsTest;
    pwd = goPkgsTest;
    subPackages = [
      "cmd/cutting-garden"
      "cmd/cg"
    ];
    modules = ./gomod2nix.toml;
    go = pkgs-master.go_1_26;
    GOTOOLCHAIN = "local";

    nativeBuildInputs = [
      purse-first.packages.${system}.dagnabit
    ];

    preBuild = ''
      dagnabit export
    '';

    doCheck = false;
  };

  # madder-clown-plugin stages a clown plugin (see clown-plugin-protocol(7)
  # / clown-json(5)) that exposes madder blobs as MCP resources at
  # `madder://blobs/<digest>`. The clown plugin protocol disallows
  # ${...} expansion in stdioServers.command, so the binary path is
  # baked in at build time via Nix substitution: the source-controlled
  # `clown.json.in` template uses an `@madder-mcp@` placeholder which is
  # rewritten to `${madder}/bin/madder-mcp` here. Consumers wire the
  # plugin into clown by pointing `--plugin-dir` at
  # `${madder-clown-plugin}/share/purse-first/madder/`.
  madder-clown-plugin = pkgs.runCommand "madder-clown-plugin" { } ''
    mkdir -p $out/share/purse-first/madder/.claude-plugin
    cp ${../plugins/madder/.claude-plugin/plugin.json} \
       $out/share/purse-first/madder/.claude-plugin/plugin.json
    substitute \
      ${../plugins/madder/clown.json.in} \
      $out/share/purse-first/madder/clown.json \
      --replace-fail '@madder-mcp@' '${madder}/bin/madder-mcp'
  '';

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
            (mkBatsLane {
              filter = tag;
              # Per-tag binaries overlay: the net_cap lane gets the
              # SFTP/WebDAV/craft-legacy-blob test-fixture binaries so
              # `nix build .#bats-net_cap` is self-sufficient.
              extraBinaries =
                if tag == "net_cap" then netCapExtraBinaries else { };
            }))
          allFileTags) // {
          bats-default = mkBatsLane { filter = "!net_cap"; };
          # No bats-race-net_cap: the race-instrumented binary doubles
          # build time and the SFTP/WebDAV harnesses already exercise
          # the same data paths under the non-race net_cap lane.
          bats-race = mkBatsLane { filter = "!net_cap"; base = madder-race; };
        };

  # SFTP test harness (RFC 0001). Exposed as a named package output so
  # downstream test harnesses (e.g. dodder's haustoria_orgmode bats
  # lanes) can consume it without duplicating the server. Kept out of
  # `packages.default` — release artifacts must not ship a server that
  # accepts any password — but addressable as
  # `madder.packages.${system}.madder-test-sftp-server` for explicit
  # opt-in by test-only consumers. See amarbel-llc/madder#177.
  madder-test-sftp-server = pkgs.buildGoApplication {
    pname = "madder-test-sftp-server";
    version = "0.0.0";
    inherit goFlakeInputs;
    src = goPkgsTest;
    pwd = goPkgsTest;
    subPackages = [ "cmd/madder-test-sftp-server" ];
    modules = ./gomod2nix.toml;
    go = pkgs-master.go_1_26;
    GOTOOLCHAIN = "local";
  };

  # Devshell-only fixture binary used by bats to materialize
  # legacy-shaped blob bytes for sftp-analyze-and-suggest-configs
  # tests. Same NOT-shipped policy as madder-test-sftp-server: the
  # binary is purely a test fixture.
  madder-test-craft-legacy-blob = pkgs.buildGoApplication {
    pname = "madder-test-craft-legacy-blob";
    version = "0.0.0";
    inherit goFlakeInputs;
    src = goPkgsTest;
    pwd = goPkgsTest;
    subPackages = [ "cmd/madder-test-craft-legacy-blob" ];
    modules = ./gomod2nix.toml;
    go = pkgs-master.go_1_26;
    GOTOOLCHAIN = "local";
  };

  # Devshell-only test harness for WebDAV integration tests (RFC 0001).
  # Intentionally NOT included in the `packages` output — release
  # artifacts must not ship a server that accepts any auth.
  madder-test-webdav-server = pkgs.buildGoApplication {
    pname = "madder-test-webdav-server";
    version = "0.0.0";
    inherit goFlakeInputs;
    src = goPkgsTest;
    pwd = goPkgsTest;
    subPackages = [ "cmd/madder-test-webdav-server" ];
    modules = ./gomod2nix.toml;
    go = pkgs-master.go_1_26;
    GOTOOLCHAIN = "local";
  };
in
{
  packages = {
    inherit madder madder-race madder-cover madder-cli-cover madder-clown-plugin cutting-garden madder-test-sftp-server;
    default = madder;
  } // batsLaneOutputs;

  devShells.default = pkgs-master.mkShell {
    packages = [
      (pkgs.mkGoEnv { pwd = ./.; inherit goFlakeInputs; })
      tommy.packages.${system}.default
      bats.packages.${system}.default
      purse-first.packages.${system}.dagnabit
      madder-test-sftp-server
      madder-test-craft-legacy-blob
      madder-test-webdav-server
    ]
    ++ pkgs-master.lib.optionals (doppelgang != null) [
      doppelgang.packages.${system}.default
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
