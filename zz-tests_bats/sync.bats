setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=sync

function cross_hash_sync { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "cross-hash-test" >"$blob"
  local blake_sha
  blake_sha="$(write_blob_id "$blob")"

  run_madder init -hash_type-id sha256 -encryption none .sha256
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

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "idempotent-test" >"$blob"
  run_madder write "$blob"
  assert_success

  run_madder init -hash_type-id sha256 -encryption none .sha256
  assert_success

  run_madder sync .default .sha256
  assert_success

  run_madder sync .default .sha256
  assert_success
}

function sync_json_auto_detects { # @test

  # Default auto-format under `run` (no TTY) must emit NDJSON, not TAP.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "sync-json-test" >"$blob"
  run_madder write -format tap "$blob"
  assert_success

  run_madder init -hash_type-id sha256 -encryption none .sha256
  assert_success

  run_madder sync .default .sha256
  assert_success
  assert_output --partial '"state":"transferred"'
  refute_output --partial 'TAP version 14'
}
