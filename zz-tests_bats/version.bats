setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=version

# Covers the version-burnin wiring end-to-end: flake.nix defines
# madderVersion and madderCommit, go/default.nix passes them via -ldflags
# to go/internal/0/buildinfo, and `madder version` prints them.
#
# When these tests run under bats, MADDER_BIN points at the nix-built
# binary, which means the ldflags MUST have fired. A devshell `go build`
# would leave the defaults ("dev+unknown") — visible to the developer
# as a separate signal that they're on an unreleased build.

function version_prints_burnt_in_identity { # @test
  run_madder version
  assert_success

  # Format: <version>+<commit>. Nix builds always override defaults.
  # Accept anything that fits "nonempty+nonempty" and rule out the
  # dev fallback explicitly.
  assert_output --regexp '^[^+]+\+[^+]+$'
  refute_output --partial 'dev+unknown'
}

function version_matches_flake_version { # @test
  # The version prefix before '+' must match the madderVersion
  # literal in flake.nix. Guards against drift between bump-version
  # sed target and the ldflag target.
  run_madder version
  assert_success

  local got_version
  got_version="$(echo "$output" | head -n1 | cut -d+ -f1)"

  local flake_version
  flake_version="$(grep 'madderVersion = ' "${BATS_TEST_DIRNAME}/../flake.nix" | sed 's/.*"\(.*\)".*/\1/')"

  [[ $got_version == "$flake_version" ]] ||
    fail "madder version prefix '$got_version' does not match flake.nix madderVersion '$flake_version'"
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
