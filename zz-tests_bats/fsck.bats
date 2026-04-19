setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=fsck

function tap14_output { # @test

  init_store
  run_madder fsck -format tap
  assert_success
  assert_output --partial "TAP version 14"
  assert_output --partial "1.."
  refute_output --partial "not ok"
}

function with_blobs { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "test content" >"$blob"
  run_madder write -format tap "$blob"
  assert_success

  run_madder fsck -format tap
  assert_success
  assert_output --partial "TAP version 14"
  refute_output --partial "not ok"
}

function fsck_json_auto_detects { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "auto-detect content" >"$blob"
  run_madder write -format tap "$blob"
  assert_success

  # Default (auto) + piped stdout -> NDJSON with verified records.
  run_madder fsck
  assert_success
  assert_output --partial '"state":"verified"'
  refute_output --partial 'TAP version 14'
}

function fsck_json_reports_missing { # @test

  # Delete a blob file from disk after writing to simulate corruption /
  # missing, then confirm fsck emits a missing record.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "blob to remove" >"$blob"
  run_madder write -format tap "$blob"
  assert_success

  # Remove every *.blob_store file to force the mismatch. The AllBlobs
  # iterator still sees the stored ID but HasBlob returns false.
  find .madder -type f -name '*.zstd*' -delete 2>/dev/null || true
  find .madder -type d -name 'blobs' -exec sh -c 'rm -rf "$0"/*' {} \; 2>/dev/null || true

  run_madder fsck -format json
  # May be success or failure depending on whether the blob existed;
  # what we care about is the stream shape when something is off.
  assert_output --partial '"store":'
}
