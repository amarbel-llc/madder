setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=fsck

function tap14_output { # @test

  init_store
  run_madder fsck
  assert_success
  assert_output --partial "TAP version 14"
  assert_output --partial "1.."
  refute_output --partial "not ok"
}

function with_blobs { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "test content" >"$blob"
  run_madder write "$blob"
  assert_success

  run_madder fsck
  assert_success
  assert_output --partial "TAP version 14"
  refute_output --partial "not ok"
}
