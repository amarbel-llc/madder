#! /bin/bash -e

if [[ -z $BATS_TEST_TMPDIR ]]; then
  # shellcheck disable=SC2016
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

run_madder() {
  local bin="${MADDER_BIN:-madder}"
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

# write_blob_id pipes a file through `madder write -format tap` and
# echoes just the blob-id (the 4th column of the `ok 1 - ...` line).
# Used to inject hand-crafted receipt blobs without touching any
# capture-specific layout. Pass an explicit store-id as the first
# arg to target a non-default store: `write_blob_id .work path`.
write_blob_id() {
  local store path
  if [[ $# -eq 1 ]]; then
    path="$1"
    run_madder write -format tap "$path"
  else
    store="$1"
    path="$2"
    run_madder write -format tap "$store" "$path"
  fi
  assert_success
  # shellcheck disable=SC2154
  echo "$output" | grep '^ok ' | awk '{print $4}' | head -n 1
}

# today_session_file echoes the path of the (first) hyphence session
# file under today's $XDG_LOG_HOME/madder/inventory_log/<date>/ dir.
# Each madder CLI invocation produces its own session file; callers
# running exactly one write get exactly one file. Returns nonzero if
# no day-dir exists yet.
today_session_file() {
  local date day_dir
  date="$(date -u +%Y-%m-%d)"
  day_dir="$XDG_LOG_HOME/madder/inventory_log/$date"
  [[ -d $day_dir ]] || return 1
  find "$day_dir" -maxdepth 1 -name '*.hyphence' 2>/dev/null | head -n 1
}

# session_body strips the 4-line hyphence header (---, ! type, ---,
# blank separator) from a session file, leaving just the NDJSON body.
session_body() {
  tail -n +5 "$1"
}
