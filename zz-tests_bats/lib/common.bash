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

require_bin MADDER_BIN madder

run_madder() {
  local bin="${MADDER_BIN:-madder}"
  run timeout --preserve-status 2s "$bin" "$@"
}

init_store() {
  run_madder init -encryption none -lock-internal-files=false "${1:-.default}"
  assert_success
}

set_xdg() {
  local base="$1"
  mkdir -p "$base/.xdg/data" "$base/.xdg/config" "$base/.xdg/state" "$base/.xdg/cache" "$base/.xdg/runtime"
  export XDG_DATA_HOME="$base/.xdg/data"
  export XDG_CONFIG_HOME="$base/.xdg/config"
  export XDG_STATE_HOME="$base/.xdg/state"
  export XDG_CACHE_HOME="$base/.xdg/cache"
  export XDG_RUNTIME_DIR="$base/.xdg/runtime"
}
