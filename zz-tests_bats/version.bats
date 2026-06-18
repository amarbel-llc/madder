setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=version

# Covers the version-burnin wiring end-to-end: version.env at repo
# root is the source of truth, flake.nix reads it via builtins
# .readFile, go/default.nix passes it through to buildGoApplication's
# -ldflags injection into go/internal/0/buildinfo, and `madder version`
# prints them. All lanes (madder, madder-race, madder-cli-cover) build
# under buildGoApplication, so the ldflags are auto-injected uniformly
# and `dev+unknown` defaults never reach this suite.

function version_prints_burnt_in_identity { # @test
  run_madder version
  assert_success

  # Format: <version>+<commit>. Every MADDER_BIN-producing lane stamps
  # ldflags from version.env, so the dev+unknown defaults must never
  # appear here.
  assert_output --regexp '^[^+]+\+[^+]+$'
  refute_output --partial 'dev+unknown'
}

function version_matches_source_of_truth { # @test
  # Guards against drift between bump-version's sed target and the
  # ldflag target — the version prefix must match version.env exactly.
  run_madder version
  assert_success

  local got_version
  got_version="$(echo "$output" | head -n1 | cut -d+ -f1)"

  local expected_version
  expected_version="$(grep '^export MADDER_VERSION=' "${BATS_TEST_DIRNAME}/../version.env" | cut -d= -f2)"

  assert_equal "$got_version" "$expected_version"
}

function version_madder_cache_matches_madder { # @test
  # Both binaries read from the same buildinfo package; reported
  # identity must match byte-for-byte. Detects ldflag drift if
  # subPackages ever needs split builds. madder-cache is derived from
  # MADDER_BIN because the bats harness only plumbs MADDER_BIN.
  local madder_cache_bin
  madder_cache_bin="$(dirname "${MADDER_BIN:-madder}")/madder-cache"

  run_madder version
  assert_success
  local madder_version="$output"

  run timeout --preserve-status 2s "$madder_cache_bin" version
  assert_success
  local madder_cache_version="$output"

  assert_equal "$madder_version" "$madder_cache_version"
}
