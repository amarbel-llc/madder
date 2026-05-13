setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=write,read

function write_and_cat { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello world" >"$blob"
  local hash
  hash="$(write_blob_id "$blob")"

  run_madder cat "$hash"
  assert_success
  assert_output --partial "hello world"
}

function write_from_stdin { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "stdin content" >"$blob"

  run_madder write -format tap -
  assert_success
}

function list_after_write { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "list test" >"$blob"

  run_madder write -format tap "$blob"
  assert_success

  run_madder list
  assert_success
  assert_output
}

function list_text_includes_path_comment { # @test

  # #172: each text-mode line ends with `# path: <rel>` pointing at the
  # store's on-disk config file.
  init_store

  run_madder list -format tap
  assert_success
  assert_output --partial "# path: "
}

function list_json_emits_ndjson_records { # @test

  # #173: `-format=json` emits one NDJSON record per store with the
  # documented fields.
  init_store
  run_madder init -encryption none .other
  assert_success

  run_madder list -format json
  assert_success

  local count
  count="$(printf '%s\n' "$output" | jq -s 'length')"
  [[ $count -eq 2 ]] || fail "expected 2 NDJSON records, got $count: $output"

  # Every record must have all four documented fields.
  local bad
  bad="$(printf '%s\n' "$output" | jq -r 'select((has("id") and has("description") and has("config_path") and has("base")) | not) | input_line_number' | wc -l)"
  [[ $bad -eq 0 ]] || fail "records missing required fields: $output"
}

function list_json_rejects_unknown_format { # @test

  init_store

  run_madder list -format wat
  assert_failure
}

function write_warns_when_file_shadows_store { # @test

  # Bare `write shadowed` resolves to the file but must warn about the
  # blob-store-id collision.
  init_store
  run_madder init -encryption none shadowed
  assert_success

  echo "file content" >shadowed

  run_madder write -format tap shadowed
  assert_success
  assert_output --partial "shadows blob-store-id"
  assert_output --partial "'./shadowed'"
  assert_output --partial '".shadowed"'
}

function pack_blobs_warns_when_file_shadows_store { # @test

  # Regression for #25: pack-blobs shares write's arg_resolver and must
  # emit the shadow warning even though .default isn't packable.
  init_store
  run_madder init -encryption none shadowed
  assert_success

  echo "file content" >shadowed

  run_madder pack-blobs -format tap shadowed
  assert_output --partial "shadows blob-store-id"
}

function write_no_warning_when_no_store_collision { # @test

  # Control for write_warns_when_file_shadows_store.
  init_store

  echo "file content" >unique_filename

  run_madder write -format tap unique_filename
  assert_success
  refute_output --partial "shadows blob store"
}

# NDJSON output mode (issue #26) -----------------------------------------

function write_json_emits_ndjson_record { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'hello world\n' >"$blob"

  run_madder write -format json "$blob"
  assert_success
  assert_line --regexp '^\{"id":"[^"]+","size":12,"source":"[^"]+blob\.txt"\}$'
}

function write_auto_detects_json_when_stdout_is_pipe { # @test

  # bats `run` is never a TTY, so auto-format must pick JSON.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'auto detect\n' >"$blob"

  run_madder write "$blob"
  assert_success
  assert_line --regexp '^\{"id":"[^"]+","size":12,"source":"[^"]+blob\.txt"\}$'
}

function write_json_check_present { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'already here\n' >"$blob"
  run_madder write -format json "$blob"
  assert_success

  run_madder write -check -format json "$blob"
  assert_success
  assert_output --partial '"present":true'
}

function write_json_check_missing { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'not stored\n' >"$blob"

  run_madder write -check -format json "$blob"
  assert_failure
  assert_output --partial '"present":false'
}

function write_tap_override_on_pipe { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'tap override\n' >"$blob"

  run_madder write -format tap "$blob"
  assert_success
  assert_line 'TAP version 14'
  assert_line --regexp '^ok 1 - '
  refute_output --partial '"id":'
}

function write_json_rejects_unknown_format { # @test

  init_store

  run_madder write -format bogus -
  assert_failure
  assert_output --partial 'unsupported output format'
}

function write_json_warning_goes_to_stderr { # @test

  # In JSON mode the warning must route to stderr so stdout stays valid
  # NDJSON. bats `run` merges streams, so we can only assert no warning
  # mid-record by checking that exactly one NDJSON line appears.
  init_store
  run_madder init -encryption none shadowed
  assert_success

  echo "file content" >shadowed

  run_madder write -format json shadowed
  assert_success
  local n
  n="$(echo "$output" | grep -c '^{')"
  [[ $n -eq 1 ]] || fail "expected 1 NDJSON record, got $n"
  assert_output --partial 'shadows blob-store-id'
}
