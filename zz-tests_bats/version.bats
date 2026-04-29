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

  # Format: <version>+<commit>. All lanes that produce MADDER_BIN stamp
  # ldflags from version.env, so the dev+unknown defaults must never
  # appear here.
  assert_output --regexp '^[^+]+\+[^+]+$'
  refute_output --partial 'dev+unknown'
}

function version_matches_source_of_truth { # @test
  # The version prefix before '+' must match MADDER_VERSION in
  # version.env, the source of truth for releases. Guards against
  # drift between bump-version's sed target and the ldflag target.
  run_madder version
  assert_success

  local got_version
  got_version="$(echo "$output" | head -n1 | cut -d+ -f1)"

  local expected_version
  expected_version="$(grep '^MADDER_VERSION=' "${BATS_TEST_DIRNAME}/../version.env" | cut -d= -f2)"

  [[ $got_version == "$expected_version" ]] ||
    fail "madder version prefix '$got_version' does not match version.env MADDER_VERSION '$expected_version'"
}

function version_madder_cache_matches_madder { # @test
  # Both binaries read from the same buildinfo package, so their
  # reported identity must match byte-for-byte. Detects ldflag drift
  # between the two subPackages if subPackages ever needs split builds.
  # madder-cache is derived from MADDER_BIN because the bats harness
  # only plumbs MADDER_BIN explicitly.
  local madder_cache_bin
  madder_cache_bin="$(dirname "${MADDER_BIN:-madder}")/madder-cache"

  run_madder version
  assert_success
  local madder_version="$output"

  run timeout --preserve-status 2s "$madder_cache_bin" version
  assert_success
  local madder_cache_version="$output"

  [[ $madder_version == "$madder_cache_version" ]] ||
    fail "madder=\"$madder_version\" madder-cache=\"$madder_cache_version\" disagree"
}
