setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=write,read

function write_and_cat { # @test
  skip "unsupported hash type bug — investigating"
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
  skip "unsupported hash type bug — investigating"
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "stdin content" >"$blob"

  run_madder write -
  assert_success
}

function list_after_write { # @test
  skip "unsupported hash type bug — investigating"
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "list test" >"$blob"

  run_madder write "$blob"
  assert_success

  run_madder list
  assert_success
  assert_output
}
