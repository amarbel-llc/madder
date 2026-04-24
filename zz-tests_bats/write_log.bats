setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
  export XDG_LOG_HOME="$BATS_TEST_TMPDIR/xdg-log"
}

# bats file_tags=write_log

today_log() {
  local date
  date="$(date -u +%Y-%m-%d)"
  echo "$XDG_LOG_HOME/madder/blob-writes-$date.ndjson"
}

function write_log_emits_written_record { # @test
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello world" >"$blob"

  run_madder write "$blob"
  assert_success

  local log
  log="$(today_log)"
  [[ -s $log ]] || fail "expected write-log at $log, got none"

  # Exactly one NDJSON record, op=written.
  local n
  n="$(grep -c '"op":"written"' "$log" || true)"
  [[ $n -eq 1 ]] || fail "expected 1 written record, got $n"
}

function write_log_duplicate_is_exists { # @test
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "same bytes" >"$blob"

  # First write: op=written.
  run_madder write "$blob"
  assert_success

  # Second write of identical bytes: op=exists (link(2) returns EEXIST,
  # verify-on-collision is not enabled by default).
  run_madder write "$blob"
  assert_success

  local log
  log="$(today_log)"
  local written_count exists_count
  written_count="$(grep -c '"op":"written"' "$log" || true)"
  exists_count="$(grep -c '"op":"exists"' "$log" || true)"

  [[ $written_count -eq 1 ]] || fail "expected 1 written, got $written_count"
  [[ $exists_count -eq 1 ]] || fail "expected 1 exists, got $exists_count"
}

function write_log_disabled_by_no_write_log_flag { # @test
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello" >"$blob"

  local bin="${MADDER_BIN:-madder}"
  run timeout --preserve-status 2s "$bin" --no-write-log write "$blob"
  assert_success

  local log
  log="$(today_log)"
  [[ ! -e $log ]] || fail "--no-write-log should prevent log file creation at $log"
}

function write_log_disabled_by_env_var { # @test
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello" >"$blob"

  MADDER_WRITE_LOG=0 run_madder write "$blob"
  assert_success

  local log
  log="$(today_log)"
  [[ ! -e $log ]] || fail "MADDER_WRITE_LOG=0 should prevent log file creation at $log"
}

function write_log_record_has_contracted_fields { # @test
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "schema check" >"$blob"

  run_madder write "$blob"
  assert_success

  local log
  log="$(today_log)"
  local line
  line="$(head -n 1 "$log")"

  # Every field the ADR contracts is present. The description field is
  # optional (omitempty) and expected to be absent when --log-description
  # is not passed — covered by a separate test below.
  echo "$line" | grep -q '"ts":' || fail "record missing ts field: $line"
  echo "$line" | grep -q '"utility":"madder"' || fail "record utility != madder: $line"
  echo "$line" | grep -q '"pid":' || fail "record missing pid field: $line"
  echo "$line" | grep -q '"store_id":' || fail "record missing store_id: $line"
  echo "$line" | grep -q '"markl_id":' || fail "record missing markl_id: $line"
  echo "$line" | grep -q '"size":' || fail "record missing size field: $line"
  echo "$line" | grep -q '"op":"written"' || fail "record op != written: $line"

  # description is omitempty when the flag is absent.
  echo "$line" | grep -q '"description"' &&
    fail "description field should be absent when --log-description not passed: $line" ||
    true
}

function write_log_description_flag_stamps_records { # @test
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "schema check" >"$blob"

  run_madder write --log-description 'imported Q3 backup tapes' "$blob"
  assert_success

  local log
  log="$(today_log)"
  [[ -s $log ]] || fail "expected write-log at $log, got none"

  local line
  line="$(head -n 1 "$log")"
  echo "$line" | grep -q '"description":"imported Q3 backup tapes"' ||
    fail "record missing or wrong description: $line"
}

function write_log_empty_description_omits_field { # @test
  # Passing --log-description with an empty string should still omit the
  # field (omitempty); otherwise users who set it to an empty default
  # get noisy "description":"" in every record.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello" >"$blob"

  run_madder write --log-description '' "$blob"
  assert_success

  local log
  log="$(today_log)"
  local line
  line="$(head -n 1 "$log")"
  echo "$line" | grep -q '"description"' &&
    fail "empty --log-description should omit the field: $line" ||
    true
}
