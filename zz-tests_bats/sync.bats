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

function sync_crap_auto_detects { # @test

  # Default auto-format under `run` (no TTY) must emit ndjson-crap.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "sync-crap-test" >"$blob"
  run_madder write -format tap "$blob"
  assert_success

  run_madder init -hash_type-id sha256 -encryption none .sha256
  assert_success

  run_madder sync .default .sha256
  assert_success
  # Meta header (Source: "madder") still emits a "crap" record; the body is
  # now operation-family records (scan phase + Operation), not a result
  # summary.
  assert_output --partial '"type":"crap"'
  assert_output --partial '"type":"operation_start"'
  assert_output --partial '"type":"operation_end"'
  refute_output --partial 'TAP version 14'
}

function sync_ndjson_opt_out { # @test

  # -format ndjson keeps the legacy {id,state,size,error} records.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "sync-ndjson-test" >"$blob"
  run_madder write -format tap "$blob"
  assert_success

  run_madder init -hash_type-id sha256 -encryption none .sha256
  assert_success

  run_madder sync -format ndjson .default .sha256
  assert_success
  assert_output --partial '"state":"transferred"'
  refute_output --partial '"type":"crap"'
}

function sync_rejects_tap { # @test

  # sync no longer supports TAP; -format tap is rejected at runtime.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "x" >"$blob"
  run_madder write -format tap "$blob"
  assert_success

  run_madder init -hash_type-id sha256 -encryption none .sha256
  assert_success

  run_madder sync -format tap .default .sha256
  assert_failure
  assert_output --partial "does not support -format tap"
}
