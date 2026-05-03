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
require_bin MADDER_BIN madder
require_bin CG_BIN cutting-garden
require_bin HYPHENCE_BIN hyphence

run_madder() {
  local bin="${MADDER_BIN:-madder}"
  run timeout --preserve-status 2s "$bin" "$@"
}

run_cg() {
  local bin="${CG_BIN:-cutting-garden}"
  run timeout --preserve-status 2s "$bin" "$@"
}

run_hyphence() {
  local bin="${HYPHENCE_BIN:-hyphence}"
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
