#! /bin/bash -e

if [[ -z $BATS_TEST_TMPDIR ]]; then
  echo 'common.bash loaded before $BATS_TEST_TMPDIR set. aborting.' >&2

  cat >&2 <<-'EOM'
    only load this file from `.bats` files like so:

    setup() {
      load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"

      # for shellcheck SC2154
      export output
    }

    as there is a hard assumption on $BATS_TEST_TMPDIR being set
EOM

  exit 1
fi

pushd "$BATS_TEST_TMPDIR" >/dev/null || exit 1

bats_load_library bats-support
bats_load_library bats-assert
bats_load_library bats-emo
bats_load_library bats-island

setup_test_home
export MADDER_CEILING_DIRECTORIES="$BATS_TEST_TMPDIR"

# pkgs.testers.batsLane (the nix-driven lane builder) only exports one
# binary env var. Both binaries ship from the same install, so derive
# CG_BIN from MADDER_BIN's directory when CG_BIN isn't explicitly set
# (the dev-loop justfile sets both).
if [ -n "${MADDER_BIN:-}" ] && [ -z "${CG_BIN:-}" ]; then
  export CG_BIN="$(dirname "$MADDER_BIN")/cutting-garden"
fi

require_bin MADDER_BIN madder
require_bin CG_BIN cutting-garden

run_madder() {
  local bin="${MADDER_BIN:-madder}"
  run timeout --preserve-status 2s "$bin" "$@"
}

run_cg() {
  local bin="${CG_BIN:-cutting-garden}"
  run timeout --preserve-status 2s "$bin" "$@"
}

init_store() {
  run_madder init -encryption none "${1:-.default}"
  assert_success
}

# Returns octal mode bits (e.g. '444', '644'). GNU stat in the nix
# devshell.
file_mode() {
  stat -c '%a' "$1"
}
