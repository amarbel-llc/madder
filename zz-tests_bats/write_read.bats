setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=write,read

function write_and_cat { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello world" >"$blob"

  run_madder write "$blob"
  assert_success
  hash="$(echo "$output" | grep '^ok ' | awk '{print $4}')"
  [[ -n $hash ]] || fail "write returned empty hash"

  run_madder cat "$hash"
  assert_success
  assert_output --partial "hello world"
}

function write_from_stdin { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "stdin content" >"$blob"

  run_madder write -
  assert_success
}

function list_after_write { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "list test" >"$blob"

  run_madder write "$blob"
  assert_success

  run_madder list
  assert_success
  assert_output
}

function write_warns_when_file_shadows_store { # @test

  # Init two stores: the default .default (CWD-scoped) plus one named
  # "shadowed" (XDG user). Then create a file with the same bare name in
  # the CWD. A bare `write shadowed` should resolve to the file but emit
  # a warning comment pointing at the store collision.
  init_store
  run_madder init -encryption none -lock-internal-files=false shadowed
  assert_success

  echo "file content" >shadowed

  run_madder write shadowed
  assert_success
  assert_output --partial "shadows blob store"
  assert_output --partial "'./shadowed'"
  assert_output --partial '".shadowed"'
}

function write_no_warning_when_no_store_collision { # @test

  # Control: same file pattern, but no store with matching name. No
  # warning should fire.
  init_store

  echo "file content" >unique_filename

  run_madder write unique_filename
  assert_success
  refute_output --partial "shadows blob store"
}
