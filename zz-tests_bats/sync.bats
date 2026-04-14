setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=sync

function cross_hash_sync { # @test
  skip "flag parsing or encryption bug — investigating (madder#2)"
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "cross-hash-test" >"$blob"
  run_madder write "$blob"
  assert_success
  blake_sha="$(echo "$output" | grep -oP 'blake2b256-\S+')"

  run_madder init -hash_type-id sha256 -encryption none -lock-internal-files=false .sha256
  assert_success

  run_madder sync .default .sha256
  assert_success

  run_madder cat-ids .sha256
  assert_success
  assert_output --partial "$blake_sha"

  run_madder cat .sha256 "$blake_sha"
  assert_success
  assert_line "cross-hash-test"
}

function sync_idempotent { # @test
  skip "flag parsing or encryption bug — investigating (madder#2)"
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "idempotent-test" >"$blob"
  run_madder write "$blob"
  assert_success

  run_madder init -hash_type-id sha256 -encryption none -lock-internal-files=false .sha256
  assert_success

  run_madder sync .default .sha256
  assert_success

  run_madder sync .default .sha256
  assert_success
}
