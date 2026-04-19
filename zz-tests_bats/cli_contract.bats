setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=contract,write,read,has

FAKE_HASH="blake2b256-0000000000000000000000000000000000000000000000000000000000000000"

function write_prints_digest_to_stdout { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "contract test content" >"$blob"

  run_madder write -format tap "$blob"
  assert_success

  hash="$(echo "$output" | grep '^ok ' | awk '{print $4}')"
  [[ -n $hash ]] || fail "write did not print digest in TAP output"
  [[ $hash == blake2b256-* ]] || fail "digest does not start with hash algorithm prefix: $hash"
}

function cat_exits_nonzero_on_missing_blob { # @test

  init_store

  run_madder cat "$FAKE_HASH"
  assert_failure
}

function has_exits_zero_for_existing_blob { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "has test content" >"$blob"

  run_madder write -format tap "$blob"
  assert_success
  hash="$(echo "$output" | grep '^ok ' | awk '{print $4}')"
  [[ -n $hash ]] || fail "write did not print digest"

  run_madder has "$hash"
  assert_success
  assert_output --partial "found"
}

function has_exits_nonzero_for_missing_blob { # @test

  init_store

  run_madder has "$FAKE_HASH"
  assert_failure
  assert_output --partial "not found"
}

function has_mixed_found_and_missing { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "mixed test content" >"$blob"

  run_madder write -format tap "$blob"
  assert_success
  hash="$(echo "$output" | grep '^ok ' | awk '{print $4}')"
  [[ -n $hash ]] || fail "write did not print digest"

  run_madder has "$hash" "$FAKE_HASH"
  assert_failure
  assert_output --partial "found"
  assert_output --partial "not found"
}
